package soap

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
)

// HandlerFunc handles a SOAP operation request.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// Server exposes SOAP endpoints over HTTP.
type Server struct {
	name        string
	port        int
	soapVersion string
	namespace   string
	logger      *slog.Logger
	server      *http.Server

	mu       sync.Mutex
	handlers map[string]HandlerFunc
	started  bool
}

// NewServer creates a new SOAP server connector.
func NewServer(name string, port int, soapVersion, namespace string, logger *slog.Logger) *Server {
	if soapVersion == "" {
		soapVersion = "1.1"
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Server{
		name:        name,
		port:        port,
		soapVersion: soapVersion,
		namespace:   namespace,
		logger:      logger,
		handlers:    make(map[string]HandlerFunc),
	}
}

func (s *Server) Name() string { return s.name }
func (s *Server) Type() string { return "soap" }

func (s *Server) Connect(ctx context.Context) error { return nil }

func (s *Server) Close(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *Server) Health(ctx context.Context) error {
	if !s.started {
		return fmt.Errorf("SOAP server not started")
	}
	return nil
}

// RegisterRoute registers a SOAP operation handler.
// Operation is the SOAP operation name (e.g., "CreateOrder", "GetOrder").
func (s *Server) RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.handlers[operation]; ok {
		s.handlers[operation] = HandlerFunc(connector.ChainRequestResponse(
			connector.HandlerFunc(existing),
			connector.HandlerFunc(handler),
			s.logger,
		))
		s.logger.Info("fan-out: multiple flows registered", "operation", operation)
	} else {
		s.handlers[operation] = handler
	}
}

// Start starts the SOAP HTTP server.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("SOAP server already started")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleSOAPRequest)
	mux.HandleFunc("/wsdl", s.handleWSDLRequest)

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("SOAP server error", slog.Any("error", err))
		}
	}()

	s.started = true
	return nil
}

// handleSOAPRequest processes incoming SOAP requests.
func (s *Server) handleSOAPRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeFault(w, "Client", "Failed to read request body", err.Error())
		return
	}

	// Unwrap SOAP envelope
	operation, params, fault, err := Unwrap(body)
	if err != nil {
		s.writeFault(w, "Client", "Invalid SOAP envelope", err.Error())
		return
	}
	if fault != nil {
		s.writeFault(w, fault.Code, fault.String, fault.Detail)
		return
	}

	// Look up handler
	s.mu.Lock()
	handler, ok := s.handlers[operation]
	s.mu.Unlock()

	if !ok {
		s.writeFault(w, "Client", fmt.Sprintf("Unknown operation: %s", operation), "")
		return
	}

	// Execute handler
	result, err := handler(r.Context(), params)
	// Fire deferred on_drop closure (no-op on success).
	flow.FireDropAspect(r.Context(), result)
	if err != nil {
		s.logger.Error("SOAP handler error",
			slog.String("operation", operation),
			slog.Any("error", err),
		)
		s.writeFault(w, "Server", err.Error(), "")
		return
	}

	// Convert result to map for envelope
	var resultMap map[string]interface{}
	switch v := result.(type) {
	case map[string]interface{}:
		resultMap = v
	case []map[string]interface{}:
		resultMap = map[string]interface{}{"items": v}
	default:
		resultMap = map[string]interface{}{"result": result}
	}

	// Check for http_status_code override in response (from response block)
	statusCode := http.StatusOK
	if code, found := connector.ExtractStatusCode(resultMap, "http_status_code"); found {
		statusCode = code
	}

	// Build response envelope
	responseOp := operation + "Response"
	envelope, err := Envelope(s.soapVersion, s.namespace, responseOp, resultMap)
	if err != nil {
		s.writeFault(w, "Server", "Failed to build response envelope", err.Error())
		return
	}

	w.Header().Set("Content-Type", ContentTypeForVersion(s.soapVersion))
	w.WriteHeader(statusCode)
	w.Write(envelope)
}

// writeFault writes a SOAP fault response.
func (s *Server) writeFault(w http.ResponseWriter, code, message, detail string) {
	faultBytes := FaultEnvelope(s.soapVersion, code, message, detail)
	w.Header().Set("Content-Type", ContentTypeForVersion(s.soapVersion))
	w.WriteHeader(http.StatusInternalServerError)
	w.Write(faultBytes)
}

// handleWSDLRequest returns a basic auto-generated WSDL.
func (s *Server) handleWSDLRequest(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	operations := make([]string, 0, len(s.handlers))
	for op := range s.handlers {
		operations = append(operations, op)
	}
	s.mu.Unlock()

	ns := s.namespace
	if ns == "" {
		ns = "http://tempuri.org/"
	}

	var buf strings.Builder
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString("\n")
	buf.WriteString(fmt.Sprintf(`<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  xmlns:tns="%s"
  targetNamespace="%s">`, ns, ns))
	buf.WriteString("\n")

	// Port type with operations
	buf.WriteString(`  <portType name="ServicePort">`)
	buf.WriteString("\n")
	for _, op := range operations {
		buf.WriteString(fmt.Sprintf(`    <operation name="%s">`, op))
		buf.WriteString("\n")
		buf.WriteString(fmt.Sprintf(`      <input message="tns:%sRequest"/>`, op))
		buf.WriteString("\n")
		buf.WriteString(fmt.Sprintf(`      <output message="tns:%sResponse"/>`, op))
		buf.WriteString("\n")
		buf.WriteString(`    </operation>`)
		buf.WriteString("\n")
	}
	buf.WriteString(`  </portType>`)
	buf.WriteString("\n")

	// Binding
	buf.WriteString(`  <binding name="ServiceBinding" type="tns:ServicePort">`)
	buf.WriteString("\n")
	buf.WriteString(fmt.Sprintf(`    <soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>`))
	buf.WriteString("\n")
	for _, op := range operations {
		buf.WriteString(fmt.Sprintf(`    <operation name="%s">`, op))
		buf.WriteString("\n")
		buf.WriteString(fmt.Sprintf(`      <soap:operation soapAction="%s/%s"/>`, ns, op))
		buf.WriteString("\n")
		buf.WriteString(`    </operation>`)
		buf.WriteString("\n")
	}
	buf.WriteString(`  </binding>`)
	buf.WriteString("\n")

	// Service
	scheme := "http"
	host := r.Host
	buf.WriteString(`  <service name="Service">`)
	buf.WriteString("\n")
	buf.WriteString(`    <port name="ServicePort" binding="tns:ServiceBinding">`)
	buf.WriteString("\n")
	buf.WriteString(fmt.Sprintf(`      <soap:address location="%s://%s/"/>`, scheme, host))
	buf.WriteString("\n")
	buf.WriteString(`    </port>`)
	buf.WriteString("\n")
	buf.WriteString(`  </service>`)
	buf.WriteString("\n")

	buf.WriteString(`</definitions>`)

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(buf.String()))
}
