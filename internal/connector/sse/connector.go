// Package sse provides a Server-Sent Events connector for unidirectional server-to-client push.
// It supports broadcast, room-based messaging, and per-user targeting over standard HTTP.
package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// HandlerFunc is a function that handles a flow request.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// Client represents a connected SSE client.
type Client struct {
	id      string
	w       http.ResponseWriter
	flusher http.Flusher
	done    chan struct{}
	rooms   map[string]bool
	userID  string
	mu      sync.Mutex
	closed  bool
}

// Connector implements a unidirectional SSE connector (server-to-client push).
type Connector struct {
	name              string
	port              int
	host              string
	path              string
	heartbeatInterval time.Duration
	corsOrigins       []string
	logger            *slog.Logger
	server            *http.Server

	mu       sync.RWMutex
	clients  map[string]*Client           // clientID -> Client
	rooms    map[string]map[string]*Client // room -> clientID -> Client
	handlers map[string]HandlerFunc
	eventID  uint64 // atomic counter for event IDs
	clientID uint64 // atomic counter for client IDs
	started  bool
}

// New creates a new SSE connector.
func New(name string, config *Config, logger *slog.Logger) *Connector {
	if logger == nil {
		logger = slog.Default()
	}

	return &Connector{
		name:              name,
		port:              config.Port,
		host:              config.Host,
		path:              config.Path,
		heartbeatInterval: config.HeartbeatInterval,
		corsOrigins:       config.CORSOrigins,
		logger:            logger,
		clients:           make(map[string]*Client),
		rooms:             make(map[string]map[string]*Client),
		handlers:          make(map[string]HandlerFunc),
	}
}

// Name returns the connector name.
func (c *Connector) Name() string { return c.name }

// Type returns the connector type.
func (c *Connector) Type() string { return "sse" }

// Connect is a no-op for SSE connector (connection happens on Start).
func (c *Connector) Connect(ctx context.Context) error { return nil }

// Close stops the SSE server and disconnects all clients.
func (c *Connector) Close(ctx context.Context) error {
	c.mu.Lock()
	clients := make([]*Client, 0, len(c.clients))
	for _, client := range c.clients {
		clients = append(clients, client)
	}
	c.mu.Unlock()

	for _, client := range clients {
		c.removeClient(client.id)
	}

	if c.server != nil {
		return c.server.Shutdown(ctx)
	}
	return nil
}

// Health checks if the connector is healthy.
func (c *Connector) Health(ctx context.Context) error {
	if !c.started {
		return fmt.Errorf("sse server not started")
	}
	return nil
}

// RegisterRoute registers a flow handler for an operation (connect, disconnect).
// This implements the runtime.RouteRegistrar interface.
func (c *Connector) RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[operation] = handler
}

// Start starts the SSE HTTP server.
func (c *Connector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("sse server already started")
	}

	mux := http.NewServeMux()
	mux.HandleFunc(c.path, c.handleSSE)

	c.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", c.host, c.port),
		Handler: mux,
	}

	go func() {
		c.logger.Info("sse server started",
			"host", c.host,
			"port", c.port,
			"path", c.path,
		)
		if err := c.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			c.logger.Error("sse server error", "error", err)
		}
	}()

	c.started = true
	return nil
}

// Write sends data to SSE clients based on the operation.
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

