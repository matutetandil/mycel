// Package websocket provides a bidirectional WebSocket connector for real-time communication.
// It supports broadcast, room-based messaging, and per-user targeting.
package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/matutetandil/mycel/internal/connector"
)

// HandlerFunc is a function that handles a flow request.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// Client represents a connected WebSocket client.
type Client struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	rooms  map[string]bool
	params map[string]interface{}
	userID string
	closed bool
}

// Message represents the WebSocket message protocol.
type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
	Room string      `json:"room,omitempty"`
}

// Connector implements a bidirectional WebSocket connector.
type Connector struct {
	name         string
	port         int
	host         string
	path         string
	pingInterval time.Duration
	pongTimeout  time.Duration
	logger       *slog.Logger

	upgrader websocket.Upgrader
	server   *http.Server

	mu       sync.RWMutex
	clients  map[*Client]bool
	rooms    map[string]map[*Client]bool
	handlers map[string]HandlerFunc
	started  bool

	// Debug throttling: single-message processing when debugger is connected
	debugGate connector.DebugGate
}

// New creates a new WebSocket connector.
func New(name string, config *Config, logger *slog.Logger) *Connector {
	if logger == nil {
		logger = slog.Default()
	}

	return &Connector{
		name:         name,
		port:         config.Port,
		host:         config.Host,
		path:         config.Path,
		pingInterval: config.PingInterval,
		pongTimeout:  config.PongTimeout,
		logger:       logger,
		clients:      make(map[*Client]bool),
		rooms:        make(map[string]map[*Client]bool),
		handlers:     make(map[string]HandlerFunc),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins (configure for production)
			},
		},
	}
}

// Name returns the connector name.
func (c *Connector) Name() string { return c.name }

// Type returns the connector type.
func (c *Connector) Type() string { return "websocket" }

// Connect is a no-op for WebSocket connector (connection happens on Start).
func (c *Connector) Connect(ctx context.Context) error { return nil }

// Close stops the WebSocket server and disconnects all clients.
func (c *Connector) Close(ctx context.Context) error {
	c.mu.Lock()
	clients := make([]*Client, 0, len(c.clients))
	for client := range c.clients {
		clients = append(clients, client)
	}
	c.mu.Unlock()

	for _, client := range clients {
		c.removeClient(client)
	}

	if c.server != nil {
		return c.server.Shutdown(ctx)
	}
	return nil
}

// Health checks if the connector is healthy.
func (c *Connector) Health(ctx context.Context) error {
	if !c.started {
		return fmt.Errorf("websocket server not started")
	}
	return nil
}

// RegisterRoute registers a flow handler for an operation (message, connect, disconnect).
// This implements the runtime.RouteRegistrar interface.
// Multiple flows can register for the same operation (fan-out): the first handler
// returns the response, additional handlers run concurrently as fire-and-forget.
func (c *Connector) RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.handlers[operation]; ok {
		c.handlers[operation] = HandlerFunc(connector.ChainRequestResponse(
			connector.HandlerFunc(existing),
			connector.HandlerFunc(handler),
			c.logger,
		))
		c.logger.Info("fan-out: multiple flows registered", "operation", operation)
	} else {
		c.handlers[operation] = handler
	}
}

// SetDebugMode enables or disables single-message debug throttling.
func (c *Connector) SetDebugMode(enabled bool) {
	c.debugGate.SetEnabled(enabled)
	if enabled {
		c.logger.Info("debug mode enabled: single-message processing", "name", c.name)
	} else {
		c.logger.Info("debug mode disabled: concurrent processing restored", "name", c.name)
	}
}

// AllowOne permits exactly one message through the debug gate.
func (c *Connector) AllowOne() {
	c.debugGate.Allow()
}

// SourceInfo returns the connector type and source info for IDE display.
func (c *Connector) SourceInfo() (string, string) {
	return "websocket", c.name
}

// Start starts the WebSocket HTTP server.
func (c *Connector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("websocket server already started")
	}

	mux := http.NewServeMux()
	mux.HandleFunc(c.path, c.handleWebSocket)

	c.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", c.host, c.port),
		Handler: mux,
	}

	go func() {
		c.logger.Info("websocket server started",
			"host", c.host,
			"port", c.port,
			"path", c.path,
		)
		if err := c.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			c.logger.Error("websocket server error", "error", err)
		}
	}()

	c.started = true
	return nil
}

