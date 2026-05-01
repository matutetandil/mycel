package http

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
)

// TestTLSInsecureSkipVerifySuccess: a connector pointed at a self-signed
// HTTPS server should succeed when insecure_skip_verify is enabled.
func TestTLSInsecureSkipVerifySuccess(t *testing.T) {
	srv := httptest.NewTLSServer(nil)
	defer srv.Close()

	c := NewWithTLS(
		"selfsigned",
		srv.URL,
		0, nil,
		&TLSConfig{InsecureSkipVerify: true},
		nil, 1,
	)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// The httptest server returns 404 for any path; what matters here is
	// that the request completed past the TLS handshake. A TLS error would
	// surface as x509 / certificate / tls in the error string.
	_, err := c.Read(context.Background(), connector.Query{Target: "GET /"})
	if err != nil && (strings.Contains(err.Error(), "x509") || strings.Contains(err.Error(), "certificate")) {
		t.Errorf("Read with insecure_skip_verify=true should not fail TLS verification, got: %v", err)
	}
}

// TestTLSInsecureSkipVerifyDefaultRejects: same server, no skip_verify, must
// fail with the expected x509 error.
func TestTLSInsecureSkipVerifyDefaultRejects(t *testing.T) {
	srv := httptest.NewTLSServer(nil)
	defer srv.Close()

	c := New("default", srv.URL, 0, nil, nil, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	_, err := c.Read(context.Background(), connector.Query{Target: "GET /"})
	if err == nil {
		t.Fatalf("expected TLS verification error, got nil")
	}
	if !strings.Contains(err.Error(), "x509") && !strings.Contains(err.Error(), "tls") && !strings.Contains(err.Error(), "certificate") {
		t.Errorf("expected x509/tls/certificate error, got: %v", err)
	}
}

// TestTLSInsecureSkipVerifyEmitsWarn: enabling skip_verify must produce a
// single WARN log at Connect() time. This is the safety net that ensures a
// misconfigured production deploy is obvious in the logs.
func TestTLSInsecureSkipVerifyEmitsWarn(t *testing.T) {
	var buf bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(original)

	c := NewWithTLS("magento", "https://example.com", 0, nil,
		&TLSConfig{InsecureSkipVerify: true}, nil, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "TLS verification disabled") {
		t.Errorf("expected WARN about disabled TLS verification, got: %s", out)
	}
	if !strings.Contains(out, "magento") {
		t.Errorf("expected connector name in WARN, got: %s", out)
	}
	// Must fire exactly once.
	if got := strings.Count(out, "TLS verification disabled"); got != 1 {
		t.Errorf("expected exactly one WARN, got %d", got)
	}
}

// TestOutboundBodyDebugLog: when MYCEL_LOG_LEVEL=debug, the HTTP connector
// emits a single log line per request with method, path, body size and the
// payload's top-level keys. Values are NOT logged. Allows users to confirm
// wrap / envelope behavior end-to-end without intercepting traffic.
func TestOutboundBodyDebugLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(original)

	c := New("api", srv.URL, 0, nil, nil, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	_, err := c.Write(context.Background(), &connector.Data{
		Target:    "POST /post",
		Operation: "POST",
		Payload: map[string]interface{}{
			"productData": map[string]interface{}{
				"sku":  "AI02LT",
				"name": "Axil",
			},
		},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "outbound HTTP body") {
		t.Errorf("expected outbound body log line, got: %s", out)
	}
	if !strings.Contains(out, "top_level_keys=[productData]") {
		t.Errorf("expected top_level_keys=[productData] in log, got: %s", out)
	}
	if !strings.Contains(out, "method=POST") {
		t.Errorf("expected method=POST in log, got: %s", out)
	}
	// Sanity: values must not appear in the log.
	if strings.Contains(out, "AI02LT") || strings.Contains(out, "Axil") {
		t.Errorf("payload values must not be in DEBUG log, got: %s", out)
	}
}

// TestOutboundBodyNoLogAtInfo: at INFO level, the body log must be silent —
// users on the default level should see zero noise.
func TestOutboundBodyNoLogAtInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(original)

	c := New("api", srv.URL, 0, nil, nil, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_, _ = c.Write(context.Background(), &connector.Data{
		Target:    "POST /post",
		Operation: "POST",
		Payload:   map[string]interface{}{"x": 1},
	})

	if strings.Contains(buf.String(), "outbound HTTP body") {
		t.Errorf("body log must be silent at INFO level, got: %s", buf.String())
	}
}

// TestRetryPreservesBodyAcrossAttempts is the regression test for the
// silent data-loss bug where the connector built a bytes.Reader once
// before the retry loop. The first attempt consumed the reader; the
// second attempt sent an empty body, turning a transient 5xx that
// might have recovered into a permanent 400 "field required" — and
// when the server had committed a side effect before the 5xx, the
// retry never overwrote it because the second request was empty.
func TestRetryPreservesBodyAcrossAttempts(t *testing.T) {
	var (
		mu       sync.Mutex
		bodies   [][]byte
		attempts int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		attempts++
		current := attempts
		mu.Unlock()

		// First attempt 500 (transient — connector should retry).
		// Second attempt 200 (success).
		if current == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"err":"transient"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// retryCount = 3 so the connector retries the 500.
	c := New("api", srv.URL, 0, nil, nil, 3)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	payload := map[string]interface{}{
		"productData": map[string]interface{}{
			"sku":  "AI02LT",
			"name": "Axil",
		},
	}
	if _, err := c.Write(context.Background(), &connector.Data{
		Target:    "POST /post",
		Operation: "POST",
		Payload:   payload,
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if attempts != 2 {
		t.Fatalf("expected exactly 2 attempts (500 then 200), got %d", attempts)
	}
	if len(bodies[0]) == 0 {
		t.Error("first attempt body must not be empty")
	}
	if len(bodies[1]) == 0 {
		t.Fatal("regression: second attempt body was empty (bytes.Reader exhausted)")
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Errorf("retry body must be identical to the first; got first=%s retry=%s",
			bodies[0], bodies[1])
	}
}

// TestTLSDefaultEmitsNoWarn: no flag, no WARN. Important — we don't want
// log noise on the common path.
func TestTLSDefaultEmitsNoWarn(t *testing.T) {
	var buf bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(original)

	c := New("safe", "https://example.com", 0, nil, nil, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if strings.Contains(buf.String(), "TLS verification disabled") {
		t.Errorf("WARN must not fire when insecure_skip_verify is false, got: %s", buf.String())
	}
}
