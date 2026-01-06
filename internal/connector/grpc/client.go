package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/desc/protoparse"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/jhump/protoreflect/dynamic/grpcdynamic"
	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
)

// ClientConnector consumes external gRPC services.
type ClientConnector struct {
	name   string
	config *ClientConfig
	conn   *grpc.ClientConn
	stub   grpcdynamic.Stub

	mu             sync.RWMutex
	serviceDescs   map[string]*desc.ServiceDescriptor
	messageFactory *dynamic.MessageFactory
}

// NewClientConnector creates a new gRPC client connector.
func NewClientConnector(name string, config *ClientConfig) *ClientConnector {
	return &ClientConnector{
		name:         name,
		config:       config,
		serviceDescs: make(map[string]*desc.ServiceDescriptor),
	}
}

// Name returns the connector name.
func (c *ClientConnector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *ClientConnector) Type() string {
	return "grpc"
}

// Connect establishes a connection to the gRPC server.
func (c *ClientConnector) Connect(ctx context.Context) error {
	// Build dial options
	opts, err := c.buildDialOptions()
	if err != nil {
		return err
	}

	// Connect with timeout
	dialCtx := ctx
	if c.config.ConnectTimeout > 0 {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, c.config.ConnectTimeout)
		defer cancel()
	}

	// Dial the server
	conn, err := grpc.DialContext(dialCtx, c.config.Target, opts...)
	if err != nil {
		return fmt.Errorf("failed to dial %s: %w", c.config.Target, err)
	}

	c.conn = conn
	c.stub = grpcdynamic.NewStub(conn)
	c.messageFactory = dynamic.NewMessageFactoryWithDefaults()

	// Load proto definitions
	if c.config.ProtoPath != "" || len(c.config.ProtoFiles) > 0 {
		if err := c.loadProtos(); err != nil {
			return err
		}
	} else {
		// Try to use server reflection
		if err := c.loadFromReflection(ctx); err != nil {
			// Reflection not available, continue without proto definitions
		}
	}

	return nil
}

// buildDialOptions builds gRPC dial options.
func (c *ClientConnector) buildDialOptions() ([]grpc.DialOption, error) {
	var opts []grpc.DialOption

	// TLS or insecure
	if c.config.Insecure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else if c.config.TLS != nil && c.config.TLS.Enabled {
		creds, err := c.buildTLSCredentials()
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		// Default to insecure for simplicity
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Authentication
	if c.config.Auth != nil {
		if authOpt := BuildClientAuthOption(c.config.Auth); authOpt != nil {
			opts = append(opts, authOpt)
		}
	}

	// Wait for ready
	if c.config.WaitForReady {
		opts = append(opts, grpc.WithDefaultCallOptions(grpc.WaitForReady(true)))
	}

	// Keep-alive
	if c.config.KeepAlive != nil {
		kaParams := keepalive.ClientParameters{
			Time:    c.config.KeepAlive.Time,
			Timeout: c.config.KeepAlive.Timeout,
		}
		opts = append(opts, grpc.WithKeepaliveParams(kaParams))
	}

	// Message sizes
	if c.config.MaxRecv > 0 {
		opts = append(opts, grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(c.config.MaxRecv*1024*1024)))
	}
	if c.config.MaxSend > 0 {
		opts = append(opts, grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(c.config.MaxSend*1024*1024)))
	}

	return opts, nil
}

// buildTLSCredentials builds TLS credentials.
func (c *ClientConnector) buildTLSCredentials() (credentials.TransportCredentials, error) {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: c.config.TLS.SkipVerify,
	}

	if c.config.TLS.ServerName != "" {
		tlsCfg.ServerName = c.config.TLS.ServerName
	}

	if c.config.TLS.CAFile != "" {
		ca, err := os.ReadFile(c.config.TLS.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(ca)
		tlsCfg.RootCAs = pool
	}

	if c.config.TLS.CertFile != "" && c.config.TLS.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.config.TLS.CertFile, c.config.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return credentials.NewTLS(tlsCfg), nil
}