// Write sends data to WebSocket clients based on the operation.
// Supported operations: "broadcast", "send_to_room", "send_to_user".
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	operation := data.Operation
	payload := data.Payload

	switch operation {
	case "broadcast":
		c.broadcast(payload)
	case "send_to_room":
		room := data.Target
		if room == "" {
			return nil, fmt.Errorf("send_to_room requires a target room")
		}
		c.sendToRoom(room, payload)
	case "send_to_user":
		userID := ""
		if data.Filters != nil {
			if uid, ok := data.Filters["user_id"].(string); ok {
				userID = uid
			}
		}
		if userID == "" {
			return nil, fmt.Errorf("send_to_user requires user_id in filters")
		}
		c.sendToUser(userID, payload)
	default:
		// Default to broadcast
		c.broadcast(payload)
	}

	return &connector.Result{Affected: 1}, nil
}

// handleWebSocket upgrades HTTP to WebSocket and starts the client read pump.
func (c *Connector) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		c.logger.Error("websocket upgrade failed", "error", err)
		return
	}

	client := &Client{
		conn:   conn,
		rooms:  make(map[string]bool),
		params: make(map[string]interface{}),
	}

	c.mu.Lock()
	c.clients[client] = true
	c.mu.Unlock()

	c.logger.Debug("websocket client connected", "remote_addr", r.RemoteAddr)

	// Notify connect handler
	if handler, ok := c.getHandler("connect"); ok {
		handler(r.Context(), map[string]interface{}{
			"event":       "connect",
			"remote_addr": r.RemoteAddr,
		})
	}

	go c.readPump(client)
}

// readPump reads messages from a client and dispatches to handlers.
func (c *Connector) readPump(client *Client) {
	defer func() {
		c.removeClient(client)
	}()

	conn := client.conn
	conn.SetReadLimit(512 * 1024) // 512KB max message size

	// Configure pong handler for keepalive
	if c.pongTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(c.pongTimeout))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(c.pongTimeout))
			return nil
		})
	}

	// Start ping ticker
	if c.pingInterval > 0 {
		go c.pingLoop(client)
	}

	for {
		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure, websocket.CloseAbnormalClosure) {
				c.logger.Debug("websocket read error", "error", err)
			}
			return
		}

		// Reset read deadline on message
		if c.pongTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(c.pongTimeout))
		}

		var msg Message
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			c.sendError(client, "invalid message format")
			continue
		}

		c.handleClientMessage(client, &msg)
	}
}

// pingLoop sends periodic ping frames to the client.
func (c *Connector) pingLoop(client *Client) {
	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()

	for range ticker.C {
		client.mu.Lock()
		if client.closed {
			client.mu.Unlock()
			return
		}
		client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err := client.conn.WriteMessage(websocket.PingMessage, nil)
		client.mu.Unlock()

		if err != nil {
			return
		}
	}
}

// handleClientMessage processes an incoming client message.
func (c *Connector) handleClientMessage(client *Client, msg *Message) {
	switch msg.Type {
	case "join_room":
		if msg.Room != "" {
			c.joinRoom(client, msg.Room)
		}
	case "leave_room":
		if msg.Room != "" {
			c.leaveRoom(client, msg.Room)
		}
	case "message":
		handler, ok := c.getHandler("message")
		if !ok {
			return
		}

		input := make(map[string]interface{})
		input["event"] = "message"

		// Flatten data into input
		switch d := msg.Data.(type) {
		case map[string]interface{}:
			for k, v := range d {
				input[k] = v
			}
		default:
			input["data"] = msg.Data
		}

		// Add client metadata
		if client.userID != "" {
			input["user_id"] = client.userID
		}

		c.debugGate.Acquire()
		result, err := handler(context.Background(), input)
		c.debugGate.Release()
		if err != nil {
			c.sendError(client, err.Error())
			return
		}

		// Send result back to client if non-nil
		if result != nil {
			c.sendToClient(client, map[string]interface{}{
				"type": "message",
				"data": result,
			})
		}
	default:
		// Try to dispatch to a custom handler matching the type
		handler, ok := c.getHandler(msg.Type)
		if !ok {
			c.sendError(client, fmt.Sprintf("unknown message type: %s", msg.Type))
			return
		}

		input := map[string]interface{}{
			"event": msg.Type,
			"data":  msg.Data,
		}
		if msg.Room != "" {
			input["room"] = msg.Room
		}

		c.debugGate.Acquire()
		handler(context.Background(), input)
		c.debugGate.Release()
	}
}

