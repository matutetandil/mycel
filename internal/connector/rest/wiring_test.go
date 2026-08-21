package rest

import (
	"context"
	"encoding/xml"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/health"
	"github.com/matutetandil/mycel/v2/internal/metrics"
)

// What the runtime hands this connector before it starts.
//
// These are the four wiring points — the format a flow declared, the health
// manager, the metrics registry and the rate limiter — and each is the whole of
// how a feature reaches an HTTP response. A format that does not arrive is a
// flow declaring `format = "xml"` and answering JSON, which is exactly what it
// did until it was fixed.
func TestTheFormatAFlowDeclaredReachesItsAnswer(t *testing.T) {
	c := New("api", 8080, nil, slog.Default())

	c.RegisterRoute("GET /items", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"id": 1, "name": "Widget"}, nil
	})
	c.SetOperationFormat("GET /items", "xml")

	c.setupRoutes()

	w := httptest.NewRecorder()
	c.mux.ServeHTTP(w, httptest.NewRequest("GET", "/items", nil))

	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "xml") {
		t.Errorf("content type = %q, want the format the flow declared", got)
	}
	// And it is XML somebody can parse, not JSON with an XML label on it.
	var into struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &into); err != nil {
		t.Errorf("the body is not XML: %v\n%s", err, w.Body.String())
	}
}

// The rest of the wiring, and that a connector works without any of it — the
// runtime sets these only when the configuration asks for them.
func TestAConnectorWorksWithAndWithoutItsWiring(t *testing.T) {
	c := New("api", 8080, nil, slog.Default())

	c.RegisterRoute("GET /items", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"id": 1}, nil
	})
	c.setupRoutes()

	bare := httptest.NewRecorder()
	c.mux.ServeHTTP(bare, httptest.NewRequest("GET", "/items", nil))
	if bare.Code != http.StatusOK {
		t.Fatalf("with nothing wired the request answered %d", bare.Code)
	}

	// Wired the way the runtime wires it.
	c.SetHealthManager(health.NewManager("test"))
	c.SetMetrics(metrics.NewRegistry("mycel", "0.0.0", "test", "test"))

	wired := httptest.NewRecorder()
	c.mux.ServeHTTP(wired, httptest.NewRequest("GET", "/items", nil))
	if wired.Code != http.StatusOK {
		t.Errorf("with health and metrics wired the request answered %d", wired.Code)
	}
}
