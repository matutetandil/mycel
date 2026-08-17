package kafka

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Confluent's Schema Registry, which is how most managed Kafka carries Avro or
// Protobuf: a message on the wire is a magic byte, a schema id, and the
// payload, and a consumer that gets the id wrong decodes garbage. None of it
// was covered.

// registry stands in for the Schema Registry, recording what it was asked.
func registry(t *testing.T, handler http.HandlerFunc) (*SchemaRegistryClient, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	return NewSchemaRegistryClient(&SchemaRegistryConfig{URL: server.URL}), &calls
}

func TestASchemaIsRegisteredOnceAndRemembered(t *testing.T) {
	// Registering per message would put a call to the registry in front of
	// every publish.
	client, calls := registry(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 7})
	})

	id, err := client.RegisterSchema("orders-value", `{"type":"record","name":"Order"}`)
	if err != nil {
		t.Fatalf("RegisterSchema: %v", err)
	}
	if id != 7 {
		t.Errorf("id = %d, want the one the registry issued", id)
	}

	if _, err := client.RegisterSchema("orders-value", `{"type":"record","name":"Order"}`); err != nil {
		t.Fatalf("the second registration failed: %v", err)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("%d calls to the registry, want one: the id is not being remembered", got)
	}
}

func TestASchemaIsFetchedByIDAndRemembered(t *testing.T) {
	// A consumer reads the id off every message; asking the registry each time
	// would be a network round trip per message.
	client, calls := registry(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/schemas/ids/7") {
			t.Errorf("asked for %s, want the schema by its id", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"schema": `{"type":"record"}`})
	})

	for i := 0; i < 3; i++ {
		schema, err := client.GetSchemaByID(7)
		if err != nil {
			t.Fatalf("GetSchemaByID: %v", err)
		}
		if schema != `{"type":"record"}` {
			t.Errorf("schema = %q", schema)
		}
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("%d calls, want one: the schema is being fetched per message", got)
	}
}

func TestAnIDNobodyPublishedIsReported(t *testing.T) {
	client, _ := registry(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error_code":40403,"message":"Schema not found"}`))
	})

	if _, err := client.GetSchemaByID(999); err == nil {
		t.Error("a schema id the registry does not know was accepted")
	}
}

func TestTheLatestSchemaForASubjectIsRead(t *testing.T) {
	client, _ := registry(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/versions/latest") {
			t.Errorf("asked for %s, want the latest version", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 11, "schema": `{"type":"record","name":"Order"}`,
		})
	})

	id, schema, err := client.GetLatestSchema("orders-value")
	if err != nil {
		t.Fatalf("GetLatestSchema: %v", err)
	}
	if id != 11 || !strings.Contains(schema, "Order") {
		t.Errorf("id = %d, schema = %q", id, schema)
	}
}

func TestASchemaIsCheckedAgainstTheOneInUse(t *testing.T) {
	// The check that stops a producer rolling out a schema its consumers
	// cannot read.
	for name, tc := range map[string]struct {
		body       string
		compatible bool
	}{
		"compatible":     {`{"is_compatible":true}`, true},
		"not compatible": {`{"is_compatible":false}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := registry(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			})

			got, err := client.CheckCompatibility("orders-value", `{"type":"record"}`)
			if err != nil {
				t.Fatalf("CheckCompatibility: %v", err)
			}
			if got != tc.compatible {
				t.Errorf("compatible = %v, want %v", got, tc.compatible)
			}
		})
	}
}

func TestARegistryThatCannotBeReachedIsReported(t *testing.T) {
	client := NewSchemaRegistryClient(&SchemaRegistryConfig{URL: "http://127.0.0.1:1"})

	if _, err := client.RegisterSchema("orders-value", `{}`); err == nil {
		t.Error("a schema was registered against a registry that is not there")
	}
	if _, err := client.GetSchemaByID(1); err == nil {
		t.Error("a schema was fetched from a registry that is not there")
	}
}

// --- The wire format --------------------------------------------------------

func TestAMessageCarriesTheSchemaItWasWrittenWith(t *testing.T) {
	// Five bytes in front of the payload: a magic byte and the schema id. A
	// consumer reads them to know how to decode what follows, so a round trip
	// has to come back byte for byte.
	payload := []byte("the encoded record")

	encoded := EncodeWithSchemaID(7, payload)
	if encoded[0] != 0 {
		t.Errorf("magic byte = %d, want 0", encoded[0])
	}

	id, decoded, err := DecodeSchemaID(encoded)
	if err != nil {
		t.Fatalf("DecodeSchemaID: %v", err)
	}
	if id != 7 {
		t.Errorf("id = %d", id)
	}
	if string(decoded) != string(payload) {
		t.Errorf("payload = %q, want it back unchanged", decoded)
	}
}

func TestSomethingThatIsNotInTheWireFormatIsRefused(t *testing.T) {
	// A topic carrying plain JSON alongside Avro is the ordinary mistake, and
	// reading four arbitrary bytes as a schema id would decode nonsense.
	for name, data := range map[string][]byte{
		"too short":      []byte{0, 1, 2},
		"no magic byte":  append([]byte{9}, []byte("0000payload")...),
		"nothing at all": {},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := DecodeSchemaID(data); err == nil {
				t.Error("it was read as a schema registry message")
			}
		})
	}
}

func TestTheSubjectASchemaIsRegisteredUnder(t *testing.T) {
	// The strategy decides which schemas evolve together: per topic, per
	// record type, or both. Registering under the wrong subject silently
	// applies another schema's compatibility rules.
	for name, tc := range map[string]struct {
		strategy string
		isKey    bool
		want     string
	}{
		"per topic":            {"topic", false, "orders-value"},
		"per topic, the key":   {"topic", true, "orders-key"},
		"per record":           {"record", false, "com.acme.Order-value"},
		"per topic and record": {"topic_record", false, "orders-com.acme.Order-value"},
		"nothing said":         {"", false, "orders-value"},
		"something unknown":    {"by-the-moon", false, "orders-value"},
	} {
		t.Run(name, func(t *testing.T) {
			got := GetSubjectName("orders", "com.acme.Order", tc.strategy, tc.isKey)
			if got != tc.want {
				t.Errorf("subject = %q, want %q", got, tc.want)
			}
		})
	}
}