// loadProtos parses .proto files and builds service descriptors.
func (c *ClientConnector) loadProtos() error {
	var protoFiles []string

	if len(c.config.ProtoFiles) > 0 {
		protoFiles = c.config.ProtoFiles
	} else if c.config.ProtoPath != "" {
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

	parser := protoparse.Parser{
		ImportPaths: []string{c.config.ProtoPath, "."},
	}

	descs, err := parser.ParseFiles(protoFiles...)
	if err != nil {
		return fmt.Errorf("failed to parse proto files: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, fd := range descs {
		for _, sd := range fd.GetServices() {
			c.serviceDescs[sd.GetFullyQualifiedName()] = sd
		}
	}

	return nil
}

// loadFromReflection loads service descriptors from server reflection.
func (c *ClientConnector) loadFromReflection(ctx context.Context) error {
	refClient := grpcreflect.NewClientV1Alpha(ctx, rpb.NewServerReflectionClient(c.conn))
	defer refClient.Reset()

	services, err := refClient.ListServices()
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, svc := range services {
		if strings.HasPrefix(svc, "grpc.") {
			continue // Skip internal services
		}

		sd, err := refClient.ResolveService(svc)
		if err != nil {
			continue
		}
		c.serviceDescs[svc] = sd
	}

	return nil
}

// Close closes the connection.
func (c *ClientConnector) Close(ctx context.Context) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Health checks if the connector is healthy.
func (c *ClientConnector) Health(ctx context.Context) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	return nil
}

// Call invokes a gRPC method.
// Operation format: "package.Service/Method" or "Service/Method"
func (c *ClientConnector) Call(ctx context.Context, operation string, input map[string]interface{}) (interface{}, error) {
	parts := strings.Split(operation, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid operation format, expected 'Service/Method': %s", operation)
	}

	serviceName := parts[0]
	methodName := parts[1]

	// Find service descriptor
	sd := c.findService(serviceName)
	if sd == nil {
		return nil, fmt.Errorf("service not found: %s", serviceName)
	}

	// Find method descriptor
	md := sd.FindMethodByName(methodName)
	if md == nil {
		return nil, fmt.Errorf("method not found: %s/%s", serviceName, methodName)
	}

	// Create input message
	inputMsg := c.messageFactory.NewDynamicMessage(md.GetInputType())
	if err := mapToDynamicMessage(input, inputMsg); err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	// Set timeout
	callCtx := ctx
	if c.config.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, c.config.Timeout)
		defer cancel()
	}

	// Make the call with retry
	var respMsg proto.Message
	var err error

	retries := c.config.RetryCount
	if retries == 0 {
		retries = 1
	}

	for i := 0; i < retries; i++ {
		respMsg, err = c.stub.InvokeRpc(callCtx, md, inputMsg)
		if err == nil {
			break
		}

		if i < retries-1 && c.config.RetryBackoff > 0 {
			time.Sleep(c.config.RetryBackoff * time.Duration(i+1))
		}
	}

	if err != nil {
		return nil, err
	}

	// Convert response to map
	resp, ok := respMsg.(*dynamic.Message)
	if !ok {
		return nil, fmt.Errorf("unexpected response type")
	}
	return dynamicMessageToMap(resp)
}

// findService finds a service descriptor by name.
func (c *ClientConnector) findService(name string) *desc.ServiceDescriptor {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if sd, ok := c.serviceDescs[name]; ok {
		return sd
	}

	// Try partial match
	for fqn, sd := range c.serviceDescs {
		if strings.HasSuffix(fqn, "."+name) {
			return sd
		}
	}

	return nil
}

// Read executes a gRPC call (alias for Call).
func (c *ClientConnector) Read(ctx context.Context, query interface{}) ([]map[string]interface{}, error) {
	q, ok := query.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid query type")
	}

	operation, _ := q["operation"].(string)
	input, _ := q["input"].(map[string]interface{})

	result, err := c.Call(ctx, operation, input)
	if err != nil {
		return nil, err
	}

	if m, ok := result.(map[string]interface{}); ok {
		return []map[string]interface{}{m}, nil
	}

	return nil, nil
}

// Write executes a gRPC call (alias for Call).
func (c *ClientConnector) Write(ctx context.Context, data interface{}) (map[string]interface{}, error) {
	d, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid data type")
	}

	operation, _ := d["operation"].(string)
	input, _ := d["input"].(map[string]interface{})

	result, err := c.Call(ctx, operation, input)
	if err != nil {
		return nil, err
	}

	if m, ok := result.(map[string]interface{}); ok {
		return m, nil
	}

	return map[string]interface{}{"result": result}, nil
}

// ListServices returns all known services.
func (c *ClientConnector) ListServices() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var services []string
	for name := range c.serviceDescs {
		services = append(services, name)
	}
	return services
}

// dynamicMessageToMap is defined in server.go, reuse here
// Note: In actual code, this would be in a shared file
func init() {
	// Ensure the function exists (it's defined in server.go)
	_ = json.Marshal
}
