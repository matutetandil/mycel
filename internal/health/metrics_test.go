package health

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/internal/metrics"
)

type fakeChecker struct {
	name     string
	connType string
	err      error
}

func (f *fakeChecker) Name() string                   { return f.name }
func (f *fakeChecker) Type() string                   { return f.connType }
func (f *fakeChecker) Health(_ context.Context) error { return f.err }

// typelessChecker implements only the Checker interface, with no Type().
type typelessChecker struct{ name string }

func (t *typelessChecker) Name() string                   { return t.name }
func (t *typelessChecker) Health(_ context.Context) error { return nil }

func scrape(t *testing.T) string {
	t.Helper()

	rec := httptest.NewRecorder()
	metrics.Default().Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec.Body.String()
}

// mycel_connector_health was defined and documented from the start but never
// set, so it never appeared at /metrics. The health endpoint had the answer all
// along; this wires the two together.
func TestCheckAll_RecordsConnectorHealth(t *testing.T) {
	metrics.SetDefault(metrics.NewRegistry("test", "1", "1", "test"))

	m := NewManager("1")
	m.Register(&fakeChecker{name: "orders_db", connType: "database"})
	m.Register(&fakeChecker{name: "payments_api", connType: "http", err: errors.New("connection refused")})

	resp := m.checkAll(context.Background())
	if resp.Status != "unhealthy" {
		t.Errorf("overall status = %q, want unhealthy", resp.Status)
	}

	out := scrape(t)
	for _, want := range []string{
		`mycel_connector_health{connector="orders_db",type="database"} 1`,
		`mycel_connector_health{connector="payments_api",type="http"} 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in /metrics output", want)
		}
	}
}

// Checker only requires Name and Health, so a checker that does not report a
// type must still be recorded rather than dropped or labelled with an empty
// string.
func TestCheckAll_CheckerWithoutTypeIsStillRecorded(t *testing.T) {
	metrics.SetDefault(metrics.NewRegistry("test", "1", "1", "test"))

	m := NewManager("1")
	m.Register(&typelessChecker{name: "plugin_thing"})
	m.checkAll(context.Background())

	if got := scrape(t); !strings.Contains(got, `mycel_connector_health{connector="plugin_thing",type="unknown"} 1`) {
		t.Errorf("checker without Type() not recorded: %s", got)
	}
}
