package tcp

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/mycel-labs/mycel/internal/connector"
)

// ClientConnector is a TCP client that connects to remote servers.
type ClientConnector struct {
	name     string
	host     string
	port     int
	protocol string
	codec    Codec
	logger   *slog.Logger

	// Connection pool
	pool     chan net.Conn
	poolSize int

	// Timeouts
	connectTimeout time.Duration
	readTimeout    time.Duration
	writeTimeout   time.Duration
	idleTimeout    time.Duration

	// Retry configuration
	retryCount int
	retryDelay time.Duration

	// TLS configuration
	tlsConfig *tls.Config

	// State
	mu        sync.RWMutex
	connected bool
}

// ClientOption configures a ClientConnector.
type ClientOption func(*ClientConnector)

// WithClientLogger sets the logger for the client.
func WithClientLogger(logger *slog.Logger) ClientOption {
	return func(c *ClientConnector) {
		c.logger = logger
	}
}

// WithPoolSize sets the connection pool size.
func WithPoolSize(size int) ClientOption {
	return func(c *ClientConnector) {
		c.poolSize = size
	}
}

// WithClientTimeouts sets the various timeouts.
func WithClientTimeouts(connect, read, write, idle time.Duration) ClientOption {
	return func(c *ClientConnector) {
		c.connectTimeout = connect
		c.readTimeout = read
		c.writeTimeout = write
		c.idleTimeout = idle
	}
}

// WithRetry sets the retry configuration.
func WithRetry(count int, delay time.Duration) ClientOption {
	return func(c *ClientConnector) {
		c.retryCount = count
		c.retryDelay = delay
	}
}

// WithClientTLS enables TLS with the given configuration.
func WithClientTLS(config *tls.Config) ClientOption {
	return func(c *ClientConnector) {
		c.tlsConfig = config
	}
}

// NewClient creates a new TCP client connector.
func NewClient(name, host string, port int, protocol string, opts ...ClientOption) (*ClientConnector, error) {
	codec, err := NewCodec(protocol)
	if err != nil {
		return nil, fmt.Errorf("failed to create codec: %w", err)
	}

	c := &ClientConnector{
		name:           name,
		host:           host,
		port:           port,
		protocol:       protocol,
		codec:          codec,
		poolSize:       10,
		connectTimeout: 10 * time.Second,
		readTimeout:    30 * time.Second,
		writeTimeout:   30 * time.Second,
		idleTimeout:    5 * time.Minute,
		retryCount:     3,
		retryDelay:     time.Second,
		logger:         slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// Name returns the connector name.
func (c *ClientConnector) Name() string {
	return c.name
}

// Type returns "tcp".
func (c *ClientConnector) Type() string {
	return "tcp"
}

// Connect initializes the connection pool.
func (c *ClientConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	// Initialize pool
	c.pool = make(chan net.Conn, c.poolSize)
	c.connected = true

	c.logger.Info("TCP client initialized",
		"name", c.name,
		"remote", fmt.Sprintf("%s:%d", c.host, c.port),
		"protocol", c.protocol,
		"pool_size", c.poolSize,
	)

	return nil
}

// Close closes all connections in the pool.
func (c *ClientConnector) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	c.connected = false

	// Drain and close all connections
	close(c.pool)
	for conn := range c.pool {
		conn.Close()
	}

	return nil
}

// Health checks if the client can connect to the server.
func (c *ClientConnector) Health(ctx context.Context) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	c.returnConn(conn)
	return nil
}

// Read sends a request and waits for a response (request-response pattern).
func (c *ClientConnector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	// Build message from query
	msg := NewRequest(query.Target, query.Params)

	// Add filters to data
	if query.Filters != nil {
		if msg.Data == nil {
			msg.Data = make(map[string]interface{})
		}
		for k, v := range query.Filters {
			msg.Data[k] = v
		}
	}

	// Send and receive
	response, err := c.sendAndReceive(ctx, msg)
	if err != nil {
		return nil, err
	}

	if response.IsError() {
		return nil, fmt.Errorf("remote error: %s", response.Error)
	}

	// Convert response to Result
	result := &connector.Result{
		Rows: []map[string]interface{}{response.Data},
		Metadata: map[string]interface{}{
			"request_id": msg.ID,
		},
	}

	return result, nil
}

// Write sends a message to the server (supports both fire-and-forget and request-response).
func (c *ClientConnector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	// Build message
	msg := NewRequest(data.Target, data.Payload)

	// Check operation mode
	mode := "request_response"
	if m, ok := data.Params["mode"].(string); ok {
		mode = m
	}

	if mode == "fire_and_forget" {
		// Fire and forget
		if err := c.sendOnly(ctx, msg); err != nil {
			return nil, err
		}

		return &connector.Result{
			Affected: 1,
			Metadata: map[string]interface{}{
				"request_id": msg.ID,
				"mode":       "fire_and_forget",
			},
		}, nil
	}

	// Request-response
	response, err := c.sendAndReceive(ctx, msg)
	if err != nil {
		return nil, err
	}

	if response.IsError() {
		return nil, fmt.Errorf("remote error: %s", response.Error)
	}

	return &connector.Result{
		Affected: 1,
		Rows:     []map[string]interface{}{response.Data},
		Metadata: map[string]interface{}{
			"request_id": msg.ID,
			"mode":       "request_response",
		},
	}, nil
}

