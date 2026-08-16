package mqtt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Talking to a broker over TLS.
//
// An MQTT broker inside a company has a certificate signed by that company's
// own authority, so verifying it means being told where that authority is.
// The setting for it was read from the configuration and never used — and the
// way past that is insecure_skip_verify, which does not fix verification, it
// turns it off.

func certificateFiles(t *testing.T) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "broker.internal"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	write(t, certPath, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	marshalled, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	write(t, keyPath, &pem.Block{Type: "EC PRIVATE KEY", Bytes: marshalled})

	return certPath, keyPath
}

func write(t *testing.T, path string, block *pem.Block) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := pem.Encode(file, block); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyingABrokerSignedByAPrivateAuthority(t *testing.T) {
	authority, _ := certificateFiles(t)

	config, err := buildTLSConfig(&TLSConfig{Enabled: true, CAFile: authority})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}

	if config.RootCAs == nil {
		t.Error("the certificate authority was read from the configuration and not used, " +
			"so an internal broker cannot be verified at all")
	}
	if config.InsecureSkipVerify {
		t.Error("verification is off and nobody asked for that")
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Errorf("minimum version = %x, want TLS 1.2 or better", config.MinVersion)
	}
}

func TestAnAuthorityThatCannotBeUsed(t *testing.T) {
	// Read as nothing, the client falls back to the public authorities and
	// the connection fails much later with an error about an unknown
	// authority — a long way from the file that was wrong.
	if _, err := buildTLSConfig(&TLSConfig{Enabled: true, CAFile: "/nonexistent/ca.pem"}); err == nil {
		t.Error("a certificate authority file that is not there was accepted")
	}

	notACertificate := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(notACertificate, []byte("this is not a certificate"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := buildTLSConfigErr(t, &TLSConfig{Enabled: true, CAFile: notACertificate})
	if err == nil {
		t.Fatal("a file that is not a certificate was accepted as one")
	}
	if !strings.Contains(err.Error(), "no certificate") {
		t.Errorf("the error does not say what was wrong: %v", err)
	}
}

func TestABrokerThatChecksTheClientToo(t *testing.T) {
	certPath, keyPath := certificateFiles(t)

	config, err := buildTLSConfig(&TLSConfig{Enabled: true, CertFile: certPath, KeyFile: keyPath})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if len(config.Certificates) != 1 {
		t.Error("the client's own certificate was not loaded, so the broker has nothing to check it by")
	}

	// A certificate with no key is refused rather than the connection being
	// made without one.
	if _, err := buildTLSConfig(&TLSConfig{
		Enabled: true, CertFile: certPath, KeyFile: "/nonexistent/key.pem",
	}); err == nil {
		t.Error("a client certificate with no key was accepted")
	}
}

func TestTLSThatWasNotAskedFor(t *testing.T) {
	// No TLS block, and a block that is present and switched off: both mean
	// a plain connection, and neither is an error.
	for name, cfg := range map[string]*TLSConfig{
		"no block":       nil,
		"switched off":   {Enabled: false},
		"off with files": {Enabled: false, CAFile: "/nonexistent/ca.pem"},
	} {
		t.Run(name, func(t *testing.T) {
			config, err := buildTLSConfig(cfg)
			if err != nil {
				t.Errorf("buildTLSConfig: %v", err)
			}
			if config != nil {
				t.Errorf("a connector that asked for no TLS got %+v", config)
			}
		})
	}
}

func TestTurningVerificationOffOnPurpose(t *testing.T) {
	config, err := buildTLSConfig(&TLSConfig{Enabled: true, InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("buildTLSConfig: %v", err)
	}
	if !config.InsecureSkipVerify {
		t.Error("verification was asked to be off and is on")
	}
}

func buildTLSConfigErr(t *testing.T, cfg *TLSConfig) error {
	t.Helper()
	_, err := buildTLSConfig(cfg)
	return err
}
