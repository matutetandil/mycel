package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterPprofHandlers_Gated(t *testing.T) {
	// Off by default (env unset).
	t.Setenv("MYCEL_PPROF", "")
	if registerPprofHandlers(http.NewServeMux()) {
		t.Fatal("pprof must be disabled when MYCEL_PPROF is unset")
	}

	// Off for non-truthy values.
	t.Setenv("MYCEL_PPROF", "no")
	if registerPprofHandlers(http.NewServeMux()) {
		t.Fatal("pprof must be disabled for MYCEL_PPROF=no")
	}

	// On when enabled, and the goroutine profile actually responds.
	t.Setenv("MYCEL_PPROF", "true")
	mux := http.NewServeMux()
	if !registerPprofHandlers(mux) {
		t.Fatal("pprof must be enabled when MYCEL_PPROF=true")
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/pprof/goroutine?debug=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /debug/pprof/goroutine, got %d", rec.Code)
	}
}
