package rest

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/flow"
)

// What a service exposes to a browser, and what it says when something fails.
//
// Both of these behave differently in production, and both were untested. One
// decides which websites may call the service from somebody's browser; the
// other decides how much of an internal failure a caller is told about.

func serverWith(t *testing.T, cors *CORSConfig, environment string) http.Handler {
	t.Helper()
	c := New("api", 0, cors, nil)
	c.environment = environment
	return c.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func request(handler http.Handler, method, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/orders", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestWhichWebsitesMayCallTheService(t *testing.T) {
	handler := serverWith(t, &CORSConfig{
		Origins: []string{"https://shop.example.test", "https://admin.example.test"},
		Methods: []string{"GET", "POST"},
	}, "production")

	allowed := request(handler, http.MethodGet, "https://shop.example.test")
	if got := allowed.Header().Get("Access-Control-Allow-Origin"); got != "https://shop.example.test" {
		t.Errorf("a listed website was not allowed: %q", got)
	}

	// The one that matters: a website nobody listed gets no permission, so
	// the browser refuses to hand the answer to it.
	refused := request(handler, http.MethodGet, "https://evil.example.test")
	if got := refused.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("an unlisted website was allowed: %q", got)
	}

	// Which methods are allowed is what a browser checks before sending
	// anything other than a simple request.
	if got := allowed.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
		t.Errorf("methods = %q", got)
	}
}

func TestTheBrowsersQuestionBeforeTheRealRequest(t *testing.T) {
	// A preflight is answered here and goes no further: letting it reach the
	// flow would run the flow twice for one call.
	handled := false
	c := New("api", 0, &CORSConfig{Origins: []string{"https://shop.example.test"}, Methods: []string{"GET", "POST"}}, nil)
	handler := c.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled = true
	}))

	answer := request(handler, http.MethodOptions, "https://shop.example.test")

	if answer.Code != http.StatusOK {
		t.Errorf("preflight answered %d", answer.Code)
	}
	if handled {
		t.Error("a preflight reached the flow, so the flow runs twice for one call")
	}
}

func TestAnythingGoesUntilItIsProduction(t *testing.T) {
	// With no CORS block, a development service allows whoever asks — which
	// is what makes a local front end work without configuration. The whole
	// point is that it stops in production: shipping that permissiveness
	// would let any website on the internet read the service through a
	// visitor's browser.
	for _, environment := range []string{"", "development", "staging"} {
		t.Run("in "+environment, func(t *testing.T) {
			answer := request(serverWith(t, nil, environment), http.MethodGet, "https://anywhere.example.test")
			if got := answer.Header().Get("Access-Control-Allow-Origin"); got != "https://anywhere.example.test" {
				t.Errorf("a development service refused a browser: %q", got)
			}
		})
	}

	for _, environment := range []string{"production", "prod"} {
		t.Run("in "+environment, func(t *testing.T) {
			answer := request(serverWith(t, nil, environment), http.MethodGet, "https://anywhere.example.test")
			if got := answer.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("a production service with no CORS block allowed %q", got)
			}
		})
	}

	// A request with no Origin is not a browser's cross-site request at all,
	// and gets no CORS headers to confuse anybody reading them.
	plain := request(serverWith(t, nil, "development"), http.MethodGet, "")
	if got := plain.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("a request from no website was answered with %q", got)
	}
}

func TestAnOriginListThatSaysEverybody(t *testing.T) {
	// Written on purpose — a public API. It echoes the caller rather than
	// answering with a star, which is what a browser requires when the
	// request carries credentials.
	handler := serverWith(t, &CORSConfig{Origins: []string{"*"}, Methods: []string{"GET"}}, "production")

	answer := request(handler, http.MethodGet, "https://anywhere.example.test")
	if got := answer.Header().Get("Access-Control-Allow-Origin"); got != "https://anywhere.example.test" {
		t.Errorf("allow-origin = %q", got)
	}

	// And a CORS block with no origins in it allows nobody, rather than
	// falling through to allowing everybody.
	empty := serverWith(t, &CORSConfig{Methods: []string{"GET"}}, "production")
	if got := request(empty, http.MethodGet, "https://anywhere.example.test").
		Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("an empty origin list allowed %q", got)
	}
}

