package tcp

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

// HandlerFunc is the function signature for message handlers.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// ServerConnector is a TCP server that listens for incoming connections.
type ServerConnector struct {
	name     string
	host     string
	port     int
	protocol string
	codec    Codec
	logger   *slog.Logger

	// Network listener
	listener net.Listener

	// Message handlers by type
	mu       sync.RWMutex
	handlers map[string]HandlerFunc

	// Configuration
	maxConns     int
	readTimeout  time.Duration
	writeTimeout time.Duration

	// TLS configuration
	tlsConfig *tls.Config

	// Connection tracking
	connMu  sync.RWMutex
	conns   map[net.Conn]struct{}
	running bool

	// Shutdown coordination
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ServerOption configures a ServerConnector.
type ServerOption func(*ServerConnector)

// WithServerLogger sets the logger for the server.
func WithServerLogger(logger *slog.Logger) ServerOption {
	return func(s *ServerConnector) {
		s.logger = logger
	}
}

// WithMaxConnections sets the maximum number of concurrent connections.
func WithMaxConnections(max int) ServerOption {
	return func(s *ServerConnector) {
		s.maxConns = max
	}
}

// WithServerTimeouts sets read and write timeouts.
func WithServerTimeouts(read, write time.Duration) ServerOption {
	return func(s *ServerConnector) {
		s.readTimeout = read
		s.writeTimeout = write
	}
}

// WithServerTLS enables TLS with the given configuration.
func WithServerTLS(config *tls.Config) ServerOption {
	return func(s *ServerConnector) {
		s.tlsConfig = config
	}
}

// NewServer creates a new TCP server connector.
func NewServer(name, host string, port int, protocol string, opts ...ServerOption) (*ServerConnector, error) {
	codec, err := NewCodec(protocol)
	if err != nil {
		return nil, fmt.Errorf("failed to create codec: %w", err)
	}

	s := &ServerConnector{
		name:         name,
		host:         host,
		port:         port,
		protocol:     protocol,
		codec:        codec,
		handlers:     make(map[string]HandlerFunc),
		conns:        make(map[net.Conn]struct{}),
		maxConns:     100,
		readTimeout:  30 * time.Second,
		writeTimeout: 30 * time.Second,
		logger:       slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s, nil
}

// Name returns the connector name.
func (s *ServerConnector) Name() string {
	return s.name
}

// Type returns "tcp".
func (s *ServerConnector) Type() string {
	return "tcp"
}

// Connect is a no-op for the server; actual listening happens in Start.
func (s *ServerConnector) Connect(ctx context.Context) error {
	return nil
}

// Close stops the server and closes all connections.
func (s *ServerConnector) Close(ctx context.Context) error {
	s.connMu.Lock()
	if !s.running {
		s.connMu.Unlock()
		return nil
	}
	s.running = false
	s.connMu.Unlock()

	// Signal shutdown
	if s.cancel != nil {
		s.cancel()
	}

	// Close listener
	if s.listener != nil {
		s.listener.Close()
	}

	// Close all active connections
	s.connMu.Lock()
	for conn := range s.conns {
		conn.Close()
	}
	s.connMu.Unlock()

	// Wait for all handlers to complete (with timeout)
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All handlers completed
	case <-time.After(5 * time.Second):
		s.logger.Warn("timeout waiting for handlers to complete")
	}

	return nil
}

// Health checks if the server is running.
func (s *ServerConnector) Health(ctx context.Context) error {
	s.connMu.RLock()
	defer s.connMu.RUnlock()

	if !s.running {
		return fmt.Errorf("server not running")
	}
	return nil
}

// Start begins listening for connections.
func (s *ServerConnector) Start(ctx context.Context) error {
	s.connMu.Lock()
	if s.running {
		s.connMu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.running = true
	s.connMu.Unlock()

	// Create shutdown context
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Create listener
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	var listener net.Listener
	var err error

	if s.tlsConfig != nil {
		listener, err = tls.Listen("tcp", addr, s.tlsConfig)
	} else {
		listener, err = net.Listen("tcp", addr)
	}

	if err != nil {
		s.connMu.Lock()
		s.running = false
		s.connMu.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	s.listener = listener
	s.logger.Info("TCP server listening",
		"name", s.name,
		"address", addr,
		"protocol", s.protocol,
	)

	// Accept connections in background
	go s.acceptLoop()

	return nil
}

// acceptLoop accepts incoming connections.
func (s *ServerConnector) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				// Server shutting down
				return
			default:
				s.logger.Error("accept error", "error", err)
				continue
			}
		}

		// Check connection limit
		s.connMu.RLock()
		count := len(s.conns)
		s.connMu.RUnlock()

		if count >= s.maxConns {
			s.logger.Warn("connection limit reached, rejecting",
				"current", count,
				"max", s.maxConns,
			)
			conn.Close()
			continue
		}

		// Track connection
		s.connMu.Lock()
		s.conns[conn] = struct{}{}
		s.connMu.Unlock()

		// Handle connection
		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// handleConnection handles a single client connection.
func (s *ServerConnector) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.connMu.Lock()
		delete(s.conns, conn)
		s.connMu.Unlock()
		conn.Close()
	}()

	framer := NewFramer(conn, s.codec)
	remoteAddr := conn.RemoteAddr().String()

	s.logger.Debug("client connected", "remote", remoteAddr)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		// Set read deadline
		if s.readTimeout > 0 {
			conn.SetReadDeadline(time.Now().Add(s.readTimeout))
		}

		// Read message
		var msg Message
		if err := framer.ReadMessage(&msg); err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Read timeout - client may still be active
				continue
			}
			// Connection closed or error
			s.logger.Debug("client disconnected", "remote", remoteAddr, "reason", err)
			return
		}

		// Process message
		s.wg.Add(1)
		go func(msg Message) {
			defer s.wg.Done()
			s.processMessage(framer, &msg)
		}(msg)
	}
}

