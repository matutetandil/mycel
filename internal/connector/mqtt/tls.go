package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// buildTLSConfig creates a *tls.Config from the TLSConfig.
func buildTLSConfig(cfg *TLSConfig) (*tls.Config, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}

	// The authority the broker's certificate is checked against.
	//
	// It was read from the configuration into CAFile and then never used, so
	// a broker with a certificate signed by a private authority — which is
	// what an internal MQTT broker has — could not be verified. The way past
	// that is insecure_skip_verify, which turns off verification altogether:
	// a setting written to work around this one silently gives up the part of
	// TLS that says who is on the other end.
	if cfg.CAFile != "" {
		authority, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate %s: %w", cfg.CAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(authority) {
			return nil, fmt.Errorf("no certificate found in %s", cfg.CAFile)
		}
		tlsConfig.RootCAs = pool
	}

	// Load client certificate if provided
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}