// broadcast sends data to all connected clients.
func (c *Connector) broadcast(data interface{}) {
	msg := map[string]interface{}{
		"type": "message",
		"data": data,
	}

	c.mu.RLock()
	clients := make([]*Client, 0, len(c.clients))
	for client := range c.clients {
		clients = append(clients, client)
	}
	c.mu.RUnlock()

	for _, client := range clients {
		c.sendToClient(client, msg)
	}
}

// sendToRoom sends data to all clients in a specific room.
func (c *Connector) sendToRoom(room string, data interface{}) {
	msg := map[string]interface{}{
		"type": "message",
		"data": data,
		"room": room,
	}

	c.mu.RLock()
	roomClients := c.rooms[room]
	clients := make([]*Client, 0, len(roomClients))
	for client := range roomClients {
		clients = append(clients, client)
	}
	c.mu.RUnlock()

	for _, client := range clients {
		c.sendToClient(client, msg)
	}
}

// sendToUser sends data to a specific user.
func (c *Connector) sendToUser(userID string, data interface{}) {
	msg := map[string]interface{}{
		"type": "message",
		"data": data,
	}

	c.mu.RLock()
	clients := make([]*Client, 0)
	for client := range c.clients {
		if client.userID == userID {
			clients = append(clients, client)
		}
	}
	c.mu.RUnlock()

	for _, client := range clients {
		c.sendToClient(client, msg)
	}
}

// sendToClient sends a JSON message to a specific client.
func (c *Connector) sendToClient(client *Client, data interface{}) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closed {
		return
	}

	client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := client.conn.WriteJSON(data); err != nil {
		c.logger.Debug("failed to send to client", "error", err)
	}
}

// sendError sends an error message to a client.
func (c *Connector) sendError(client *Client, message string) {
	c.sendToClient(client, map[string]interface{}{
		"type":    "error",
		"message": message,
	})
}

// joinRoom adds a client to a room.
func (c *Connector) joinRoom(client *Client, room string) {
	c.mu.Lock()
	if c.rooms[room] == nil {
		c.rooms[room] = make(map[*Client]bool)
	}
	c.rooms[room][client] = true
	c.mu.Unlock()

	client.mu.Lock()
	client.rooms[room] = true
	client.mu.Unlock()

	c.logger.Debug("client joined room", "room", room)
}

// leaveRoom removes a client from a room.
func (c *Connector) leaveRoom(client *Client, room string) {
	c.mu.Lock()
	if roomClients, ok := c.rooms[room]; ok {
		delete(roomClients, client)
		if len(roomClients) == 0 {
			delete(c.rooms, room)
		}
	}
	c.mu.Unlock()

	client.mu.Lock()
	delete(client.rooms, room)
	client.mu.Unlock()

	c.logger.Debug("client left room", "room", room)
}

// removeClient cleans up a client on disconnect.
func (c *Connector) removeClient(client *Client) {
	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()
		return
	}
	client.closed = true
	rooms := make([]string, 0, len(client.rooms))
	for room := range client.rooms {
		rooms = append(rooms, room)
	}
	client.mu.Unlock()

	// Remove from all rooms
	c.mu.Lock()
	for _, room := range rooms {
		if roomClients, ok := c.rooms[room]; ok {
			delete(roomClients, client)
			if len(roomClients) == 0 {
				delete(c.rooms, room)
			}
		}
	}
	delete(c.clients, client)
	c.mu.Unlock()

	client.conn.Close()

	// Notify disconnect handler
	if handler, ok := c.getHandler("disconnect"); ok {
		handler(context.Background(), map[string]interface{}{
			"event": "disconnect",
		})
	}

	c.logger.Debug("websocket client disconnected")
}

// getHandler returns a handler for the given operation, thread-safe.
func (c *Connector) getHandler(operation string) (HandlerFunc, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	h, ok := c.handlers[operation]
	return h, ok
}

// ClientCount returns the number of connected clients.
func (c *Connector) ClientCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.clients)
}

// RoomCount returns the number of active rooms.
func (c *Connector) RoomCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.rooms)
}

// RoomClientCount returns the number of clients in a specific room.
func (c *Connector) RoomClientCount(room string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.rooms[room])
}
