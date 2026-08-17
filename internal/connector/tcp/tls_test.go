package tcp

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

// The TLS a TCP connector is configured with.
//
// A socket carrying orders between two services is either encrypted or it is
// not, and the difference is decided here, from a few file paths. Nothing
// exercised it — not the certificate loading, not the certificate authority a
// self-signed deployment supplies, and not the switch that turns verification
// off, which is the one that quietly makes the encryption prove nothing about
// who is on the other end.

// certificate writes a self-signed certificate and its key, and returns their
// paths.
func certificate(t *testing.T) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "orders.internal"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
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

	certOut, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	_ = certOut.Close()

	marshalled, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyOut, err := os.Create(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: marshalled}); err != nil {
		t.Fatal(err)
	}
	_ = keyOut.Close()

	return certPath, keyPath
}

func TestAServerThatIsAskedToEncrypt(t *testing.T) {
	certPath, keyPath := certificate(t)

	config, err := (&Factory{}).buildServerTLS(map[string]interface{}{
		"cert": certPath,
		"key":  keyPath,
	})
	if err != nil {
		t.Fatalf("buildServerTLS: %v", err)
	}

	if len(config.Certificates) != 1 {
		t.Errorf("certificates = %d, want the one it was given", len(config.Certificates))
	}
	// Anything below 1.2 has known attacks against it and is refused by every
	// current client anyway; leaving the floor unset would let a client
	// negotiate one.
	if config.MinVersion != tls.VersionTLS12 {
		t.Errorf("minimum version = %x, want TLS 1.2 or better", config.MinVersion)
	}
}

func TestAServerAskedToEncryptWithNothingToEncryptWith(t *testing.T) {
	// Refusing at start-up is the point. A server that fell back to plaintext
	// here would accept connections and look like it was working.
	for name, props := range map[string]map[string]interface{}{
		"no certificate at all": {},
		"a certificate and no key": {
			"cert": "/nonexistent/cert.pem",
		},
		"paths that are not there": {
			"cert": "/nonexistent/cert.pem", "key": "/nonexistent/key.pem",
		},
	} {
		t.Run(name, func(t *testing.T) {
			config, err := (&Factory{}).buildServerTLS(props)
			if err == nil {
				t.Fatal("a server was configured to encrypt with nothing to encrypt with")
			}
			if config != nil {
				t.Errorf("a configuration came back anyway: %+v", config)
			}
		})
	}
}

func TestAClientTalkingToAServerItCanVerify(t *testing.T) {
	certPath, _ := certificate(t)

	config, err := (&Factory{}).buildClientTLS(map[string]interface{}{
		"ca_cert": certPath,
	})
	if err != nil {
		t.Fatalf("buildClientTLS: %v", err)
	}

	// The point of naming a certificate authority: an internal service whose
	// certificate no public authority signed can still be verified, rather
	// than verification being turned off to get past it.
	if config.RootCAs == nil {
		t.Error("the certificate authority was read and not used")
	}
	if config.InsecureSkipVerify {
		t.Error("verification is off and nobody asked for that")
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Errorf("minimum version = %x", config.MinVersion)
	}
}

func TestAClientThatPresentsItsOwnCertificate(t *testing.T) {
	// Mutual TLS: the server checks the client too, which is how a socket
	// between two services is restricted to those two.
	certPath, keyPath := certificate(t)

	config, err := (&Factory{}).buildClientTLS(map[string]interface{}{
		"ca_cert": certPath,
		"cert":    certPath,
		"key":     keyPath,
	})
	if err != nil {
		t.Fatalf("buildClientTLS: %v", err)
	}
	if len(config.Certificates) != 1 {
		t.Error("the client's own certificate was not loaded, so the server has nothing to check it by")
	}
}

func TestACertificateAuthorityThatCannotBeUsed(t *testing.T) {
	// Read as nothing, the client falls back to the system authorities and a
	// connection to an internal service fails later with an error about an
	// unknown authority — a long way from the file that was wrong.
	if _, err := (&Factory{}).buildClientTLS(map[string]interface{}{
		"ca_cert": "/nonexistent/ca.pem",
	}); err == nil {
		t.Error("a certificate authority file that is not there was accepted")
	}

	notACertificate := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(notACertificate, []byte("this is not a certificate"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := (&Factory{}).buildClientTLS(map[string]interface{}{"ca_cert": notACertificate})
	if err == nil {
		t.Fatal("a file that is not a certificate was accepted as one")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("the error does not say what was wrong with it: %v", err)
	}

	// A client certificate that cannot be loaded is refused rather than the
	// connection being made without one.
	certPath, _ := certificate(t)
	if _, err := (&Factory{}).buildClientTLS(map[string]interface{}{
		"cert": certPath, "key": "/nonexistent/key.pem",
	}); err == nil {
		t.Error("a client certificate with no key was accepted")
	}
}

func TestTurningVerificationOff(t *testing.T) {
	// It has to be possible — a development server with a self-signed
	// certificate — and it has to take saying so, because the encryption then
	// proves nothing about who is on the other end.
	config, err := (&Factory{}).buildClientTLS(map[string]interface{}{
		"insecure_skip_verify": true,
	})
	if err != nil {
		t.Fatalf("buildClientTLS: %v", err)
	}
	if !config.InsecureSkipVerify {
		t.Error("verification was asked to be off and is on")
	}

	// Written as a word, which is what env() hands back.
	spelt, err := (&Factory{}).buildClientTLS(map[string]interface{}{
		"insecure_skip_verify": "true",
	})
	if err != nil {
		t.Fatalf("buildClientTLS: %v", err)
	}
	if !spelt.InsecureSkipVerify {
		t.Error("a switch written as a word was ignored")
	}

	// And nothing said leaves it on, which is the safe direction.
	quiet, err := (&Factory{}).buildClientTLS(map[string]interface{}{})
	if err != nil {
		t.Fatalf("buildClientTLS: %v", err)
	}
	if quiet.InsecureSkipVerify {
		t.Error("verification was off by default")
	}
}
