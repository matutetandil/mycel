package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A flow may serve OPTIONS, and the CORS middleware used to answer every
// OPTIONS request before the flow was asked — so with CORS configured the flow
// was registered, printed in the banner, and unreachable.
//
// The discrimination is the header a browser always sends with a preflight and
// nothing else sends.

func corsConnector(t *testing.T, cors *CORSConfig) http.Handler {
	t.Helper()
	c := &Connector{cors: cors, environment: "production"}
	return c.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
}

func TestAPreflightIsAnsweredByTheMiddleware(t *testing.T) {
	handler := corsConnector(t, &CORSConfig{Origins: []string{"*"}, Methods: []string{"GET", "OPTIONS"}})

	req := httptest.NewRequest(http.MethodOptions, "/anything", nil)
	req.Header.Set("Origin", "https://example.test")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("preflight answered %d, want 200", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("preflight carried no Access-Control-Allow-Methods")
	}
}

func TestAnOptionsRequestForAFlowReachesTheFlow(t *testing.T) {
	handler := corsConnector(t, &CORSConfig{Origins: []string{"*"}, Methods: []string{"GET", "OPTIONS"}})

	// No Access-Control-Request-Method: not a preflight, whoever sent it wants
	// what the flow serving OPTIONS answers.
	req := httptest.NewRequest(http.MethodOptions, "/opts", nil)
	req.Header.Set("Origin", "https://example.test")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Errorf("the request was answered %d by the middleware; the flow behind it was never asked", rec.Code)
	}
}

func TestTheAdvertisedMethodsIncludeHead(t *testing.T) {
	// The permissive development branch lists the methods itself, and left out
	// HEAD — which a flow may serve and the router registers.
	c := &Connector{environment: "development"}
	handler := c.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/items", nil)
	req.Header.Set("Origin", "https://example.test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	allowed := rec.Header().Get("Access-Control-Allow-Methods")
	for _, method := range []string{"GET", "HEAD", "POST", "QUERY", "OPTIONS"} {
		if !strings.Contains(allowed, method) {
			t.Errorf("Access-Control-Allow-Methods = %q, missing %s", allowed, method)
		}
	}
}