// handleSSE handles incoming SSE connections.
func (c *Connector) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Check that the response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Set CORS headers
	if len(c.corsOrigins) > 0 {
		origin := r.Header.Get("Origin")
		for _, allowed := range c.corsOrigins {
			if allowed == "*" || allowed == origin {
				w.Header().Set("Access-Control-Allow-Origin", allowed)
				break
			}
		}
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create client
	id := fmt.Sprintf("sse_%d", atomic.AddUint64(&c.clientID, 1))
	client := &Client{
		id:      id,
		w:       w,
		flusher: flusher,
		done:    make(chan struct{}),
		rooms:   make(map[string]bool),
	}

	// Auto-join rooms from query params
	if room := r.URL.Query().Get("room"); room != "" {
		client.rooms[room] = true
	}
	if rooms := r.URL.Query().Get("rooms"); rooms != "" {
		for _, room := range strings.Split(rooms, ",") {
			room = strings.TrimSpace(room)
			if room != "" {
				client.rooms[room] = true
			}
		}
	}

	// Extract user_id from query params
	if userID := r.URL.Query().Get("user_id"); userID != "" {
		client.userID = userID
	}

	// Register client
	c.mu.Lock()
	c.clients[id] = client
	for room := range client.rooms {
		if c.rooms[room] == nil {
			c.rooms[room] = make(map[string]*Client)
		}
		c.rooms[room][id] = client
	}
	c.mu.Unlock()

	c.logger.Debug("sse client connected", "id", id, "remote_addr", r.RemoteAddr)

	// Flush headers to establish the connection (synchronized with sendEvent)
	client.mu.Lock()
	flusher.Flush()
	client.mu.Unlock()

	// Notify connect handler
	if handler, ok := c.getHandler("connect"); ok {
		handler(r.Context(), map[string]interface{}{
			"event":       "connect",
			"client_id":   id,
			"remote_addr": r.RemoteAddr,
		})
	}

	// Start heartbeat if configured
	if c.heartbeatInterval > 0 {
		go c.heartbeatLoop(client)
	}

	// Block until client disconnects
	select {
	case <-client.done:
	case <-r.Context().Done():
	}

	c.removeClient(id)
}

// sendEvent writes an SSE event to a client.
func (c *Connector) sendEvent(client *Client, data interface{}) {
	client.mu.Lock()
	defer client.mu.Unlock()

	if client.closed {
		return
	}

	id := atomic.AddUint64(&c.eventID, 1)
	jsonData, err := json.Marshal(data)
	if err != nil {
		c.logger.Debug("failed to marshal sse event", "error", err)
		return
	}

	fmt.Fprintf(client.w, "id: %d\nevent: message\ndata: %s\n\n", id, jsonData)
	client.flusher.Flush()
}

// heartbeatLoop sends periodic keepalive comments.
func (c *Connector) heartbeatLoop(client *Client) {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			client.mu.Lock()
			if client.closed {
				client.mu.Unlock()
				return
			}
			fmt.Fprintf(client.w, ": keepalive\n\n")
			client.flusher.Flush()
			client.mu.Unlock()
		case <-client.done:
			return
		}
	}
}

// broadcast sends data to all connected clients.
func (c *Connector) broadcast(data interface{}) {
	c.mu.RLock()
	clients := make([]*Client, 0, len(c.clients))
	for _, client := range c.clients {
		clients = append(clients, client)
	}
	c.mu.RUnlock()

	for _, client := range clients {
		c.sendEvent(client, data)
	}
}

// sendToRoom sends data to all clients in a specific room.
func (c *Connector) sendToRoom(room string, data interface{}) {
	c.mu.RLock()
	roomClients := c.rooms[room]
	clients := make([]*Client, 0, len(roomClients))
	for _, client := range roomClients {
		clients = append(clients, client)
	}
	c.mu.RUnlock()

	for _, client := range clients {
		c.sendEvent(client, data)
	}
}

// sendToUser sends data to all clients of a specific user.
func (c *Connector) sendToUser(userID string, data interface{}) {
	c.mu.RLock()
	clients := make([]*Client, 0)
	for _, client := range c.clients {
		if client.userID == userID {
			clients = append(clients, client)
		}
	}
	c.mu.RUnlock()

	for _, client := range clients {
		c.sendEvent(client, data)
	}
}

// removeClient cleans up a client on disconnect.
func (c *Connector) removeClient(id string) {
	c.mu.Lock()
	client, ok := c.clients[id]
	if !ok {
		c.mu.Unlock()
		return
	}

	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()
		c.mu.Unlock()
		return
	}
	client.closed = true
	close(client.done)
	client.mu.Unlock()

	// Remove from all rooms
	for room := range client.rooms {
		if roomClients, ok := c.rooms[room]; ok {
			delete(roomClients, id)
			if len(roomClients) == 0 {
				delete(c.rooms, room)
			}
		}
	}
	delete(c.clients, id)
	c.mu.Unlock()

	// Notify disconnect handler
	if handler, ok := c.getHandler("disconnect"); ok {
		handler(context.Background(), map[string]interface{}{
			"event":     "disconnect",
			"client_id": id,
		})
	}

	c.logger.Debug("sse client disconnected", "id", id)
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
