package http

import (
	"context"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCA writes the test server's own certificate out as a PEM file, which is
// what a `ca_cert` in the configuration points at.
func writeCA(t *testing.T, server *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	encoded := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	})
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}
	return path
}

func tlsServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(nil)
	t.Cleanup(server.Close)
	return server
}

// A named CA is the CA that gets used.
func TestACANamedInTheConfigurationIsUsed(t *testing.T) {
	server := tlsServer(t)

	conn := NewWithTLS("api", server.URL, 5*time.Second, nil,
		&TLSConfig{CACert: writeCA(t, server)}, nil, 0)

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.client.Get(server.URL); err != nil {
		t.Errorf("a request to the server whose CA was named failed: %v", err)
	}
}

// Without it, the same server is refused — which is what proves the CA above
// was doing the work rather than the test connecting to anything at all.
func TestAServerSignedByNobodyKnownIsRefused(t *testing.T) {
	server := tlsServer(t)

	conn := NewWithTLS("api", server.URL, 5*time.Second, nil, nil, nil, 0)
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.client.Get(server.URL); err == nil {
		t.Error("a self-signed server was accepted with no CA configured")
	}
}

// insecure_skip_verify does what it says, and the connector says so out loud.
func TestVerificationCanBeSkipped(t *testing.T) {
	server := tlsServer(t)

	conn := NewWithTLS("api", server.URL, 5*time.Second, nil,
		&TLSConfig{InsecureSkipVerify: true}, nil, 0)

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.client.Get(server.URL); err != nil {
		t.Errorf("verification was meant to be skipped: %v", err)
	}
}

// TLS that cannot be built is TLS that is not in force, so the connector has
// to refuse rather than quietly fall back to the system roots.
//
// The build error was discarded: a mistyped ca_cert path left the connector
// verifying against the system roots instead of the CA that was named, and a
// client certificate that would not load meant connecting without one. Both
// look like working TLS from the outside.
func TestATLSConfigurationThatCannotBeBuiltIsRefused(t *testing.T) {
	for name, cfg := range map[string]*TLSConfig{
		"a ca_cert that is not there":  {CACert: "/no/such/ca.pem"},
		"a ca_cert that is not a cert": {CACert: notACert(t)},
		"a client cert that is not there": {
			ClientCert: "/no/such/cert.pem",
			ClientKey:  "/no/such/key.pem",
		},
	} {
		t.Run(name, func(t *testing.T) {
			conn := NewWithTLS("api", "https://example.invalid", time.Second, nil, cfg, nil, 0)
			err := conn.Connect(context.Background())
			if err == nil {
				t.Fatal("the connector started with TLS it could not build")
			}
			if !strings.Contains(err.Error(), "tls configuration") {
				t.Errorf("the error does not say what went wrong: %v", err)
			}
		})
	}
}

func notACert(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "junk.pem")
	if err := os.WriteFile(path, []byte("this is not a certificate\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}
