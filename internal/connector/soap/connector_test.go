package soap

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// --- Envelope Tests ---

func TestEnvelopeSOAP11(t *testing.T) {
	body := map[string]interface{}{
		"OrderID": "123",
	}

	env, err := Envelope("1.1", "http://example.com/service", "GetOrder", body)
	if err != nil {
		t.Fatalf("Envelope error: %v", err)
	}

	s := string(env)
	if !strings.Contains(s, NS11) {
		t.Errorf("expected SOAP 1.1 namespace, got: %s", s)
	}
	if !strings.Contains(s, "<ns:GetOrder>") {
		t.Errorf("expected <ns:GetOrder> operation, got: %s", s)
	}
	if !strings.Contains(s, "<OrderID>123</OrderID>") {
		t.Errorf("expected OrderID element, got: %s", s)
	}
}

func TestEnvelopeSOAP12(t *testing.T) {
	body := map[string]interface{}{
		"Name": "Widget",
	}

	env, err := Envelope("1.2", "http://example.com/service", "CreateProduct", body)
	if err != nil {
		t.Fatalf("Envelope error: %v", err)
	}

	s := string(env)
	if !strings.Contains(s, NS12) {
		t.Errorf("expected SOAP 1.2 namespace, got: %s", s)
	}
	if !strings.Contains(s, "<ns:CreateProduct>") {
		t.Errorf("expected operation element, got: %s", s)
	}
}

func TestUnwrapResponse(t *testing.T) {
	response := fmt.Sprintf(`<?xml version="1.0"?>
<soap:Envelope xmlns:soap="%s">
  <soap:Body>
    <GetOrderResponse>
      <OrderID>123</OrderID>
      <Status>shipped</Status>
    </GetOrderResponse>
  </soap:Body>
</soap:Envelope>`, NS11)

	op, body, fault, err := Unwrap([]byte(response))
	if err != nil {
		t.Fatalf("Unwrap error: %v", err)
	}
	if fault != nil {
		t.Fatalf("unexpected fault: %v", fault)
	}
	if op != "GetOrder" {
		t.Errorf("expected operation=GetOrder, got %s", op)
	}
	if body["OrderID"] != "123" {
		t.Errorf("expected OrderID=123, got %v", body["OrderID"])
	}
	if body["Status"] != "shipped" {
		t.Errorf("expected Status=shipped, got %v", body["Status"])
	}
}

func TestUnwrapFault(t *testing.T) {
	response := fmt.Sprintf(`<?xml version="1.0"?>
<soap:Envelope xmlns:soap="%s">
  <soap:Body>
    <soap:Fault>
      <faultcode>Client</faultcode>
      <faultstring>Invalid order ID</faultstring>
      <detail>Order not found</detail>
    </soap:Fault>
  </soap:Body>
</soap:Envelope>`, NS11)

	_, _, fault, err := Unwrap([]byte(response))
	if err != nil {
		t.Fatalf("Unwrap error: %v", err)
	}
	if fault == nil {
		t.Fatal("expected fault, got nil")
	}
	if fault.Code != "Client" {
		t.Errorf("expected fault code=Client, got %s", fault.Code)
	}
	if fault.String != "Invalid order ID" {
		t.Errorf("expected fault string, got %s", fault.String)
	}
	if fault.Detail != "Order not found" {
		t.Errorf("expected fault detail, got %s", fault.Detail)
	}
}

func TestFaultEnvelope(t *testing.T) {
	env := FaultEnvelope("1.1", "Server", "Internal error", "something broke")
	s := string(env)

	if !strings.Contains(s, "<faultcode>Server</faultcode>") {
		t.Errorf("expected faultcode element, got: %s", s)
	}
	if !strings.Contains(s, "<faultstring>Internal error</faultstring>") {
		t.Errorf("expected faultstring element, got: %s", s)
	}
}

