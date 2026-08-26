package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"time"
)

// The TLS half of the mock server.
//
// Nothing in the test stack spoke TLS, so the `tls` block — the one every
// connector that talks to something outside the cluster has — could not be
// exercised anywhere, and the example that would show it had no endpoint to
// point at. This serves the same handlers over HTTPS with a certificate it
// signs itself at startup, and hands that certificate out over plain HTTP at
// /ca.pem so a client can be told to trust it.
//
// Generated per run rather than committed: a private key in a repository is a
// private key in a repository, whatever it is for.

// selfSigned is the certificate this server presents and the CA a client is
// pointed at — the same one, which is what self-signed means.
type selfSigned struct {
	certificate tls.Certificate
	pem         []byte
}

func newSelfSigned() (*selfSigned, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{Organization: []string{"Mycel integration stack"}, CommonName: "mock"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour * 365),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		// Reached as `mock` from inside the compose network and as localhost
		// from the machine running the tests.
		DNSNames:              []string{"mock", "localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	return &selfSigned{certificate: certificate, pem: certPEM}, nil
}

// serveTLS starts the HTTPS listener and registers the route that hands out
// the certificate to trust.
func serveTLS(mux *http.ServeMux, address string) error {
	own, err := newSelfSigned()
	if err != nil {
		return err
	}

	// Over plain HTTP on purpose: a client that does not yet trust this
	// server cannot fetch the thing that would let it.
	mux.HandleFunc("GET /ca.pem", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		_, _ = w.Write(own.pem)
	})

	server := &http.Server{
		Addr:      address,
		Handler:   mux,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{own.certificate}, MinVersion: tls.VersionTLS12},
	}

	go func() {
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()
	return nil
}
