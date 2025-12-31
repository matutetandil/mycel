package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/desc/protoparse"
	"github.com/jhump/protoreflect/dynamic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

// HandlerFunc is a function that handles a gRPC request.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// ServerConnector exposes gRPC services.
type ServerConnector struct {
	name   string
	config *ServerConfig
	server *grpc.Server
	logger *slog.Logger

	mu             sync.RWMutex
	handlers       map[string]HandlerFunc // "service/method" -> handler
	serviceDescs   map[string]*desc.ServiceDescriptor
	messageFactory *dynamic.MessageFactory
	started        bool
}

// NewServerConnector creates a new gRPC server connector.
func NewServerConnector(name string, config *ServerConfig, logger *slog.Logger) *ServerConnector {
	if logger == nil {
		logger = slog.Default()
	}

	return &ServerConnector{
		name:         name,
		config:       config,
		logger:       logger,
		handlers:     make(map[string]HandlerFunc),
		serviceDescs: make(map[string]*desc.ServiceDescriptor),
	}
}

// Name returns the connector name.
func (c *ServerConnector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *ServerConnector) Type() string {
	return "grpc"
}

// Connect loads proto definitions.
func (c *ServerConnector) Connect(ctx context.Context) error {
	if c.config.ProtoPath != "" || len(c.config.ProtoFiles) > 0 {
		return c.loadProtos()
	}
	return nil
}

// loadProtos parses .proto files and builds service descriptors.
func (c *ServerConnector) loadProtos() error {
	var protoFiles []string

	if len(c.config.ProtoFiles) > 0 {
		protoFiles = c.config.ProtoFiles
	} else if c.config.ProtoPath != "" {
		// Find all .proto files in directory
		err := filepath.Walk(c.config.ProtoPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(info.Name(), ".proto") {
				protoFiles = append(protoFiles, path)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to scan proto directory: %w", err)
		}
	}

	if len(protoFiles) == 0 {
		return nil
	}

	// Parse protos
	parser := protoparse.Parser{
		ImportPaths: []string{c.config.ProtoPath, "."},
	}

	descs, err := parser.ParseFiles(protoFiles...)
	if err != nil {
		return fmt.Errorf("failed to parse proto files: %w", err)
	}

	// Extract service descriptors
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, fd := range descs {
		for _, sd := range fd.GetServices() {
			c.serviceDescs[sd.GetFullyQualifiedName()] = sd
			c.logger.Debug("Loaded gRPC service",
				slog.String("service", sd.GetFullyQualifiedName()),
				slog.Int("methods", len(sd.GetMethods())),
			)
		}
	}

	c.messageFactory = dynamic.NewMessageFactoryWithDefaults()

	return nil
}

// Close stops the gRPC server.
func (c *ServerConnector) Close(ctx context.Context) error {
	if c.server != nil {
		c.server.GracefulStop()
	}
	return nil
}

// Health checks if the connector is healthy.
func (c *ServerConnector) Health(ctx context.Context) error {
	if !c.started {
		return fmt.Errorf("server not started")
	}
	return nil
}

// RegisterRoute registers a handler for a gRPC method.
// Operation format: "package.Service/Method" or "Service/Method"
func (c *ServerConnector) RegisterRoute(operation string, handler HandlerFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[operation] = handler
}

// Start starts the gRPC server.
func (c *ServerConnector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("server already started")
	}

	// Create listener
	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// Build server options
	opts := c.buildServerOptions()

	// Create server
	c.server = grpc.NewServer(opts...)

	// Register services based on loaded protos and handlers
	if err := c.registerServices(); err != nil {
		return err
	}

	// Enable reflection if configured
	if c.config.Reflection {
		reflection.Register(c.server)
	}

	// Start server in goroutine
	go func() {
		if err := c.server.Serve(lis); err != nil {
			c.logger.Error("gRPC server error", slog.Any("error", err))
		}
	}()

	c.started = true
	c.logger.Info("gRPC server started",
		slog.String("address", addr),
		slog.Bool("reflection", c.config.Reflection),
	)

	return nil
}

// buildServerOptions builds gRPC server options.
func (c *ServerConnector) buildServerOptions() []grpc.ServerOption {
	var opts []grpc.ServerOption

	// TLS
	if c.config.TLS != nil && c.config.TLS.Enabled {
		creds, err := credentials.NewServerTLSFromFile(c.config.TLS.CertFile, c.config.TLS.KeyFile)
		if err == nil {
			opts = append(opts, grpc.Creds(creds))
		}
	}

	// Message sizes
	if c.config.MaxRecv > 0 {
		opts = append(opts, grpc.MaxRecvMsgSize(c.config.MaxRecv*1024*1024))
	}
	if c.config.MaxSend > 0 {
		opts = append(opts, grpc.MaxSendMsgSize(c.config.MaxSend*1024*1024))
	}

	return opts
}

// registerServices registers gRPC services based on handlers.
func (c *ServerConnector) registerServices() error {
	// Group handlers by service
	serviceHandlers := make(map[string]map[string]HandlerFunc)

	for op, handler := range c.handlers {
		parts := strings.Split(op, "/")
		if len(parts) != 2 {
			c.logger.Warn("Invalid operation format, expected 'Service/Method'",
				slog.String("operation", op),
			)
			continue
		}

		serviceName := parts[0]
		methodName := parts[1]

		if _, ok := serviceHandlers[serviceName]; !ok {
			serviceHandlers[serviceName] = make(map[string]HandlerFunc)
		}
		serviceHandlers[serviceName][methodName] = handler
	}

	// Register each service
	for serviceName, methods := range serviceHandlers {
		if err := c.registerService(serviceName, methods); err != nil {
			return err
		}
	}

	return nil
}

