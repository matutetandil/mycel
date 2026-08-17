package kafka

import (
	"crypto/rand"
	"crypto/rsa"
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

// Every managed Kafka a service actually connects to wants credentials and TLS:
// Confluent and Aiven want SASL over TLS, MSK wants one or the other. This is
// the part that decides whether a service can reach its broker at all, and
// getting it wrong shows up as a connection that never establishes — never as a
// message that looks wrong.

func withSASL(mechanism, username, password string) *Connector {
	return &Connector{
		config: &Config{
			SASL: &SASLConfig{
				Mechanism: mechanism, Username: username, Password: password,
			},
		},
	}
}

func TestTheMechanismsABrokerAsksFor(t *testing.T) {
	for _, mechanism := range []string{"PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"} {
		t.Run(mechanism, func(t *testing.T) {
			built, err := withSASL(mechanism, "svc", "s3cret").buildSASLMechanism()
			if err != nil {
				t.Fatalf("buildSASLMechanism: %v", err)
			}
			if built == nil {
				t.Fatal("no mechanism was built")
			}
			if built.Name() == "" {
				t.Error("the mechanism does not name itself to the broker")
			}
		})
	}
}

func TestAMechanismNobodyImplementsIsRefused(t *testing.T) {
	// Refusing by name beats connecting with no credentials and failing at the
	// broker with something less specific.
	_, err := withSASL("GSSAPI", "svc", "s3cret").buildSASLMechanism()
	if err == nil {
		t.Fatal("a mechanism this connector cannot speak was accepted")
	}
	if !strings.Contains(err.Error(), "GSSAPI") {
		t.Errorf("error = %q, want it to name what was asked for", err)
	}
}

func TestNoCredentialsMeansNoMechanism(t *testing.T) {
	// A broker with no authentication is the ordinary local case.
	c := &Connector{config: &Config{}}
	mechanism, err := c.buildSASLMechanism()
	if err != nil {
		t.Fatalf("buildSASLMechanism: %v", err)
	}
	if mechanism != nil {
		t.Error("a mechanism was built for a broker that asks for none")
	}
}

// selfSigned writes a certificate and its key, the way a broker's CA bundle and
// a client certificate arrive on disk.
func selfSigned(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "kafka.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating a certificate: %v", err)
	}

	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("writing the certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}), 0o600); err != nil {
		t.Fatalf("writing the key: %v", err)
	}
	return certFile, keyFile
}

func TestTLSIsOffUnlessItIsAskedFor(t *testing.T) {
	var absent *TLSConfig
	if built, err := absent.BuildTLSConfig(); err != nil || built != nil {
		t.Errorf("built = %v, err = %v, want nothing at all", built, err)
	}

	off := &TLSConfig{Enabled: false, CAFile: "/does/not/exist"}
	if built, err := off.BuildTLSConfig(); err != nil || built != nil {
		t.Errorf("built = %v, err = %v, want nothing at all", built, err)
	}
}

func TestATrustedAuthorityIsLoaded(t *testing.T) {
	// A managed broker presents a certificate signed by its own authority, so
	// without this the connection is refused by the client.
	dir := t.TempDir()
	certFile, _ := selfSigned(t, dir)

	built, err := (&TLSConfig{Enabled: true, CAFile: certFile}).BuildTLSConfig()
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if built.RootCAs == nil {
		t.Error("the authority was not loaded, so the broker's certificate cannot be checked")
	}
	// And nothing weaker than TLS 1.2, whatever the broker offers.
	if built.MinVersion < 0x0303 {
		t.Errorf("minimum version = %x", built.MinVersion)
	}
}

func TestAClientCertificateIsPresented(t *testing.T) {
	// Mutual TLS: the broker checks the service too.
	dir := t.TempDir()
	certFile, keyFile := selfSigned(t, dir)

	built, err := (&TLSConfig{Enabled: true, CertFile: certFile, KeyFile: keyFile}).BuildTLSConfig()
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if len(built.Certificates) != 1 {
		t.Error("no client certificate was presented, so a broker asking for one refuses the connection")
	}
}

func TestCertificatesThatCannotBeLoadedAreReported(t *testing.T) {
	// At startup, where somebody can fix the path — not as a connection that
	// fails for reasons the log does not explain.
	dir := t.TempDir()
	certFile, keyFile := selfSigned(t, dir)

	notPEM := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notPEM, []byte("this is not a certificate"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	for name, config := range map[string]*TLSConfig{
		"an authority file that is not there": {Enabled: true, CAFile: filepath.Join(dir, "absent.pem")},
		"an authority file that is not one":   {Enabled: true, CAFile: notPEM},
		"a client certificate that is not there": {
			Enabled: true, CertFile: filepath.Join(dir, "absent.pem"), KeyFile: keyFile,
		},
		"a client key that does not match": {
			Enabled: true, CertFile: certFile, KeyFile: notPEM,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := config.BuildTLSConfig(); err == nil {
				t.Error("it was accepted")
			}
		})
	}
}

func TestSkippingVerificationIsCarriedThrough(t *testing.T) {
	// A setting for a broker with a self-signed certificate and nobody's
	// authority to check it against. It has to reach the connection, or
	// somebody sets it and the handshake still fails.
	built, err := (&TLSConfig{Enabled: true, InsecureSkipVerify: true}).BuildTLSConfig()
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if !built.InsecureSkipVerify {
		t.Error("the setting was dropped between the configuration and the connection")
	}
}
