package grpc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
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

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// Mutual TLS is how a gRPC service between two internal systems usually
// authenticates: no tokens anywhere, the certificate is the identity. Which
// means the authority those certificates are checked against decides who can
// call the service at all — and that authority was never read.

// certificate writes a certificate and its key the way they arrive on disk.
func certificate(t *testing.T, dir, name string, notBefore, notAfter time.Time) (certFile, keyFile string, parsed *x509.Certificate) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject: pkix.Name{
			CommonName:   name,
			Organization: []string{"Waterworks"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,
		IsCA:      true,
		KeyUsage:  x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating a certificate: %v", err)
	}
	parsed, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing it back: %v", err)
	}

	certFile = filepath.Join(dir, name+".pem")
	keyFile = filepath.Join(dir, name+".key")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("writing the certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}), 0o600); err != nil {
		t.Fatalf("writing the key: %v", err)
	}
	return certFile, keyFile, parsed
}

func TestTheAuthorityClientCertificatesAreCheckedAgainstIsTheConfiguredOne(t *testing.T) {
	// This returned the system pool and ignored the file, which is worse than
	// leaving it unimplemented: a server configured for mTLS against a private
	// authority started without complaint and then refused every client it was
	// set up to accept, because the certificates were being checked against the
	// public roots.
	dir := t.TempDir()
	caFile, _, ca := certificate(t, dir, "waterworks-ca", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))

	pool, err := loadCACert(caFile)
	if err != nil {
		t.Fatalf("loadCACert: %v", err)
	}

	// A pool that holds the configured authority, and not merely whatever the
	// machine happens to trust.
	if len(pool.Subjects()) == 0 { //nolint:staticcheck // the point is what was loaded
		t.Fatal("the pool is empty: the authority file was not read")
	}
	if _, err := ca.Verify(x509.VerifyOptions{Roots: pool}); err != nil {
		t.Errorf("a certificate from the configured authority does not verify against it: %v", err)
	}
}

func TestAnAuthorityThatCannotBeLoadedIsRefused(t *testing.T) {
	// At startup, where the path can be fixed — not as clients being turned
	// away for reasons nothing explains.
	dir := t.TempDir()

	notPEM := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notPEM, []byte("this is not a certificate"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	for name, path := range map[string]string{
		"a file that is not there": filepath.Join(dir, "absent.pem"),
		"a file that is not one":   notPEM,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadCACert(path); err == nil {
				t.Error("it was accepted")
			}
		})
	}
}

func TestAServerAskingForClientCertificatesRequiresThem(t *testing.T) {
	dir := t.TempDir()
	caFile, _, _ := certificate(t, dir, "ca", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	certFile, keyFile, _ := certificate(t, dir, "server", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))

	built, err := BuildMTLSConfig(&TLSConfig{
		Enabled: true, CertFile: certFile, KeyFile: keyFile, CAFile: caFile,
	})
	if err != nil {
		t.Fatalf("BuildMTLSConfig: %v", err)
	}
	if len(built.Certificates) != 1 {
		t.Error("the server presents no certificate of its own")
	}
	if built.ClientCAs == nil {
		t.Error("no authority to check clients against")
	}
	// Asking for a certificate and not checking it authenticates nobody.
	if built.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %v, want the client certificate to be required and verified", built.ClientAuth)
	}
}

func TestMTLSIsOffUnlessItIsAskedFor(t *testing.T) {
	for name, config := range map[string]*TLSConfig{
		"absent":     nil,
		"turned off": {Enabled: false, CAFile: "/does/not/exist"},
	} {
		t.Run(name, func(t *testing.T) {
			built, err := BuildMTLSConfig(config)
			if err != nil || built != nil {
				t.Errorf("built = %v, err = %v, want nothing at all", built, err)
			}
		})
	}
}

func TestMTLSConfigurationThatCannotBeLoadedIsReported(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, _ := certificate(t, dir, "server", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))

	for name, config := range map[string]*TLSConfig{
		"a server certificate that is not there": {
			Enabled: true, CertFile: filepath.Join(dir, "absent.pem"), KeyFile: keyFile,
		},
		"an authority that is not there": {
			Enabled: true, CertFile: certFile, KeyFile: keyFile, CAFile: filepath.Join(dir, "absent.pem"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildMTLSConfig(config); err == nil {
				t.Error("it was accepted, so the server starts and turns everybody away")
			}
		})
	}
}

