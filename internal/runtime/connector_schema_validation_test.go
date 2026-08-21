package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/parser"
	"github.com/matutetandil/mycel/v3/pkg/connectors"
	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// Many connector settings accept one of a fixed set of words — an auth type, a
// compression codec, a file format. The schema has always declared which words
// those are, and nothing ever checked. So `auth { type = "beare" }` validated
// clean and the HTTP client, finding no type it recognised, sent every request
// with no credentials at all: a service that answers 401 to everything, with
// the configuration file insisting authentication is set up.
//
// These tests say what a mistyped value should do instead.

func schemaConfig(conns ...*connector.Config) *parser.Configuration {
	return &parser.Configuration{Connectors: conns}
}

func TestAValueTheConnectorDoesNotKnowIsRefused(t *testing.T) {
	errs := ValidateConnectorSchemas(schemaConfig(&connector.Config{
		Name: "api", Type: "http",
		Properties: map[string]interface{}{
			"base_url": "https://example.com",
			"auth":     map[string]interface{}{"type": "beare", "token": "x"},
		},
	}), connectors.FullRegistry())

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want the one about the auth type: %v", len(errs), errs)
	}
	message := errs[0].Error()

	// Everything needed to fix it without opening the source: which connector,
	// which setting, what was written, and what may be written instead.
	for _, want := range []string{"api", "auth.type", "beare", "bearer", "basic"} {
		if !strings.Contains(message, want) {
			t.Errorf("error = %q, want it to mention %q", message, want)
		}
	}
}

func TestAValueTheConnectorKnowsIsLeftAlone(t *testing.T) {
	errs := ValidateConnectorSchemas(schemaConfig(&connector.Config{
		Name: "api", Type: "http",
		Properties: map[string]interface{}{
			"base_url": "https://example.com",
			"format":   "xml",
			"auth":     map[string]interface{}{"type": "basic", "username": "u", "password": "p"},
		},
	}), connectors.FullRegistry())

	if len(errs) != 0 {
		t.Errorf("a configuration the connector accepts was refused: %v", errs)
	}
}

func TestEveryWordTheCodeAcceptsIsAWordTheSchemaOffers(t *testing.T) {
	// The lists are only worth enforcing if they are complete. The HTTP client
	// implements client_credentials — the OAuth2 grant a service uses to
	// authenticate as itself — and the schema did not list it, so enforcement
	// would have rejected a configuration that works.
	errs := ValidateConnectorSchemas(schemaConfig(&connector.Config{
		Name: "api", Type: "http",
		Properties: map[string]interface{}{
			"base_url": "https://example.com",
			"auth": map[string]interface{}{
				"type":          "client_credentials",
				"token_url":     "https://example.com/token",
				"client_id":     "id",
				"client_secret": "secret",
			},
		},
	}), connectors.FullRegistry())

	if len(errs) != 0 {
		t.Errorf("a grant type the connector implements was refused: %v", errs)
	}
}

func TestASettingWithNoFixedListIsNotSecondGuessed(t *testing.T) {
	// Most settings are free text — a URL, a queue name, a header. Only the
	// ones that name a fixed set are checked.
	errs := ValidateConnectorSchemas(schemaConfig(&connector.Config{
		Name: "api", Type: "http",
		Properties: map[string]interface{}{
			"base_url": "https://whatever.example.com/some/path",
			"headers":  map[string]interface{}{"X-Anything": "at all"},
		},
	}), connectors.FullRegistry())

	if len(errs) != 0 {
		t.Errorf("free-form settings were checked against a list: %v", errs)
	}
}

func TestAConnectorNobodyDescribedIsSkipped(t *testing.T) {
	// A plugin brings its own connector type and no schema. Refusing what
	// cannot be checked would make plugins unusable.
	errs := ValidateConnectorSchemas(schemaConfig(&connector.Config{
		Name: "custom", Type: "acme-widget",
		Properties: map[string]interface{}{"anything": "goes"},
	}), connectors.FullRegistry())

	if len(errs) != 0 {
		t.Errorf("a connector with no schema was checked anyway: %v", errs)
	}
}

func TestTheCheckReachesIntoNestedBlocks(t *testing.T) {
	// A queue's settings live two levels down, which is exactly where a typo
	// is least likely to be spotted by eye.
	errs := ValidateConnectorSchemas(schemaConfig(&connector.Config{
		Name: "events", Type: "mq", Driver: "kafka",
		Properties: map[string]interface{}{
			"brokers": []interface{}{"localhost:9092"},
			"producer": map[string]interface{}{
				"compression": "gzpi",
			},
		},
	}), connectors.FullRegistry())

	if len(errs) != 1 {
		t.Fatalf("got %d errors, want the one about compression: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "producer.compression") {
		t.Errorf("error = %q, want the path to the setting", errs[0])
	}
}

func TestEveryOffendingSettingIsReportedNotJustTheFirst(t *testing.T) {
	// Fixing one, running again, finding the next is a slow way to learn there
	// were three.
	errs := ValidateConnectorSchemas(schemaConfig(
		&connector.Config{
			Name: "api", Type: "http",
			Properties: map[string]interface{}{
				"base_url": "https://example.com",
				"format":   "yaml",
				"auth":     map[string]interface{}{"type": "beare"},
			},
		},
		&connector.Config{
			Name: "files", Type: "file",
			Properties: map[string]interface{}{"path": "/tmp", "format": "parquet"},
		},
	), connectors.FullRegistry())

	if len(errs) != 3 {
		t.Fatalf("got %d errors, want all three: %v", len(errs), errs)
	}
	// Sorted, so a configuration that fails fails the same way twice.
	if !strings.Contains(errs[0].Error(), "api") || !strings.Contains(errs[2].Error(), "files") {
		t.Errorf("errors came back in an unstable order: %v", errs)
	}
}

func TestAValueThatIsNotWrittenAsAWordIsLeftToTheConnector(t *testing.T) {
	// A number or a boolean where a word belongs is a different mistake, and
	// the connector reports it with what it was trying to do at the time.
	errs := ValidateConnectorSchemas(schemaConfig(&connector.Config{
		Name: "api", Type: "http",
		Properties: map[string]interface{}{
			"base_url": "https://example.com",
			"format":   42,
		},
	}), connectors.FullRegistry())

	if len(errs) != 0 {
		t.Errorf("a wrongly typed value was reported as an unknown word: %v", errs)
	}
}

func TestNothingToCheckIsNotAnError(t *testing.T) {
	if errs := ValidateConnectorSchemas(nil, connectors.FullRegistry()); errs != nil {
		t.Errorf("errs = %v", errs)
	}
	if errs := ValidateConnectorSchemas(schemaConfig(), nil); errs != nil {
		t.Errorf("errs = %v", errs)
	}
}

func TestTheSchemaListsAreWorthEnforcing(t *testing.T) {
	// Enforcing a list is only safe if the list is right, and a list nobody
	// checks drifts from the code it describes. This is the cheap half of that
	// guarantee: every declared value is a single word, so a list holding a
	// description or an example fails here rather than rejecting somebody's
	// working configuration.
	registry := connectors.FullRegistry()
	for _, connType := range registry.AllConnectorTypes() {
		block := registry.ConnectorSchema(connType, "")
		walkSchemaBlocks(&block, connType, func(path string, attr schema.Attr) {
			for _, value := range attr.Values {
				if value == "" {
					t.Errorf("%s declares an empty allowed value", path)
				}
				if strings.ContainsAny(value, " \t,") {
					t.Errorf("%s declares %q, which is not a word a configuration file would carry", path, value)
				}
			}
		})
	}
}