// sendAndReceive sends a message and waits for a response.
func (c *ClientConnector) sendAndReceive(ctx context.Context, msg *Message) (*Message, error) {
	var lastErr error

	for attempt := 0; attempt <= c.retryCount; attempt++ {
		if attempt > 0 {
			c.logger.Debug("retrying request",
				"attempt", attempt,
				"max", c.retryCount,
				"delay", c.retryDelay,
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.retryDelay):
			}
		}

		response, err := c.doSendAndReceive(ctx, msg)
		if err == nil {
			return response, nil
		}

		lastErr = err
		c.logger.Debug("request failed", "attempt", attempt, "error", err)
	}

	return nil, fmt.Errorf("all retries failed: %w", lastErr)
}

// doSendAndReceive performs a single send/receive operation.
func (c *ClientConnector) doSendAndReceive(ctx context.Context, msg *Message) (*Message, error) {
	conn, err := c.getConn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Use NestJS framer for NestJS protocol
	if c.protocol == "nestjs" {
		return c.doSendAndReceiveNestJS(conn, msg)
	}

	framer := NewFramer(conn, c.codec)

	// Set write deadline
	if c.writeTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}

	// Send message
	if err := framer.WriteMessage(msg); err != nil {
		conn.Close() // Don't return bad connection to pool
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	// Set read deadline
	if c.readTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(c.readTimeout))
	}

	// Read response
	var response Message
	if err := framer.ReadMessage(&response); err != nil {
		conn.Close() // Don't return bad connection to pool
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Return connection to pool
	c.returnConn(conn)

	return &response, nil
}

// doSendAndReceiveNestJS handles NestJS protocol communication.
func (c *ClientConnector) doSendAndReceiveNestJS(conn net.Conn, msg *Message) (*Message, error) {
	framer := NewNestJSFramer(conn)

	// Set write deadline
	if c.writeTimeout > 0 {
		framer.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}

	// Convert Mycel message to NestJS message
	nestMsg := FromMycelMessage(msg)

	// Send message
	if err := framer.WriteMessage(nestMsg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send NestJS message: %w", err)
	}

	// Set read deadline
	if c.readTimeout > 0 {
		framer.SetReadDeadline(time.Now().Add(c.readTimeout))
	}

	// Read response
	nestResp, err := framer.ReadMessage()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read NestJS response: %w", err)
	}

	// Return connection to pool
	c.returnConn(conn)

	// Convert NestJS response to Mycel message
	return nestResp.ToMycelMessage(), nil
}

// sendOnly sends a message without waiting for a response.
func (c *ClientConnector) sendOnly(ctx context.Context, msg *Message) error {
	var lastErr error

	for attempt := 0; attempt <= c.retryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.retryDelay):
			}
		}

		err := c.doSendOnly(ctx, msg)
		if err == nil {
			return nil
		}

		lastErr = err
	}

	return fmt.Errorf("all retries failed: %w", lastErr)
}

// doSendOnly performs a single send operation.
func (c *ClientConnector) doSendOnly(ctx context.Context, msg *Message) error {
	conn, err := c.getConn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}

	// Use NestJS framer for NestJS protocol
	if c.protocol == "nestjs" {
		return c.doSendOnlyNestJS(conn, msg)
	}

	framer := NewFramer(conn, c.codec)

	// Set write deadline
	if c.writeTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}

	// Send message
	if err := framer.WriteMessage(msg); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send message: %w", err)
	}

	// Return connection to pool
	c.returnConn(conn)

	return nil
}

// doSendOnlyNestJS handles NestJS protocol fire-and-forget.
func (c *ClientConnector) doSendOnlyNestJS(conn net.Conn, msg *Message) error {
	framer := NewNestJSFramer(conn)

	// Set write deadline
	if c.writeTimeout > 0 {
		framer.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}

	// Convert Mycel message to NestJS message
	nestMsg := FromMycelMessage(msg)

	// Send message
	if err := framer.WriteMessage(nestMsg); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send NestJS message: %w", err)
	}

	// Return connection to pool
	c.returnConn(conn)

	return nil
}

// getConn gets a connection from the pool or creates a new one.
func (c *ClientConnector) getConn(ctx context.Context) (net.Conn, error) {
	// Try to get from pool first
	select {
	case conn := <-c.pool:
		// Check if connection is still valid
		if c.isConnAlive(conn) {
			return conn, nil
		}
		conn.Close()
	default:
		// Pool empty, create new connection
	}

	return c.dial(ctx)
}

// returnConn returns a connection to the pool.
func (c *ClientConnector) returnConn(conn net.Conn) {
	select {
	case c.pool <- conn:
		// Returned to pool
	default:
		// Pool full, close connection
		conn.Close()
	}
}

// dial creates a new connection to the server.
func (c *ClientConnector) dial(ctx context.Context) (net.Conn, error) {
	addr := fmt.Sprintf("%s:%d", c.host, c.port)

	var conn net.Conn
	var err error

	// Create dialer with timeout
	dialer := &net.Dialer{
		Timeout: c.connectTimeout,
	}

	if c.tlsConfig != nil {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, c.tlsConfig)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	return conn, nil
}

// isConnAlive checks if a connection is still alive.
func (c *ClientConnector) isConnAlive(conn net.Conn) bool {
	// Set a very short read deadline
	conn.SetReadDeadline(time.Now().Add(time.Millisecond))

	// Try to read one byte (should timeout, not EOF)
	one := make([]byte, 1)
	_, err := conn.Read(one)

	// Reset deadline
	conn.SetReadDeadline(time.Time{})

	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			// Timeout is expected - connection is alive
			return true
		}
		// EOF or other error - connection is dead
		return false
	}

	// Got data unexpectedly - connection might still be valid
	// but we can't put this byte back, so close it
	return false
}

// Address returns the remote server address.
func (c *ClientConnector) Address() string {
	return fmt.Sprintf("%s:%d", c.host, c.port)
}