// registerService registers a single gRPC service.
func (c *ServerConnector) registerService(serviceName string, methods map[string]HandlerFunc) error {
	// Find service descriptor
	var sd *desc.ServiceDescriptor
	for name, desc := range c.serviceDescs {
		if name == serviceName || strings.HasSuffix(name, "."+serviceName) {
			sd = desc
			break
		}
	}

	if sd == nil {
		// Create a dynamic service without proto definition
		return c.registerDynamicService(serviceName, methods)
	}

	// Register service with proto definition
	return c.registerProtoService(sd, methods)
}

// registerProtoService registers a service with proto definition.
func (c *ServerConnector) registerProtoService(sd *desc.ServiceDescriptor, methods map[string]HandlerFunc) error {
	// Build service description
	svcDesc := grpc.ServiceDesc{
		ServiceName: sd.GetFullyQualifiedName(),
		HandlerType: (*interface{})(nil),
	}

	for _, md := range sd.GetMethods() {
		handler, ok := methods[md.GetName()]
		if !ok {
			continue
		}

		methodDesc := md
		h := handler

		if md.IsClientStreaming() || md.IsServerStreaming() {
			// Streaming method
			svcDesc.Streams = append(svcDesc.Streams, grpc.StreamDesc{
				StreamName:    md.GetName(),
				ServerStreams: md.IsServerStreaming(),
				ClientStreams: md.IsClientStreaming(),
				Handler: func(srv interface{}, stream grpc.ServerStream) error {
					return c.handleStream(methodDesc, h, stream)
				},
			})
		} else {
			// Unary method
			svcDesc.Methods = append(svcDesc.Methods, grpc.MethodDesc{
				MethodName: md.GetName(),
				Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
					return c.handleUnary(ctx, methodDesc, h, dec, interceptor)
				},
			})
		}
	}

	c.server.RegisterService(&svcDesc, nil)
	return nil
}

// registerDynamicService registers a service without proto definition.
func (c *ServerConnector) registerDynamicService(serviceName string, methods map[string]HandlerFunc) error {
	svcDesc := grpc.ServiceDesc{
		ServiceName: serviceName,
		HandlerType: (*interface{})(nil),
	}

	for methodName, handler := range methods {
		h := handler
		name := methodName

		svcDesc.Methods = append(svcDesc.Methods, grpc.MethodDesc{
			MethodName: name,
			Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
				return c.handleDynamicUnary(ctx, h, dec, interceptor)
			},
		})
	}

	c.server.RegisterService(&svcDesc, nil)
	return nil
}

// handleUnary handles a unary RPC call with proto definition.
func (c *ServerConnector) handleUnary(ctx context.Context, md *desc.MethodDescriptor, handler HandlerFunc, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	// Create input message
	inputMsg := c.messageFactory.NewDynamicMessage(md.GetInputType())

	// Decode request
	if err := dec(inputMsg); err != nil {
		return nil, err
	}

	// Convert to map
	inputData, err := dynamicMessageToMap(inputMsg)
	if err != nil {
		return nil, err
	}

	// Call handler
	result, err := handler(ctx, inputData)
	if err != nil {
		return nil, err
	}

	// Convert result to output message
	outputMsg := c.messageFactory.NewDynamicMessage(md.GetOutputType())
	if err := mapToDynamicMessage(result, outputMsg); err != nil {
		return nil, err
	}

	return outputMsg, nil
}

// handleDynamicUnary handles a unary RPC call without proto definition.
func (c *ServerConnector) handleDynamicUnary(ctx context.Context, handler HandlerFunc, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	// For dynamic calls, we use JSON encoding
	var input map[string]interface{}
	if err := dec(&input); err != nil {
		// Try decoding as bytes
		var data []byte
		if err := dec(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &input); err != nil {
			return nil, err
		}
	}

	return handler(ctx, input)
}

// handleStream handles a streaming RPC call.
func (c *ServerConnector) handleStream(md *desc.MethodDescriptor, handler HandlerFunc, stream grpc.ServerStream) error {
	// For now, we only support simple request-response patterns
	// Full streaming support can be added later

	inputMsg := c.messageFactory.NewDynamicMessage(md.GetInputType())
	if err := stream.RecvMsg(inputMsg); err != nil {
		return err
	}

	inputData, err := dynamicMessageToMap(inputMsg)
	if err != nil {
		return err
	}

	result, err := handler(stream.Context(), inputData)
	if err != nil {
		return err
	}

	outputMsg := c.messageFactory.NewDynamicMessage(md.GetOutputType())
	if err := mapToDynamicMessage(result, outputMsg); err != nil {
		return err
	}

	return stream.SendMsg(outputMsg)
}

// dynamicMessageToMap converts a dynamic protobuf message to a map.
func dynamicMessageToMap(msg *dynamic.Message) (map[string]interface{}, error) {
	data, err := msg.MarshalJSON()
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// mapToDynamicMessage converts a map/interface to a dynamic protobuf message.
func mapToDynamicMessage(data interface{}, msg *dynamic.Message) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return msg.UnmarshalJSON(jsonData)
}

// FindSymbol looks up a service by name.
func (c *ServerConnector) FindSymbol(name string) (*desc.ServiceDescriptor, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if sd, ok := c.serviceDescs[name]; ok {
		return sd, nil
	}

	// Try to find by short name
	for fullName, sd := range c.serviceDescs {
		if strings.HasSuffix(fullName, "."+name) {
			return sd, nil
		}
	}

	return nil, fmt.Errorf("symbol not found: %s", name)
}

// ListServices returns all registered service names.
func (c *ServerConnector) ListServices() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var services []string
	for name := range c.serviceDescs {
		services = append(services, name)
	}
	return services
}
