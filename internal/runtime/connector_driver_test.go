package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/parser"
)

// A connector that does not say which implementation runs behind it.
//
// `connector "db" { type = "database" }` parsed, validated, was listed by
// `mycel validate` as a database connector, and failed at start-up with
// `no factory found for connector type=database driver=`. The generator
// produced exactly that file, so the command meant to give somebody a correct
// starting point gave them one that could not run.

func configWith(conns ...*connector.Config) *parser.Configuration {
	return &parser.Configuration{Connectors: conns}
}

func TestAConnectorThatDoesNotSayWhichDatabase(t *testing.T) {
	reg := NewSchemaRegistry()

	errs := ValidateConnectorSchemas(configWith(&connector.Config{Name: "db", Type: "database"}), reg)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want one", errs)
	}
	// The message has to carry the answer: somebody reading it should not have
	// to open the documentation to find the four words that work.
	message := errs[0].Error()
	for _, want := range []string{"db", "driver", "postgres", "mysql", "sqlite", "mongodb"} {
		if !strings.Contains(message, want) {
			t.Errorf("the error does not mention %q: %s", want, message)
		}
	}
}

func TestEveryTypeThatNeedsADriverIsChecked(t *testing.T) {
	reg := NewSchemaRegistry()

	for _, connType := range []string{"database", "mq", "cache", "oauth"} {
		if errs := ValidateConnectorSchemas(configWith(&connector.Config{Name: "x", Type: connType}), reg); len(errs) == 0 {
			t.Errorf("a %s connector with no driver was accepted", connType)
		}
	}

	// And types that are one thing are left alone.
	for _, connType := range []string{"rest", "http", "s3", "exec", "cdc", "webhook"} {
		if errs := ValidateConnectorSchemas(configWith(&connector.Config{Name: "x", Type: connType}), reg); len(errs) != 0 {
			t.Errorf("a %s connector was asked for a driver it does not have: %v", connType, errs)
		}
	}
}

func TestADriverThatIsNotOneOfTheDrivers(t *testing.T) {
	// A misspelling is the common case, and until the schema merge was fixed
	// no driver was checked against its list at all.
	reg := NewSchemaRegistry()

	errs := ValidateConnectorSchemas(configWith(
		&connector.Config{Name: "q", Type: "mq", Driver: "rabitmq",
			Properties: map[string]interface{}{"driver": "rabitmq"}},
	), reg)
	if len(errs) == 0 {
		t.Fatal("a misspelt broker driver was accepted")
	}
	if !strings.Contains(errs[0].Error(), "rabbitmq") {
		t.Errorf("the error does not offer the right spelling: %s", errs[0])
	}
}

func TestAConnectorThatNamesItsDriverIsFine(t *testing.T) {
	reg := NewSchemaRegistry()

	for _, c := range []*connector.Config{
		{Name: "db", Type: "database", Driver: "postgres", Properties: map[string]interface{}{"driver": "postgres"}},
		{Name: "q", Type: "mq", Driver: "rabbitmq", Properties: map[string]interface{}{"driver": "rabbitmq"}},
		{Name: "c", Type: "cache", Driver: "memory", Properties: map[string]interface{}{"driver": "memory"}},
	} {
		if errs := ValidateConnectorSchemas(configWith(c), reg); len(errs) != 0 {
			t.Errorf("%s (%s/%s) was refused: %v", c.Name, c.Type, c.Driver, errs)
		}
	}
}

func TestAPluginsConnectorTypeIsNotSecondGuessed(t *testing.T) {
	// A plugin brings its own type, and nothing here knows what it takes.
	reg := NewSchemaRegistry()

	if errs := ValidateConnectorSchemas(configWith(
		&connector.Config{Name: "sf", Type: "salesforce"},
	), reg); len(errs) != 0 {
		t.Errorf("a plugin's connector was judged against a schema that does not exist: %v", errs)
	}
}