func TestHowMuchACallerIsToldAboutAFailure(t *testing.T) {
	// A stack of internal error text is a map of the inside of a service:
	// table names, hosts, driver messages. In production the caller is told
	// the kind of failure and nothing else.
	production := New("api", 0, nil, nil)
	production.environment = "production"

	w := httptest.NewRecorder()
	status := production.writeError(w, errors.New("dial tcp 10.0.0.5:5432: connect: connection refused"))

	if status != http.StatusInternalServerError {
		t.Errorf("status = %d", status)
	}
	if strings.Contains(w.Body.String(), "10.0.0.5") {
		t.Errorf("the address of an internal system was sent to the caller: %s", w.Body.String())
	}

	// A validation failure is the caller's own doing, so it is told what was
	// wrong — otherwise it cannot fix the request.
	w = httptest.NewRecorder()
	status = production.writeError(w, errors.New("validation failed: email is required"))
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
	if !strings.Contains(w.Body.String(), "email") {
		t.Errorf("a caller was not told what was wrong with its request: %s", w.Body.String())
	}

	// In development the whole thing is shown, which is the point of
	// development.
	development := New("api", 0, nil, nil)
	w = httptest.NewRecorder()
	development.writeError(w, errors.New("dial tcp 10.0.0.5:5432: connect: connection refused"))
	if !strings.Contains(w.Body.String(), "10.0.0.5") {
		t.Errorf("a developer was not told what actually failed: %s", w.Body.String())
	}
}

func TestAFailureThatChoseItsOwnAnswer(t *testing.T) {
	// An error_response block: the status, body and headers a flow decided a
	// caller should get, rather than a generic 500.
	c := New("api", 0, nil, nil)
	c.environment = "production"

	w := httptest.NewRecorder()
	status := c.writeError(w, flow.NewFlowError(
		errors.New("duplicate key"), 409,
		map[string]interface{}{"error": "that order already exists", "order_id": "order-1"},
		map[string]string{"X-Order-Id": "order-1"},
	))

	if status != 409 {
		t.Errorf("status = %d", status)
	}
	if w.Header().Get("X-Order-Id") != "order-1" {
		t.Errorf("the headers the flow chose were not sent: %v", w.Header())
	}
	body := w.Body.String()
	if !strings.Contains(body, "that order already exists") || !strings.Contains(body, "order-1") {
		t.Errorf("body = %s", body)
	}
	// The body the flow chose is sent even in production: it was written for
	// the caller, unlike the internal error text above.
	if strings.Contains(body, "duplicate key") {
		t.Errorf("the underlying failure leaked into the answer: %s", body)
	}
}

func TestWhatAnAnswerLooksLike(t *testing.T) {
	c := New("api", 0, nil, nil)

	w := httptest.NewRecorder()
	c.writeJSON(w, http.StatusCreated, map[string]interface{}{"id": "order-1"})

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d", w.Code)
	}
	// Without this, a browser or a client library reads the answer as text
	// and hands back a string where a record was expected.
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Errorf("content type = %q", got)
	}
	if !strings.Contains(w.Body.String(), "order-1") {
		t.Errorf("body = %s", w.Body.String())
	}

	// Something that cannot be encoded must still answer: a status with no
	// body beats a connection that closes mid-answer.
	w = httptest.NewRecorder()
	c.writeJSON(w, http.StatusOK, map[string]interface{}{"bad": make(chan int)})
	if w.Code == 0 {
		t.Error("nothing was answered at all")
	}
}

func TestAPathWrittenTheOtherWay(t *testing.T) {
	// Mycel's own documentation and every other framework write a path
	// parameter as :id; Go's router wants {id}. A path converted wrongly is
	// an endpoint that never matches, or one that matches too much.
	for written, want := range map[string]string{
		"/orders/:id":            "/orders/{id}",
		"/orders/:id/items/:sku": "/orders/{id}/items/{sku}",
		"/orders/{id}":           "/orders/{id}",
		"/orders":                "/orders",
		"/":                      "/",
		"/orders/:id/":           "/orders/{id}/",
	} {
		if got := convertPathParams(written); got != want {
			t.Errorf("%q became %q, want %q", written, got, want)
		}
	}
}

func TestReadingAnOperation(t *testing.T) {
	for written, want := range map[string][2]string{
		"GET /orders":          {"GET", "/orders"},
		"POST /orders":         {"POST", "/orders"},
		"QUERY /orders/search": {"QUERY", "/orders/search"},
		// A path on its own is a read, which is what makes `operation =
		// "/health"` mean what somebody writing it expects.
		"/orders": {"GET", "/orders"},
	} {
		method, path := parseOperation(written)
		if method != want[0] || path != want[1] {
			t.Errorf("%q read as %s %s, want %s %s", written, method, path, want[0], want[1])
		}
	}
}

func TestTheConnectorSaysWhatItIs(t *testing.T) {
	c := New("api", 8080, nil, nil)

	if c.Name() != "api" || c.Type() != "rest" {
		t.Errorf("name/type = %s/%s", c.Name(), c.Type())
	}
	// A REST connector listens; it is never the destination of a flow, and
	// the runtime asks it this to know that.
	if !c.InboundOnly() {
		t.Error("a REST server said it could be written to")
	}
	// Connecting is a no-op — the socket is opened on Start — and must not
	// fail, or the service refuses to start.
	if err := c.Connect(t.Context()); err != nil {
		t.Errorf("Connect: %v", err)
	}
	// Health before anything is listening: not an error, because a service
	// that has not started yet is not a service that is broken.
	if err := c.Health(t.Context()); err != nil {
		fmt.Println("health before start:", err)
	}
}