func TestFaultEnvelope12(t *testing.T) {
	env := FaultEnvelope("1.2", "soap:Receiver", "Internal error", "")
	s := string(env)

	if !strings.Contains(s, NS12) {
		t.Errorf("expected SOAP 1.2 namespace, got: %s", s)
	}
	if !strings.Contains(s, "<soap:Value>soap:Receiver</soap:Value>") {
		t.Errorf("expected SOAP 1.2 fault code, got: %s", s)
	}
}

// --- Client Tests ---

func TestClientRoundTrip(t *testing.T) {
	// Create a mock SOAP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.Header.Get("Content-Type"), "text/xml") {
			t.Errorf("expected text/xml content type, got %s", r.Header.Get("Content-Type"))
		}

		// Read and verify SOAP action header
		soapAction := r.Header.Get("SOAPAction")
		if !strings.Contains(soapAction, "GetOrder") {
			t.Errorf("expected SOAPAction containing GetOrder, got %s", soapAction)
		}

		// Read request body
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "GetOrder") {
			t.Errorf("expected GetOrder in body, got %s", string(body))
		}

		// Send response
		resp := fmt.Sprintf(`<?xml version="1.0"?>
<soap:Envelope xmlns:soap="%s">
  <soap:Body>
    <GetOrderResponse>
      <OrderID>42</OrderID>
      <Status>delivered</Status>
    </GetOrderResponse>
  </soap:Body>
</soap:Envelope>`, NS11)

		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(200)
		w.Write([]byte(resp))
	}))
	defer server.Close()

	client := NewClient("test-soap", server.URL, "1.1", "http://example.com/service", 5*time.Second, nil, nil)

	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	result, err := client.Read(ctx, connector.Query{
		Operation: "GetOrder",
		Filters:   map[string]interface{}{"OrderID": "42"},
	})
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}

	if len(result.Rows) == 0 {
		t.Fatal("expected rows in result")
	}
	if result.Rows[0]["OrderID"] != "42" {
		t.Errorf("expected OrderID=42, got %v", result.Rows[0]["OrderID"])
	}
	if result.Rows[0]["Status"] != "delivered" {
		t.Errorf("expected Status=delivered, got %v", result.Rows[0]["Status"])
	}
}

func TestClientSOAP12(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/soap+xml") {
			t.Errorf("expected application/soap+xml, got %s", ct)
		}
		if !strings.Contains(ct, "action=") {
			t.Errorf("expected action parameter in Content-Type, got %s", ct)
		}

		// Verify no SOAPAction header for 1.2
		if r.Header.Get("SOAPAction") != "" {
			t.Error("SOAP 1.2 should not use SOAPAction header")
		}

		resp := fmt.Sprintf(`<?xml version="1.0"?>
<soap:Envelope xmlns:soap="%s">
  <soap:Body>
    <PingResponse>
      <Result>pong</Result>
    </PingResponse>
  </soap:Body>
</soap:Envelope>`, NS12)

		w.Header().Set("Content-Type", "application/soap+xml")
		w.Write([]byte(resp))
	}))
	defer server.Close()

	client := NewClient("test-soap12", server.URL, "1.2", "http://example.com/v2", 5*time.Second, nil, nil)
	ctx := context.Background()
	client.Connect(ctx)

	result, err := client.Write(ctx, &connector.Data{
		Operation: "Ping",
		Payload:   map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if result.Rows[0]["Result"] != "pong" {
		t.Errorf("expected pong, got %v", result.Rows[0]["Result"])
	}
}

func TestClientFaultHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := fmt.Sprintf(`<?xml version="1.0"?>
<soap:Envelope xmlns:soap="%s">
  <soap:Body>
    <soap:Fault>
      <faultcode>Server</faultcode>
      <faultstring>Service unavailable</faultstring>
    </soap:Fault>
  </soap:Body>
</soap:Envelope>`, NS11)

		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(500)
		w.Write([]byte(resp))
	}))
	defer server.Close()

	client := NewClient("test-fault", server.URL, "1.1", "", 5*time.Second, nil, nil)
	ctx := context.Background()
	client.Connect(ctx)

	_, err := client.Read(ctx, connector.Query{Operation: "Fail"})
	if err == nil {
		t.Fatal("expected error for SOAP fault")
	}

	fault, ok := err.(*Fault)
	if !ok {
		t.Fatalf("expected *Fault error, got %T: %v", err, err)
	}
	if fault.Code != "Server" {
		t.Errorf("expected fault code=Server, got %s", fault.Code)
	}
}

func TestClientBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			t.Errorf("expected basic auth admin:secret, got %s:%s", user, pass)
		}

		resp := fmt.Sprintf(`<?xml version="1.0"?>
<soap:Envelope xmlns:soap="%s">
  <soap:Body><AuthResponse><OK>true</OK></AuthResponse></soap:Body>
</soap:Envelope>`, NS11)
		w.Write([]byte(resp))
	}))
	defer server.Close()

	auth := &AuthConfig{Type: "basic", Username: "admin", Password: "secret"}
	client := NewClient("test-auth", server.URL, "1.1", "", 5*time.Second, auth, nil)
	ctx := context.Background()
	client.Connect(ctx)

	_, err := client.Read(ctx, connector.Query{Operation: "Auth"})
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
}

// --- Server Tests ---

func TestServerRegisterAndHandle(t *testing.T) {
	srv := NewServer("test-soap-srv", 0, "1.1", "http://myservice.example.com", nil)

	// Register a handler
	srv.RegisterRoute("CreateOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{
			"OrderID": "ORD-001",
			"Status":  "created",
		}, nil
	})

	// Build SOAP request
	reqBody := fmt.Sprintf(`<?xml version="1.0"?>
<soap:Envelope xmlns:soap="%s">
  <soap:Body>
    <CreateOrder>
      <Name>Widget</Name>
      <Quantity>5</Quantity>
    </CreateOrder>
  </soap:Body>
</soap:Envelope>`, NS11)

	req := httptest.NewRequest("POST", "/", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "text/xml")
	w := httptest.NewRecorder()

	srv.handleSOAPRequest(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	s := string(body)

	if !strings.Contains(s, "CreateOrderResponse") {
		t.Errorf("expected CreateOrderResponse in body, got: %s", s)
	}
	if !strings.Contains(s, "<OrderID>ORD-001</OrderID>") {
		t.Errorf("expected OrderID element, got: %s", s)
	}
	if !strings.Contains(s, "<Status>created</Status>") {
		t.Errorf("expected Status element, got: %s", s)
	}
}

