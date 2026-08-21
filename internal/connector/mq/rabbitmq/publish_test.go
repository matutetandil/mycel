package rabbitmq

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/matutetandil/mycel/v3/internal/connector/mq/types"
)

// What a published message looks like on the wire. The setting that matters
// most is persistence: a message the broker holds only in memory is one a
// restart loses, and nothing about a queue suggests that is what happened.

func publisher(t *testing.T, config *PublisherConfig) *Connector {
	t.Helper()
	return &Connector{
		name:   "orders_rabbit",
		config: &Config{URL: "amqp://localhost:5672/", Publisher: config},
	}
}

func TestAMessageSurvivesARestartWhenItIsMeantTo(t *testing.T) {
	persistent := publisher(t, &PublisherConfig{Persistent: true})
	published, err := persistent.buildPublishing(&types.Message{
		ID: "order-1", Body: map[string]interface{}{"sku": "WIDGET-1"},
	})
	if err != nil {
		t.Fatalf("buildPublishing: %v", err)
	}
	if published.DeliveryMode != uint8(types.DeliveryModePersistent) {
		t.Error("the message is held in memory, so a broker restart loses it")
	}

	// And a publisher that asked for speed instead gets it.
	transient := publisher(t, &PublisherConfig{Persistent: false})
	published, err = transient.buildPublishing(&types.Message{Body: map[string]interface{}{}})
	if err != nil {
		t.Fatalf("buildPublishing: %v", err)
	}
	if published.DeliveryMode != uint8(types.DeliveryModeTransient) {
		t.Error("a publisher that asked for transient got persistent")
	}
}

func TestAPublishedMessageCarriesWhatItWasGiven(t *testing.T) {
	c := publisher(t, &PublisherConfig{Persistent: true, ContentType: "application/json"})

	published, err := c.buildPublishing(&types.Message{
		ID:        "order-1",
		Body:      map[string]interface{}{"sku": "WIDGET-1"},
		Headers:   map[string]string{"traceparent": "00-abc-def-01"},
		Timestamp: 1700000000,
	})
	if err != nil {
		t.Fatalf("buildPublishing: %v", err)
	}

	if published.MessageId != "order-1" {
		t.Errorf("message id = %q — a consumer dedupes on this", published.MessageId)
	}
	if string(published.Body) != `{"sku":"WIDGET-1"}` {
		t.Errorf("body = %s", published.Body)
	}
	// Without this a trace stops at the queue and the consumer's work looks
	// like it came from nowhere.
	if published.Headers["traceparent"] != "00-abc-def-01" {
		t.Errorf("headers = %v", published.Headers)
	}
	if published.Timestamp.Unix() != 1700000000 {
		t.Errorf("timestamp = %v, want the one the message carried", published.Timestamp)
	}
	if published.ContentType != "application/json" {
		t.Errorf("content type = %q", published.ContentType)
	}
}

func TestAMessageWithNoTimeOfItsOwnIsStampedNow(t *testing.T) {
	// A consumer ordering by timestamp needs one on every message.
	c := publisher(t, &PublisherConfig{})

	published, err := c.buildPublishing(&types.Message{Body: map[string]interface{}{}})
	if err != nil {
		t.Fatalf("buildPublishing: %v", err)
	}
	if published.Timestamp.IsZero() {
		t.Error("the message carries no timestamp at all")
	}
	if time.Since(published.Timestamp) > time.Minute {
		t.Errorf("timestamp = %v, want about now", published.Timestamp)
	}
}

func TestAContentTypeIsAlwaysDeclared(t *testing.T) {
	// A consumer written by somebody else switches on it, and an empty one
	// makes them guess.
	c := publisher(t, nil)

	published, err := c.buildPublishing(&types.Message{Body: map[string]interface{}{}})
	if err != nil {
		t.Fatalf("buildPublishing: %v", err)
	}
	if published.ContentType != "application/json" {
		t.Errorf("content type = %q, want a default", published.ContentType)
	}

	// A message naming its own wins, which is how a flow publishes something
	// that is not JSON.
	published, err = c.buildPublishing(&types.Message{
		Body: map[string]interface{}{}, ContentType: "application/xml",
	})
	if err != nil {
		t.Fatalf("buildPublishing: %v", err)
	}
	if published.ContentType != "application/xml" {
		t.Errorf("content type = %q, want the one the message named", published.ContentType)
	}
}

func TestABodyThatCannotBeSerialisedIsReported(t *testing.T) {
	// Rather than published as something the consumer cannot read.
	c := publisher(t, &PublisherConfig{})

	if _, err := c.buildPublishing(&types.Message{
		Body: map[string]interface{}{"fn": func() {}},
	}); err == nil {
		t.Error("a message that cannot be serialised was published")
	}
}

// --- Reaching a managed broker ----------------------------------------------

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

func TestABrokerBehindItsOwnAuthorityIsTrusted(t *testing.T) {
	// CloudAMQP and a self-hosted broker both present a certificate signed by
	// somebody the machine does not know by default.
	dir := t.TempDir()
	caFile := writeCertificate(t, dir)

	built, err := (&TLSConfig{Enabled: true, CAFile: caFile}).BuildTLSConfig()
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if built.RootCAs == nil {
		t.Error("the authority was not loaded, so the broker's certificate cannot be checked")
	}
}

func TestTLSMaterialThatCannotBeLoadedIsReported(t *testing.T) {
	// At startup, where the path can be fixed — not as a connection that
	// fails for reasons the log does not explain.
	dir := t.TempDir()
	notPEM := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notPEM, []byte("this is not a certificate"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	for name, config := range map[string]*TLSConfig{
		"an authority that is not there": {Enabled: true, CAFile: filepath.Join(dir, "absent.pem")},
		"a client certificate that is not there": {
			Enabled: true, CertFile: filepath.Join(dir, "absent.pem"), KeyFile: notPEM,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := config.BuildTLSConfig(); err == nil {
				t.Error("it was accepted")
			}
		})
	}
}

// writeCertificate writes a self-signed certificate, the way a broker's CA
// bundle arrives on disk.
func writeCertificate(t *testing.T, dir string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "rabbit.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating a certificate: %v", err)
	}

	path := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	return path
}

var _ = amqp.Publishing{}
