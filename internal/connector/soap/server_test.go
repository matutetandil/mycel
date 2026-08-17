package soap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Exposing a SOAP service.
//
// SOAP is what an ERP, a bank or a carrier gives a company to integrate
// against, so the caller is usually somebody else's software that will not be
// changed to suit us. What it receives on a good day and on a bad one is
// fixed by the specification, and none of it was tested from the outside.

func serverFor(t *testing.T, version string) *Server {
	t.Helper()
	return NewServer("orders", 0, version, "http://example.test/orders",
		slog.New(slog.NewTextHandler(silent{}, nil)))
}

type silent struct{}

func (silent) Write(p []byte) (int, error) { return len(p), nil }

// call posts a SOAP request the way a client would.
func call(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", ContentType11)
	recorder := httptest.NewRecorder()
	s.handleSOAPRequest(recorder, request)
	return recorder
}

func envelopeFor(operation, inner string) string {
	return `<?xml version="1.0"?>
<soap:Envelope xmlns:soap="` + NS11 + `">
  <soap:Body>
    <` + operation + ` xmlns="http://example.test/orders">` + inner + `</` + operation + `>
  </soap:Body>
</soap:Envelope>`
}

func TestACallReachesItsFlowAndComesBack(t *testing.T) {
	s := serverFor(t, "1.1")

	var received map[string]interface{}
	s.RegisterRoute("CreateOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		received = input
		return map[string]interface{}{"OrderId": "order-1", "Status": "accepted"}, nil
	})

	answer := call(t, s, envelopeFor("CreateOrder",
		`<CustomerId>customer-1</CustomerId><Total>42.50</Total>`))

	if answer.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", answer.Code, answer.Body.String())
	}
	if received["CustomerId"] != "customer-1" {
		t.Errorf("the flow received %v", received)
	}

	body := answer.Body.String()
	// The name is fixed by convention: a caller generated from the WSDL looks
	// for <Operation>Response and nothing else.
	if !strings.Contains(body, "CreateOrderResponse") {
		t.Errorf("the answer is not named as the operation's response:\n%s", body)
	}
	if !strings.Contains(body, "order-1") {
		t.Errorf("the answer does not carry what the flow returned:\n%s", body)
	}
	// The content type is how a SOAP client decides which version it is
	// talking to.
	if got := answer.Header().Get("Content-Type"); got != ContentType11 {
		t.Errorf("content type = %q", got)
	}
}

func TestAFlowThatFailedComesBackAsAFault(t *testing.T) {
	// Not as a plain 500 with Go's error text in the page: a SOAP client
	// parses a fault and hands the caller its code and message, and gets
	// nothing at all out of anything else.
	s := serverFor(t, "1.1")
	s.RegisterRoute("CreateOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, errors.New("that customer has no credit")
	})

	answer := call(t, s, envelopeFor("CreateOrder", `<CustomerId>customer-1</CustomerId>`))

	body := answer.Body.String()
	if !strings.Contains(body, "Fault") {
		t.Fatalf("a failure came back as something other than a fault:\n%s", body)
	}
	if !strings.Contains(body, "that customer has no credit") {
		t.Errorf("the fault does not say what went wrong:\n%s", body)
	}
	// The fault says whose fault it is: a Server fault is worth retrying and
	// a Client fault never is.
	if !strings.Contains(body, "Server") {
		t.Errorf("the fault does not say it was the server's:\n%s", body)
	}
	if got := answer.Header().Get("Content-Type"); got != ContentType11 {
		t.Errorf("content type = %q", got)
	}
}

