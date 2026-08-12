package grpc

import (
	"strings"
	"testing"
)

// A server told to use TLS must not start without it. The failure mode this
// guards against is the quiet one: the certificate cannot be loaded, the error
// is dropped, the listener comes up in plaintext, and the operator has every
// reason to believe the traffic is encrypted because that is what the
// configuration says.

func TestServerRefusesToStartWhenTheCertificateCannotBeLoaded(t *testing.T) {
	c := &ServerConnector{config: &ServerConfig{
		TLS: &TLSConfig{
			Enabled:  true,
			CertFile: "/nonexistent/server.pem",
			KeyFile:  "/nonexistent/server.key",
		},
	}}

	_, err := c.buildServerOptions()
	if err == nil {
		t.Fatal("a missing certificate was accepted, so the server would have started in plaintext")
	}
	if !strings.Contains(err.Error(), "could not be loaded") {
		t.Errorf("error = %q, want it to say the certificate could not be loaded", err)
	}
}

func TestServerRefusesToStartWhenTLSIsEnabledWithNoCertificate(t *testing.T) {
	// The likeliest way to arrive here: the block is written with the names one
	// of the other connectors uses, so nothing lands in CertFile at all.
	c := &ServerConnector{config: &ServerConfig{TLS: &TLSConfig{Enabled: true}}}

	_, err := c.buildServerOptions()
	if err == nil {
		t.Fatal("TLS with no certificate was accepted")
	}
	if !strings.Contains(err.Error(), "no certificate") {
		t.Errorf("error = %q, want it to name the missing certificate", err)
	}
}

func TestServerWithoutTLSIsUnaffected(t *testing.T) {
	for _, name := range []string{"no tls block", "tls disabled"} {
		t.Run(name, func(t *testing.T) {
			cfg := &ServerConfig{}
			if name == "tls disabled" {
				cfg.TLS = &TLSConfig{Enabled: false, CertFile: "/nonexistent/server.pem"}
			}
			c := &ServerConnector{config: cfg}
			if _, err := c.buildServerOptions(); err != nil {
				t.Errorf("a plaintext server failed to build options: %v", err)
			}
		})
	}
}

func TestMessageSizeOptionsStillApply(t *testing.T) {
	c := &ServerConnector{config: &ServerConfig{MaxRecv: 8, MaxSend: 16}}
	opts, err := c.buildServerOptions()
	if err != nil {
		t.Fatalf("buildServerOptions: %v", err)
	}
	if len(opts) != 2 {
		t.Errorf("got %d options, want the two message-size ones", len(opts))
	}
}