// processMessage processes a single message.
func (s *ServerConnector) processMessage(framer *Framer, msg *Message) {
	// Find handler
	s.mu.RLock()
	handler, ok := s.handlers[msg.Type]
	s.mu.RUnlock()

	if !ok {
		s.logger.Warn("no handler for message type", "type", msg.Type)
		s.sendErrorResponse(framer, msg.ID, fmt.Sprintf("unknown message type: %s", msg.Type))
		return
	}

	// Build input from message data
	input := msg.Data
	if input == nil {
		input = make(map[string]interface{})
	}

	// Execute handler
	ctx, cancel := context.WithTimeout(s.ctx, s.readTimeout)
	defer cancel()

	result, err := handler(ctx, input)
	if err != nil {
		s.sendErrorResponse(framer, msg.ID, err.Error())
		return
	}

	// Send response
	s.sendSuccessResponse(framer, msg.ID, result)
}

// sendSuccessResponse sends a success response.
func (s *ServerConnector) sendSuccessResponse(framer *Framer, requestID string, result interface{}) {
	var data map[string]interface{}

	switch v := result.(type) {
	case map[string]interface{}:
		data = v
	case nil:
		data = map[string]interface{}{"success": true}
	default:
		data = map[string]interface{}{"result": v}
	}

	response := NewSuccessResponse(requestID, data)
	s.sendResponse(framer, response.ToMessage())
}

// sendErrorResponse sends an error response.
func (s *ServerConnector) sendErrorResponse(framer *Framer, requestID, errMsg string) {
	response := NewFailureResponse(requestID, errMsg)
	s.sendResponse(framer, response.ToMessage())
}

// sendResponse sends a response message.
func (s *ServerConnector) sendResponse(framer *Framer, msg *Message) {
	if s.writeTimeout > 0 {
		framer.SetWriteDeadline(time.Now().Add(s.writeTimeout))
	}

	if err := framer.WriteMessage(msg); err != nil {
		s.logger.Error("failed to send response", "error", err)
	}
}

// RegisterRoute registers a handler for a message type.
// This implements the RouteRegistrar interface used by the runtime.
func (s *ServerConnector) RegisterRoute(operation string, handler HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[operation] = handler
	s.logger.Debug("registered handler", "type", operation)
}

// Address returns the listening address.
func (s *ServerConnector) Address() string {
	return fmt.Sprintf("%s:%d", s.host, s.port)
}

// ConnectionCount returns the current number of active connections.
func (s *ServerConnector) ConnectionCount() int {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return len(s.conns)
}