// --- What the server makes of a client's certificate -------------------------

// withPeerCertificate builds the context a gRPC handler sees for a connection
// carrying the given certificates.
func withPeerCertificate(certs ...*x509.Certificate) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{
			State: tls.ConnectionState{PeerCertificates: certs},
		},
	})
}

func TestAClientCertificateBecomesTheCallerIdentity(t *testing.T) {
	dir := t.TempDir()
	_, _, client := certificate(t, dir, "orders-service", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))

	interceptor := NewAuthInterceptor(&AuthConfig{Type: "mtls"})
	authCtx, err := interceptor.authenticate(withPeerCertificate(client))
	if err != nil {
		t.Fatalf("a valid client certificate was refused: %v", err)
	}

	if !authCtx.Authenticated || authCtx.Method != "mtls" {
		t.Errorf("authenticated = %v, method = %q", authCtx.Authenticated, authCtx.Method)
	}
	// The common name is the identity a flow can act on; without it mTLS
	// authenticates but says nothing about who.
	if authCtx.UserID != "orders-service" {
		t.Errorf("UserID = %q, want the certificate's common name", authCtx.UserID)
	}
	if authCtx.Claims["cn"] != "orders-service" {
		t.Errorf("claims = %v", authCtx.Claims)
	}
	if authCtx.ClientCert == nil {
		t.Error("the certificate itself was not carried through")
	}
}

func TestACertificateOutsideItsValidityIsRefused(t *testing.T) {
	dir := t.TempDir()
	_, _, expired := certificate(t, dir, "expired", time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour))
	_, _, early := certificate(t, dir, "early", time.Now().Add(time.Hour), time.Now().Add(48*time.Hour))

	interceptor := NewAuthInterceptor(&AuthConfig{Type: "mtls"})

	for name, cert := range map[string]*x509.Certificate{
		"expired":       expired,
		"not yet valid": early,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := interceptor.authenticate(withPeerCertificate(cert)); err == nil {
				t.Error("it was accepted")
			}
		})
	}
}

func TestAConnectionWithNoCertificateIsRefused(t *testing.T) {
	interceptor := NewAuthInterceptor(&AuthConfig{Type: "mtls"})

	// Nothing about the connection at all.
	if _, err := interceptor.authenticate(context.Background()); err == nil {
		t.Error("a call with no peer information was accepted")
	}

	// A connection, but not a TLS one.
	plain := peer.NewContext(context.Background(), &peer.Peer{})
	if _, err := interceptor.authenticate(plain); err == nil {
		t.Error("a call over a plain connection was accepted")
	}

	// TLS, but the client presented nothing.
	if _, err := interceptor.authenticate(withPeerCertificate()); err == nil {
		t.Error("a call presenting no certificate was accepted")
	}
}

func TestAnAuthenticationMethodNobodyImplementsIsRefused(t *testing.T) {
	// By name, so the configuration mistake is visible. Falling through to
	// "allowed" would be the dangerous direction.
	interceptor := NewAuthInterceptor(&AuthConfig{Type: "kerberos"})
	_, err := interceptor.authenticate(context.Background())
	if err == nil {
		t.Fatal("a method this connector cannot speak was accepted")
	}
	if !strings.Contains(err.Error(), "kerberos") {
		t.Errorf("error = %q, want it to name what was asked for", err)
	}
}

func TestNoAuthenticationConfiguredLetsCallsThrough(t *testing.T) {
	// The ordinary internal case, and the default.
	for name, config := range map[string]*AuthConfig{
		"absent": nil,
		"empty":  {},
		"none":   {Type: "none"},
	} {
		t.Run(name, func(t *testing.T) {
			authCtx, err := NewAuthInterceptor(config).authenticate(context.Background())
			if err != nil {
				t.Fatalf("a call was refused by a service with no authentication: %v", err)
			}
			if !authCtx.Authenticated {
				t.Error("the call was not marked as allowed")
			}
		})
	}
}
