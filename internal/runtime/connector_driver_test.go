package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/parser"
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

	// Complete in every other way, so the only thing missing is the driver.
	errs := ValidateConnectorSchemas(configWith(&connector.Config{
		Name: "db", Type: "database",
		Properties: map[string]interface{}{"url": "postgres://mycel@localhost:5432/orders"},
	}), reg)
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

	// cdc is here rather than below: a change-data-capture connector with no
	// driver cannot run — the factory answers "unsupported CDC driver:
	// (supported: postgres)" at connect time — so the schema asks for it at
	// validate time instead, where the answer is cheaper to act on.
	for _, connType := range []string{"database", "mq", "cache", "oauth", "cdc"} {
		if errs := ValidateConnectorSchemas(configWith(&connector.Config{Name: "x", Type: connType}), reg); len(errs) == 0 {
			t.Errorf("a %s connector with no driver was accepted", connType)
		}
	}

	// And types that are one thing are left alone.
	for _, connType := range []string{"rest", "http", "s3", "exec", "webhook"} {
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
		{Name: "db", Type: "database", Driver: "postgres", Properties: map[string]interface{}{
			"driver": "postgres", "url": "postgres://mycel@localhost:5432/orders"}},
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

// A setting that can be written in more than one way.
//
// A Postgres connector needs a database name and a user. Both can be written
// as attributes, and both can be written inside a `url` — every managed
// platform hands over one connection string, and it is taken apart before
// anything is checked, so the two forms end up in the same place. The schema
// said `database` was required, which described a rule that does not exist:
// a url-only connector is complete and ran fine. Meanwhile a connector with
// neither validated clean and failed at start-up.

func TestEitherWayOfNamingTheDatabase(t *testing.T) {
	reg := NewSchemaRegistry()

	for name, props := range map[string]map[string]interface{}{
		"the whole connection string": {
			"url": "postgres://mycel:secret@localhost:5432/orders",
		},
		"the discrete attributes": {
			"host": "localhost", "database": "orders", "user": "mycel",
		},
		"a url with the user written over it": {
			"url": "postgres://localhost:5432/orders", "user": "someone_else",
		},
	} {
		t.Run(name, func(t *testing.T) {
			errs := ValidateConnectorSchemas(configWith(&connector.Config{
				Name: "db", Type: "database", Driver: "postgres", Properties: props,
			}), reg)
			if len(errs) != 0 {
				t.Errorf("a complete connector was refused: %v", errs)
			}
		})
	}
}

func TestAConnectorThatNamesNoDatabaseAtAll(t *testing.T) {
	// Neither form. Until now this validated clean and failed at start-up
	// with "postgres connector requires database name".
	reg := NewSchemaRegistry()

	errs := ValidateConnectorSchemas(configWith(&connector.Config{
		Name: "db", Type: "database", Driver: "postgres",
		Properties: map[string]interface{}{"host": "localhost"},
	}), reg)

	if len(errs) != 2 {
		t.Fatalf("errors = %v, want one for the database and one for the user", errs)
	}
	// The message has to name both ways of answering, or somebody who meant
	// to use a url is told to add a database attribute.
	joined := errs[0].Error() + errs[1].Error()
	for _, want := range []string{"url", "database", "user"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the errors do not mention %q: %v", want, errs)
		}
	}
}

func TestAnUnsetVariableIsNotAMissingAttribute(t *testing.T) {
	// `url = env("DATABASE_URL")` with the variable unset reads as empty. The
	// author answered the question; the environment did not, and that case
	// already has a better answer than anything this could say — the start-up
	// error names the variable and the connector that wanted it. Refusing it
	// here would replace that with "needs url or database", about a connector
	// that has a url.
	reg := NewSchemaRegistry()

	errs := ValidateConnectorSchemas(configWith(&connector.Config{
		Name: "db", Type: "database", Driver: "postgres",
		Properties: map[string]interface{}{"url": ""},
	}), reg)

	if len(errs) != 0 {
		t.Errorf("a connector whose url comes from an unset variable was refused: %v", errs)
	}
}

func TestMongoStillNeedsItsDatabaseNamed(t *testing.T) {
	// Mongo is the one where the rule really is "this attribute is required":
	// the uri does not carry the database name, and the connector reads it
	// separately.
	reg := NewSchemaRegistry()

	if errs := ValidateConnectorSchemas(configWith(&connector.Config{
		Name: "docs", Type: "database", Driver: "mongodb",
		Properties: map[string]interface{}{"uri": "mongodb://localhost:27017"},
	}), reg); len(errs) != 0 {
		// Nothing enforces plain Required on a connector block, so this
		// passes — but the schema must not claim the uri supplies the name.
		t.Logf("errors = %v", errs)
	}

	block := reg.ConnectorSchema("database", "mongodb")
	if len(block.RequiredOneOf) != 0 {
		t.Errorf("mongo declares alternatives it does not have: %v", block.RequiredOneOf)
	}
	var found bool
	for _, attr := range block.Attrs {
		if attr.Name == "database" {
			found = attr.Required
		}
	}
	if !found {
		t.Error("mongo's database name is not marked required, and it is")
	}
}

func TestSQLiteAsksForNothing(t *testing.T) {
	// It writes to ./data/mycel.db unless told otherwise, so a required path
	// was a required attribute with a default behind it — two statements that
	// contradict each other, and `mycel add` believed the first.
	reg := NewSchemaRegistry()

	if errs := ValidateConnectorSchemas(configWith(&connector.Config{
		Name: "db", Type: "database", Driver: "sqlite",
	}), reg); len(errs) != 0 {
		t.Errorf("a sqlite connector with no path was refused: %v", errs)
	}

	for _, attr := range reg.ConnectorSchema("database", "sqlite").Attrs {
		if attr.Name == "database" {
			if attr.Required {
				t.Error("the file path is marked required and it has a default")
			}
			if attr.Default == nil {
				t.Error("the file path has a default and does not say what it is")
			}
		}
	}
}