func TestServerUnknownOperation(t *testing.T) {
	srv := NewServer("test-soap-srv", 0, "1.1", "", nil)

	reqBody := fmt.Sprintf(`<?xml version="1.0"?>
<soap:Envelope xmlns:soap="%s">
  <soap:Body>
    <NonExistent><Foo>bar</Foo></NonExistent>
  </soap:Body>
</soap:Envelope>`, NS11)

	req := httptest.NewRequest("POST", "/", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	srv.handleSOAPRequest(w, req)

	resp := w.Result()
	if resp.StatusCode != 500 {
		t.Errorf("expected 500 for unknown operation, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	if !strings.Contains(s, "Unknown operation") {
		t.Errorf("expected unknown operation fault, got: %s", s)
	}
}

func TestServerMethodNotAllowed(t *testing.T) {
	srv := NewServer("test-soap-srv", 0, "1.1", "", nil)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	srv.handleSOAPRequest(w, req)

	if w.Result().StatusCode != 405 {
		t.Errorf("expected 405, got %d", w.Result().StatusCode)
	}
}

func TestServerHandlerError(t *testing.T) {
	srv := NewServer("test-soap-srv", 0, "1.1", "", nil)

	srv.RegisterRoute("FailOp", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, fmt.Errorf("database connection failed")
	})

	reqBody := fmt.Sprintf(`<?xml version="1.0"?>
<soap:Envelope xmlns:soap="%s">
  <soap:Body><FailOp/></soap:Body>
</soap:Envelope>`, NS11)

	req := httptest.NewRequest("POST", "/", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	srv.handleSOAPRequest(w, req)

	resp := w.Result()
	if resp.StatusCode != 500 {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "database connection failed") {
		t.Errorf("expected error message in fault, got: %s", string(body))
	}
}

func TestServerWSDLEndpoint(t *testing.T) {
	srv := NewServer("test-soap-srv", 0, "1.1", "http://myservice.example.com", nil)

	srv.RegisterRoute("GetOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})
	srv.RegisterRoute("CreateOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})

	req := httptest.NewRequest("GET", "/wsdl", nil)
	req.Host = "localhost:8081"
	w := httptest.NewRecorder()

	srv.handleWSDLRequest(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	s := string(body)

	if !strings.Contains(s, "definitions") {
		t.Errorf("expected WSDL definitions, got: %s", s)
	}
	if !strings.Contains(s, "myservice.example.com") {
		t.Errorf("expected namespace in WSDL, got: %s", s)
	}
}

// --- Factory Tests ---

func TestFactorySupports(t *testing.T) {
	f := NewFactory(nil)
	if !f.Supports("soap", "") {
		t.Error("expected factory to support 'soap'")
	}
	if f.Supports("rest", "") {
		t.Error("expected factory to not support 'rest'")
	}
}

func TestFactoryCreateClient(t *testing.T) {
	f := NewFactory(nil)
	cfg := &connector.Config{
		Name: "erp",
		Type: "soap",
		Properties: map[string]interface{}{
			"endpoint":     "https://erp.example.com/service",
			"soap_version": "1.1",
			"namespace":    "http://example.com/erp",
			"timeout":      "10s",
		},
	}

	conn, err := f.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	if conn.Name() != "erp" {
		t.Errorf("expected name=erp, got %s", conn.Name())
	}
	if conn.Type() != "soap" {
		t.Errorf("expected type=soap, got %s", conn.Type())
	}
}

func TestFactoryCreateServer(t *testing.T) {
	f := NewFactory(nil)
	cfg := &connector.Config{
		Name: "soap_srv",
		Type: "soap",
		Properties: map[string]interface{}{
			"port":         8081,
			"soap_version": "1.1",
			"namespace":    "http://myservice.example.com",
		},
	}

	conn, err := f.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	if conn.Name() != "soap_srv" {
		t.Errorf("expected name=soap_srv, got %s", conn.Name())
	}
}

func TestFactoryBothEndpointAndPort(t *testing.T) {
	f := NewFactory(nil)
	cfg := &connector.Config{
		Name: "invalid",
		Type: "soap",
		Properties: map[string]interface{}{
			"endpoint": "https://example.com",
			"port":     8080,
		},
	}

	_, err := f.Create(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for both endpoint and port")
	}
}

func TestFactoryNeitherEndpointNorPort(t *testing.T) {
	f := NewFactory(nil)
	cfg := &connector.Config{
		Name:       "invalid",
		Type:       "soap",
		Properties: map[string]interface{}{},
	}

	_, err := f.Create(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for neither endpoint nor port")
	}
}

func TestClientConnectorInterfaces(t *testing.T) {
	client := NewClient("test", "http://localhost", "1.1", "", 5*time.Second, nil, nil)

	// Verify client implements Reader and Writer
	var _ connector.Reader = client
	var _ connector.Writer = client
}

func TestEnvelopeNestedBody(t *testing.T) {
	body := map[string]interface{}{
		"Customer": map[string]interface{}{
			"Name":  "Alice",
			"Email": "alice@example.com",
		},
		"Items": []interface{}{
			map[string]interface{}{"Name": "Widget", "Qty": "5"},
			map[string]interface{}{"Name": "Gadget", "Qty": "3"},
		},
	}

	env, err := Envelope("1.1", "http://example.com", "CreateOrder", body)
	if err != nil {
		t.Fatalf("Envelope error: %v", err)
	}

	s := string(env)
	if !strings.Contains(s, "<Name>Alice</Name>") {
		t.Errorf("expected nested Customer/Name, got: %s", s)
	}
	if !strings.Contains(s, "<Items>") {
		t.Errorf("expected Items elements, got: %s", s)
	}
}
