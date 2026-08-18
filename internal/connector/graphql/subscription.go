package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
)

// SubscriptionManager manages GraphQL subscriptions over WebSocket.
type SubscriptionManager struct {
	schema   *graphql.Schema
	logger   *slog.Logger
	upgrader websocket.Upgrader
	mu       sync.RWMutex
	clients  map[*wsClient]bool
	pubsub   *PubSub

	// keepAlive is how often an idle connection is pinged, and idleTimeout is
	// how long one may go without answering before it is dropped.
	keepAlive   time.Duration
	idleTimeout time.Duration
}

// wsClient represents a WebSocket client connection.
type wsClient struct {
	conn             *websocket.Conn
	manager          *SubscriptionManager
	subscriptions    map[string]context.CancelFunc
	mu               sync.Mutex
	closed           bool
	connectionParams map[string]interface{} // Params from connection_init payload
}

// graphql-ws protocol message types
const (
	// Client -> Server
	msgConnectionInit = "connection_init"
	msgPing           = "ping"
	msgPong           = "pong"
	msgSubscribe      = "subscribe"
	msgComplete       = "complete"

	// Server -> Client
	msgConnectionAck = "connection_ack"
	msgNext          = "next"
	msgError         = "error"
	msgComplete_     = "complete"
)

// wsMessage represents a graphql-ws protocol message.
type wsMessage struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// subscribePayload is the payload for subscribe messages.
type subscribePayload struct {
	Query         string                 `json:"query"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
	OperationName string                 `json:"operationName,omitempty"`
	Extensions    map[string]interface{} `json:"extensions,omitempty"`
}

// NewSubscriptionManager creates a new subscription manager.
func NewSubscriptionManager(schema *graphql.Schema, logger *slog.Logger) *SubscriptionManager {
	return NewSubscriptionManagerWithTimings(schema, logger, 0, 0)
}

// NewSubscriptionManagerWithTimings creates a manager that pings idle
// connections and drops ones that stop answering.
//
// A subscription is idle by nature — waiting for something to happen is what
// it is for — and nothing kept the socket busy: the server answered a ping and
// never sent one, so `keep_alive_interval`, whose documented purpose is the
// ping period on an idle socket, did nothing. Worse, the read deadline was a
// hardcoded minute refreshed only by incoming messages, so a subscription
// whose client had nothing to say was dropped by this server after sixty
// seconds — even one that was actively delivering events, because those are
// writes and the deadline only watches reads.
func NewSubscriptionManagerWithTimings(schema *graphql.Schema, logger *slog.Logger, keepAlive, idleTimeout time.Duration) *SubscriptionManager {
	if logger == nil {
		logger = slog.Default()
	}
	if keepAlive <= 0 {
		keepAlive = 30 * time.Second
	}
	if idleTimeout <= 0 {
		idleTimeout = 60 * time.Second
	}
	// A connection must be given time to answer: dropping it before a ping
	// could have come back would close every idle subscription on schedule.
	if idleTimeout <= keepAlive {
		idleTimeout = keepAlive * 2
	}

	return &SubscriptionManager{
		schema:      schema,
		logger:      logger,
		keepAlive:   keepAlive,
		idleTimeout: idleTimeout,
		clients:     make(map[*wsClient]bool),
		pubsub:      NewPubSub(),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins (configure for production)
			},
			Subprotocols: []string{"graphql-transport-ws"},
		},
	}
}

// Handler returns an HTTP handler for WebSocket connections.
func (m *SubscriptionManager) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Upgrade to WebSocket
		conn, err := m.upgrader.Upgrade(w, r, nil)
		if err != nil {
			m.logger.Error("websocket upgrade failed", "error", err)
			return
		}

		client := &wsClient{
			conn:          conn,
			manager:       m,
			subscriptions: make(map[string]context.CancelFunc),
		}

		m.mu.Lock()
		m.clients[client] = true
		m.mu.Unlock()

		m.logger.Debug("websocket client connected",
			"remote_addr", r.RemoteAddr)

		go client.readPump()
		go client.keepAlivePump()
	}
}

// readPump handles incoming messages from the client.
func (c *wsClient) readPump() {
	defer func() {
		c.close()
	}()

	idleTimeout := c.manager.idleTimeout
	if idleTimeout <= 0 {
		idleTimeout = 60 * time.Second
	}

	c.conn.SetReadLimit(512 * 1024) // 512KB max message size
	_ = c.conn.SetReadDeadline(time.Now().Add(idleTimeout))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(idleTimeout))
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.manager.logger.Debug("websocket read error", "error", err)
			}
			return
		}

		_ = c.conn.SetReadDeadline(time.Now().Add(idleTimeout))

		var msg wsMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			c.manager.logger.Debug("invalid message format", "error", err)
			continue
		}

		c.handleMessage(&msg)
	}
}

// keepAlivePump pings an idle connection so it stays open.
//
// The ping is the protocol's own, which is what the client libraries answer,
// and it doubles as the thing that keeps every box between here and the client
// from closing a socket it believes to be dead. A subscription can sit quiet
// for hours and is expected to.
func (c *wsClient) keepAlivePump() {
	interval := c.manager.keepAlive
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		closed := c.closed
		c.mu.Unlock()
		if closed {
			return
		}

		// send takes the same lock and gives up quietly on a closed
		// connection, so a client that went away ends this loop on the next
		// tick rather than leaving it running.
		c.send(&wsMessage{Type: msgPing})
	}
}

// handleMessage processes incoming graphql-ws messages.
func (c *wsClient) handleMessage(msg *wsMessage) {
	switch msg.Type {
	case msgConnectionInit:
		// Parse connection params (may contain auth tokens, user info, etc.)
		if msg.Payload != nil {
			var params map[string]interface{}
			if err := json.Unmarshal(msg.Payload, &params); err == nil {
				c.connectionParams = params
			}
		}
		// Acknowledge connection
		c.send(&wsMessage{Type: msgConnectionAck})

	case msgPing:
		c.send(&wsMessage{Type: msgPong})

	case msgPong:
		// Ignore pong

	case msgSubscribe:
		c.handleSubscribe(msg)

	case msgComplete:
		c.handleComplete(msg)

	default:
		c.manager.logger.Debug("unknown message type", "type", msg.Type)
	}
}

// handleSubscribe starts a new subscription.
func (c *wsClient) handleSubscribe(msg *wsMessage) {
	if msg.ID == "" {
		c.sendError(msg.ID, fmt.Errorf("subscription ID is required"))
		return
	}

	var payload subscribePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.sendError(msg.ID, fmt.Errorf("invalid payload: %w", err))
		return
	}

	// Check if subscription already exists
	c.mu.Lock()
	if _, exists := c.subscriptions[msg.ID]; exists {
		c.mu.Unlock()
		c.sendError(msg.ID, fmt.Errorf("subscriber for %s already exists", msg.ID))
		return
	}

	// Create cancellable context for this subscription, with connection params
	ctx := context.Background()
	if len(c.connectionParams) > 0 {
		ctx = context.WithValue(ctx, contextKeyConnectionParams{}, c.connectionParams)
	}
	ctx, cancel := context.WithCancel(ctx)
	c.subscriptions[msg.ID] = cancel
	c.mu.Unlock()

	// Execute subscription
	go c.executeSubscription(ctx, msg.ID, &payload)
}

// executeSubscription runs a GraphQL subscription.
func (c *wsClient) executeSubscription(ctx context.Context, id string, payload *subscribePayload) {
	defer func() {
		c.mu.Lock()
		delete(c.subscriptions, id)
		c.mu.Unlock()
	}()

	// Execute the GraphQL subscription
	params := graphql.Params{
		Schema:         *c.manager.schema,
		RequestString:  payload.Query,
		VariableValues: payload.Variables,
		OperationName:  payload.OperationName,
		Context:        ctx,
	}

	// For subscriptions, we need to use Subscribe instead of Do
	resultChannel := graphql.Subscribe(params)

	for {
		select {
		case <-ctx.Done():
			c.send(&wsMessage{ID: id, Type: msgComplete_})
			return

		case result, ok := <-resultChannel:
			if !ok {
				c.send(&wsMessage{ID: id, Type: msgComplete_})
				return
			}

			if len(result.Errors) > 0 {
				c.sendErrors(id, result.Errors)
				continue
			}

			// Send next result
			data, _ := json.Marshal(result)
			c.send(&wsMessage{
				ID:      id,
				Type:    msgNext,
				Payload: data,
			})
		}
	}
}

// handleComplete stops a subscription.
func (c *wsClient) handleComplete(msg *wsMessage) {
	c.mu.Lock()
	if cancel, exists := c.subscriptions[msg.ID]; exists {
		cancel()
		delete(c.subscriptions, msg.ID)
	}
	c.mu.Unlock()
}

// send sends a message to the client.
func (c *wsClient) send(msg *wsMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		c.manager.logger.Error("failed to marshal message", "error", err)
		return
	}

	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		c.manager.logger.Debug("failed to send message", "error", err)
	}
}

// sendError sends an error message.
func (c *wsClient) sendError(id string, err error) {
	errPayload, _ := json.Marshal([]map[string]string{
		{"message": err.Error()},
	})
	c.send(&wsMessage{
		ID:      id,
		Type:    msgError,
		Payload: errPayload,
	})
}

// sendErrors sends multiple GraphQL errors.
func (c *wsClient) sendErrors(id string, errors []gqlerrors.FormattedError) {
	errPayload, _ := json.Marshal(errors)
	c.send(&wsMessage{
		ID:      id,
		Type:    msgError,
		Payload: errPayload,
	})
}

// close closes the client connection.
func (c *wsClient) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true

	// Cancel all subscriptions
	for _, cancel := range c.subscriptions {
		cancel()
	}
	c.subscriptions = nil
	c.mu.Unlock()

	c.conn.Close()

	c.manager.mu.Lock()
	delete(c.manager.clients, c)
	c.manager.mu.Unlock()

	c.manager.logger.Debug("websocket client disconnected")
}

// contextKeyConnectionParams is the context key for WebSocket connection parameters.
type contextKeyConnectionParams struct{}

// ConnectionParamsFromContext extracts connection parameters from context.
func ConnectionParamsFromContext(ctx context.Context) map[string]interface{} {
	if params, ok := ctx.Value(contextKeyConnectionParams{}).(map[string]interface{}); ok {
		return params
	}
	return nil
}

// filteredSubscriber wraps a channel with an optional filter function.
type filteredSubscriber struct {
	ch     chan interface{}
	filter func(data interface{}) bool // nil means no filter (accept all)
}

// PubSub provides a simple publish/subscribe mechanism for subscriptions.
type PubSub struct {
	mu          sync.RWMutex
	subscribers map[string][]*filteredSubscriber
}

// NewPubSub creates a new PubSub instance.
func NewPubSub() *PubSub {
	return &PubSub{
		subscribers: make(map[string][]*filteredSubscriber),
	}
}

// Subscribe creates a subscription to a topic (no filter).
func (p *PubSub) Subscribe(topic string) chan interface{} {
	return p.SubscribeWithFilter(topic, nil)
}

// SubscribeWithFilter creates a subscription with an optional filter function.
// If filter is nil, all messages are delivered. If filter returns false, the message is skipped.
func (p *PubSub) SubscribeWithFilter(topic string, filter func(data interface{}) bool) chan interface{} {
	ch := make(chan interface{}, 10)

	p.mu.Lock()
	p.subscribers[topic] = append(p.subscribers[topic], &filteredSubscriber{
		ch:     ch,
		filter: filter,
	})
	p.mu.Unlock()

	return ch
}

// Unsubscribe removes a subscription.
func (p *PubSub) Unsubscribe(topic string, ch chan interface{}) {
	p.mu.Lock()
	defer p.mu.Unlock()

	subs := p.subscribers[topic]
	for i, sub := range subs {
		if sub.ch == ch {
			p.subscribers[topic] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
}

// Publish sends a message to all subscribers of a topic.
// Messages are only delivered to subscribers whose filter (if any) returns true.
func (p *PubSub) Publish(topic string, data interface{}) {
	p.mu.RLock()
	subs := make([]*filteredSubscriber, len(p.subscribers[topic]))
	copy(subs, p.subscribers[topic])
	p.mu.RUnlock()

	for _, sub := range subs {
		// Apply filter if present
		if sub.filter != nil && !sub.filter(data) {
			continue
		}
		select {
		case sub.ch <- data:
		default:
			// Channel full, skip
		}
	}
}

// GetPubSub returns the PubSub instance for external publishing.
func (m *SubscriptionManager) GetPubSub() *PubSub {
	return m.pubsub
}

// Broadcast sends data to all subscribers of a topic.
func (m *SubscriptionManager) Broadcast(topic string, data interface{}) {
	m.pubsub.Publish(topic, data)
}

// Close closes all client connections.
func (m *SubscriptionManager) Close() {
	m.mu.Lock()
	clients := make([]*wsClient, 0, len(m.clients))
	for client := range m.clients {
		clients = append(clients, client)
	}
	m.mu.Unlock()

	for _, client := range clients {
		client.close()
	}
}