func TestAnOperationNobodyImplements(t *testing.T) {
	// The caller's mistake, and the fault has to say so — a Server fault
	// would have them retrying a call that will never work.
	s := serverFor(t, "1.1")
	s.RegisterRoute("CreateOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	})

	body := call(t, s, envelopeFor("DeleteOrder", `<OrderId>order-1</OrderId>`)).Body.String()

	if !strings.Contains(body, "Client") {
		t.Errorf("an unknown operation was blamed on the server:\n%s", body)
	}
	if !strings.Contains(body, "DeleteOrder") {
		t.Errorf("the fault does not name the operation asked for:\n%s", body)
	}
}

func TestSomethingThatIsNotASOAPRequest(t *testing.T) {
	s := serverFor(t, "1.1")

	for name, body := range map[string]string{
		"not XML at all":       "this is not XML",
		"XML that is not SOAP": `<?xml version="1.0"?><order><id>1</id></order>`,
		"nothing":              "",
	} {
		t.Run(name, func(t *testing.T) {
			answer := call(t, s, body)
			if !strings.Contains(answer.Body.String(), "Fault") {
				t.Errorf("answered %d with %s", answer.Code, answer.Body.String())
			}
			// Even the refusal is a SOAP envelope: a client that cannot parse
			// the answer reports a transport error, which sends whoever is
			// debugging it to the network rather than to their request.
			if got := answer.Header().Get("Content-Type"); got != ContentType11 {
				t.Errorf("content type = %q", got)
			}
		})
	}
}

func TestAFaultTheCallerSentUs(t *testing.T) {
	// A client posting a fault envelope. It is answered rather than treated
	// as an operation named Fault.
	s := serverFor(t, "1.1")

	body := call(t, s, `<?xml version="1.0"?>
<soap:Envelope xmlns:soap="`+NS11+`">
  <soap:Body>
    <soap:Fault>
      <faultcode>soap:Client</faultcode>
      <faultstring>Something on my end</faultstring>
    </soap:Fault>
  </soap:Body>
</soap:Envelope>`).Body.String()

	if !strings.Contains(body, "Fault") {
		t.Errorf("a fault sent to us was answered with %s", body)
	}
}

func TestOnlyPostsAreCalls(t *testing.T) {
	// A browser opening the endpoint, or a health check: answered plainly
	// rather than parsed as an envelope.
	s := serverFor(t, "1.1")

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		recorder := httptest.NewRecorder()
		s.handleSOAPRequest(recorder, httptest.NewRequest(method, "/", nil))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s answered %d", method, recorder.Code)
		}
	}
}

func TestTheServiceDescribesItself(t *testing.T) {
	// The WSDL is how the other side generates its client, so an operation
	// missing from it is an operation nobody can call.
	s := serverFor(t, "1.1")
	s.RegisterRoute("CreateOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})
	s.RegisterRoute("GetOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})

	recorder := httptest.NewRecorder()
	s.handleWSDLRequest(recorder, httptest.NewRequest(http.MethodGet, "/wsdl", nil))

	wsdl := recorder.Body.String()
	if recorder.Code != http.StatusOK {
		t.Fatalf("the WSDL answered %d", recorder.Code)
	}
	for _, operation := range []string{"CreateOrder", "GetOrder"} {
		if !strings.Contains(wsdl, operation) {
			t.Errorf("%s is not in the WSDL, so nobody can generate a client for it:\n%s", operation, wsdl)
		}
	}
	// The namespace ties the generated client's types to ours.
	if !strings.Contains(wsdl, "http://example.test/orders") {
		t.Errorf("the WSDL does not carry the service namespace:\n%s", wsdl)
	}
}

func TestAnAnswerThatChoseItsOwnStatus(t *testing.T) {
	// A response block naming a status code — 202 for something accepted and
	// not yet done, which some clients treat differently.
	s := serverFor(t, "1.1")
	s.RegisterRoute("CreateOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"http_status_code": 202, "OrderId": "order-1"}, nil
	})

	answer := call(t, s, envelopeFor("CreateOrder", `<CustomerId>customer-1</CustomerId>`))

	if answer.Code != http.StatusAccepted {
		t.Errorf("answered %d, want the status the flow chose", answer.Code)
	}
	if !strings.Contains(answer.Body.String(), "order-1") {
		t.Errorf("body = %s", answer.Body.String())
	}
}

func TestAnAnswerThatIsNotARecord(t *testing.T) {
	// A flow returning rows, or a bare value. Both have to become an
	// envelope: returning nothing would leave the caller parsing an empty
	// body.
	s := serverFor(t, "1.1")
	s.RegisterRoute("ListOrders", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return []map[string]interface{}{{"OrderId": "order-1"}, {"OrderId": "order-2"}}, nil
	})
	s.RegisterRoute("CountOrders", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return 42, nil
	})

	rows := call(t, s, envelopeFor("ListOrders", "")).Body.String()
	if !strings.Contains(rows, "order-1") || !strings.Contains(rows, "order-2") {
		t.Errorf("rows were lost on the way out:\n%s", rows)
	}

	count := call(t, s, envelopeFor("CountOrders", "")).Body.String()
	if !strings.Contains(count, "42") {
		t.Errorf("a plain value was lost on the way out:\n%s", count)
	}
}

