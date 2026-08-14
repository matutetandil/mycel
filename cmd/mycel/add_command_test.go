package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The commands themselves rather than the generators behind them: where a file
// lands, what happens when the name is taken, and whether what comes out of a
// session of `mycel init` followed by a few `mycel add` calls is something
// `mycel validate` accepts. That last one is the whole promise of the pair.

// project runs each command against a fresh directory, restoring the package
// state the commands read their flags from.
func addProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	previous := configDir
	configDir = dir
	t.Cleanup(func() { configDir = previous })

	// Flags are package variables, so a value left behind by one test reaches
	// the next one.
	addType, addDriver, addFrom, addTo, addOperation, addTarget = "", "", "", "", "", ""
	addListTypes = false
	t.Cleanup(func() {
		addType, addDriver, addFrom, addTo, addOperation, addTarget = "", "", "", "", "", ""
		addListTypes = false
	})

	return dir
}

func TestAddingAConnectorPutsItWhereItBelongs(t *testing.T) {
	dir := addProject(t)

	addType, addDriver = "database", "postgres"
	if err := runAddConnector(addConnectorCmd, []string{"orders_db"}); err != nil {
		t.Fatalf("add connector: %v", err)
	}

	written := filepath.Join(dir, "connectors", "orders_db.mycel")
	body, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("nothing was written to connectors/: %v", err)
	}
	if !strings.Contains(string(body), `connector "orders_db"`) {
		t.Errorf("the file does not declare the connector:\n%s", body)
	}
	if !strings.Contains(string(body), "postgres") {
		t.Errorf("the driver asked for is not in the file:\n%s", body)
	}
}

func TestAddingAConnectorNeedsAKind(t *testing.T) {
	// Without a type there is no schema to generate from, so this cannot fall
	// back to a template — that is the whole reason these are generated.
	addProject(t)

	err := runAddConnector(addConnectorCmd, []string{"orders_db"})
	if err == nil {
		t.Fatal("a connector with no type was generated")
	}
	if !strings.Contains(err.Error(), "--type") {
		t.Errorf("error = %q, want it to name the flag", err)
	}
}

func TestAKindNobodyImplementsIsRefused(t *testing.T) {
	addProject(t)

	addType = "carrier-pigeon"
	err := runAddConnector(addConnectorCmd, []string{"birds"})
	if err == nil {
		t.Fatal("a connector of a type that does not exist was generated")
	}
	if !strings.Contains(err.Error(), "carrier-pigeon") {
		t.Errorf("error = %q, want it to name what was asked for", err)
	}
}

func TestANameAlreadyTakenIsRefused(t *testing.T) {
	// Names are global across every file, so a clash is a parse error somebody
	// would otherwise meet after editing rather than before.
	addProject(t)

	addType, addDriver = "database", "postgres"
	if err := runAddConnector(addConnectorCmd, []string{"orders_db"}); err != nil {
		t.Fatalf("add connector: %v", err)
	}

	err := runAddConnector(addConnectorCmd, []string{"orders_db"})
	if err == nil {
		t.Fatal("the same name was taken twice")
	}
	if !strings.Contains(err.Error(), "orders_db") {
		t.Errorf("error = %q, want it to name the clash", err)
	}
}

func TestAFlowIsWiredToConnectorsThatExist(t *testing.T) {
	// A flow naming a connector nobody declared parses and then fails at
	// startup, which is a worse moment to find out.
	dir := addProject(t)

	addType = "rest"
	if err := runAddConnector(addConnectorCmd, []string{"api"}); err != nil {
		t.Fatalf("add connector: %v", err)
	}
	addType, addDriver = "database", "postgres"
	if err := runAddConnector(addConnectorCmd, []string{"orders_db"}); err != nil {
		t.Fatalf("add connector: %v", err)
	}

	addType, addDriver = "", ""
	addFrom, addTo = "api", "orders_db"
	if err := runAddFlow(addFlowCmd, []string{"list_orders"}); err != nil {
		t.Fatalf("add flow: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, "flows", "list_orders.mycel"))
	if err != nil {
		t.Fatalf("nothing was written to flows/: %v", err)
	}
	for _, want := range []string{`flow "list_orders"`, `"api"`, `"orders_db"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the flow does not carry %s:\n%s", want, body)
		}
	}

	// And one wired to something nobody declared.
	addFrom, addTo = "a_connector_nobody_declared", ""
	if err := runAddFlow(addFlowCmd, []string{"orphan"}); err == nil {
		t.Error("a flow was wired to a connector that does not exist")
	}
}

func TestNothingIsOverwritten(t *testing.T) {
	// Somebody's edited file is not a thing to replace because a name was
	// reused, and the clash check will not catch a file that fails to parse.
	dir := addProject(t)

	path := filepath.Join(dir, "connectors", "orders_db.mycel")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("# work in progress, does not parse yet\nconnector {"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	addType, addDriver = "database", "postgres"
	if err := runAddConnector(addConnectorCmd, []string{"orders_db"}); err == nil {
		t.Fatal("an existing file was replaced")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "work in progress") {
		t.Error("the file that was there is gone")
	}
}

func TestTheTypesOnOfferAreListed(t *testing.T) {
	// The command somebody runs when they do not know what to pass to --type,
	// which is the first thing anyone does.
	addProject(t)

	addListTypes = true
	if err := runAddConnector(addConnectorCmd, nil); err != nil {
		t.Fatalf("--list: %v", err)
	}
}

func TestAWholeProjectBuiltFromTheCommandsValidates(t *testing.T) {
	// init, then a connector at each end, then a flow between them: the
	// sequence the documentation walks somebody through. If what it produces
	// does not validate, the first thing anyone does with Mycel fails.
	dir := addProject(t)

	if err := runInit(initCmd, []string{dir}); err != nil {
		t.Fatalf("init: %v", err)
	}

	addType = "rest"
	if err := runAddConnector(addConnectorCmd, []string{"orders_api"}); err != nil {
		t.Fatalf("add connector: %v", err)
	}

	addType, addDriver = "database", "sqlite"
	if err := runAddConnector(addConnectorCmd, []string{"orders_store"}); err != nil {
		t.Fatalf("add connector: %v", err)
	}

	addType, addDriver = "", ""
	addFrom, addTo = "orders_api", "orders_store"
	addOperation, addTarget = "GET /orders", "orders"
	if err := runAddFlow(addFlowCmd, []string{"list_orders"}); err != nil {
		t.Fatalf("add flow: %v", err)
	}

	if _, err := parseConfigDirQuietly(); err != nil {
		t.Fatalf("a project built entirely from these commands does not parse: %v", err)
	}
}