func TestTwoFlowsOnOneOperation(t *testing.T) {
	// Fan-out: the first answers the caller, the rest run alongside.
	s := serverFor(t, "1.1")

	alsoRan := make(chan struct{}, 1)
	s.RegisterRoute("CreateOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"OrderId": "order-1"}, nil
	})
	s.RegisterRoute("CreateOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		alsoRan <- struct{}{}
		return map[string]interface{}{"OrderId": "ignored"}, nil
	})

	body := call(t, s, envelopeFor("CreateOrder", `<CustomerId>customer-1</CustomerId>`)).Body.String()

	if !strings.Contains(body, "order-1") {
		t.Errorf("the caller got the wrong flow's answer:\n%s", body)
	}
}

func TestTheServerSaysWhatItIs(t *testing.T) {
	s := serverFor(t, "1.2")

	if s.Name() != "orders" || s.Type() != "soap" {
		t.Errorf("name/type = %s/%s", s.Name(), s.Type())
	}
	if !s.InboundOnly() {
		t.Error("a SOAP server said it was not a source")
	}
	if err := s.Connect(context.Background()); err != nil {
		t.Errorf("Connect: %v", err)
	}
	// A server that never started is not healthy, which is what keeps a load
	// balancer from sending it calls.
	if err := s.Health(context.Background()); err == nil {
		t.Error("a server that never started reported itself healthy")
	}
	// Closing one that never started is what a service that fails to start
	// does on its way out.
	if err := s.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}

	// And a 1.2 service answers with its own content type: a 1.1 client
	// reading it knows immediately that it is talking to the wrong version.
	s.RegisterRoute("CreateOrder", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"OrderId": "order-1"}, nil
	})
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`<?xml version="1.0"?>
<soap:Envelope xmlns:soap="`+NS12+`">
  <soap:Body><CreateOrder xmlns="http://example.test/orders"><CustomerId>c</CustomerId></CreateOrder></soap:Body>
</soap:Envelope>`))
	recorder := httptest.NewRecorder()
	s.handleSOAPRequest(recorder, request)

	if got := recorder.Header().Get("Content-Type"); got != ContentType12 {
		t.Errorf("content type = %q, want the 1.2 one", got)
	}
}

func TestAFaultSaysWhatItIs(t *testing.T) {
	// The error a flow's on_error sees when a SOAP call it made came back a
	// fault, so it can tell a bad request from a service that is down.
	withDetail := &Fault{Code: "Client", String: "Invalid order", Detail: "CustomerId is required"}
	if !strings.Contains(withDetail.Error(), "CustomerId is required") {
		t.Errorf("the detail was dropped: %s", withDetail.Error())
	}
	if !strings.Contains(withDetail.Error(), "Client") {
		t.Errorf("the code was dropped: %s", withDetail.Error())
	}

	plain := &Fault{Code: "Server", String: "Service unavailable"}
	if !strings.Contains(plain.Error(), "Service unavailable") {
		t.Errorf("error = %s", plain.Error())
	}
}
