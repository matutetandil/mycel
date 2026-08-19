package examples

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/parser"
)

// Every table an example names has to be created by something the example
// ships.
//
// Thirty of the thirty-six examples using SQLite shipped no way to create their
// tables — no setup file, no migrations — and the database files are ignored by
// git, so the tables have never existed for anybody who cloned this. Each of
// those examples started, printed its routes, and answered 500 to every
// request.
//
// Starting an example and following its README is the stronger check, and it is
// next door; this one needs no service running, so it covers the examples that
// want a broker or a database server too — which is most of them.

// sqlDrivers are the drivers where a table has to exist before it is written
// to. A Mongo collection appears when something is put in it, so a Mongo
// example that ships no schema is not missing anything.
var sqlDrivers = map[string]bool{
	"sqlite":   true,
	"postgres": true,
	"mysql":    true,
	"mariadb":  true,
}

var (
	createTable = regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?[\"'`]?([a-zA-Z_][a-zA-Z0-9_]*)")
	fromTable   = regexp.MustCompile("(?i)\\b(?:FROM|JOIN|INTO|UPDATE)\\s+[\"'`]?([a-zA-Z_][a-zA-Z0-9_]*)")
	// A target naming a field of the message is decided per message, not here.
	expressionTarget = regexp.MustCompile(`^(input|step|output)\.`)
)

func TestEveryTableAnExampleNamesIsCreated(t *testing.T) {
	entries, err := os.ReadDir(repoPath("examples"))
	if err != nil {
		t.Fatalf("reading examples: %v", err)
	}

	checked := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		example := entry.Name()

		configs, err := filepath.Glob(repoPath("examples", example, "*.mycel"))
		if err != nil || len(configs) == 0 {
			continue
		}

		t.Run(example, func(t *testing.T) {
			config, err := parser.NewHCLParser().Parse(context.Background(), repoPath("examples", example))
			if err != nil {
				t.Skipf("does not parse on its own: %v", err)
			}

			sqlConnectors := map[string]bool{}
			// A connector may declare named operations, and a flow points at
			// one by name where a table would otherwise go. The table is
			// inside the operation.
			operations := map[string]map[string]*connector.OperationDef{}
			for _, conn := range config.Connectors {
				if conn.Type != "database" || !sqlDrivers[strings.ToLower(conn.Driver)] {
					continue
				}
				sqlConnectors[conn.Name] = true
				byName := map[string]*connector.OperationDef{}
				for _, op := range conn.Operations {
					byName[op.Name] = op
				}
				operations[conn.Name] = byName
			}
			if len(sqlConnectors) == 0 {
				t.Skip("no SQL database")
			}
			checked++

			named := tablesNamed(config, sqlConnectors, operations)
			if len(named) == 0 {
				t.Skip("names no table")
			}

			created := tablesCreated(t, repoPath("examples", example))
			var missing []string
			for _, table := range named {
				if !created[strings.ToLower(table)] {
					missing = append(missing, table)
				}
			}
			sort.Strings(missing)

			if len(missing) > 0 {
				t.Errorf("names %s, and nothing in the example creates %s — "+
					"the service starts and answers 500 to every request that touches them",
					strings.Join(named, ", "), strings.Join(missing, ", "))
			}
		})
	}

	if checked == 0 {
		t.Fatal("no example was checked; this test is looking at nothing")
	}
}

// tablesNamed collects the tables an example's flows read from and write to.
func tablesNamed(
	config *parser.Configuration,
	sqlConnectors map[string]bool,
	operations map[string]map[string]*connector.OperationDef,
) []string {
	seen := map[string]bool{}

	var addFromSQL func(string)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || expressionTarget.MatchString(name) || strings.ContainsAny(name, " ${}/") {
			return
		}
		seen[name] = true
	}
	addFromSQL = func(query string) {
		for _, match := range fromTable.FindAllStringSubmatch(query, -1) {
			add(match[1])
		}
	}

	// named resolves what a flow wrote where a table goes: either a table, or
	// the name of an operation the connector declares, which holds one.
	named := func(connectorName, value string) {
		// A plain verb is what the flow does, not a name it refers to.
		if connector.IsWriteOperation(value) || strings.EqualFold(value, "SELECT") {
			return
		}
		if op, isOperation := operations[connectorName][value]; isOperation {
			add(op.Table)
			addFromSQL(op.Query)
			return
		}
		add(value)
	}

	for _, f := range config.Flows {
		if f.To != nil && sqlConnectors[f.To.Connector] {
			named(f.To.Connector, f.To.GetTarget())
			named(f.To.Connector, f.To.GetOperation())
			addFromSQL(f.To.GetQuery())
			if f.To.Transaction != nil {
				var walk func([]flow.TxStatement)
				walk = func(statements []flow.TxStatement) {
					for _, statement := range statements {
						if statement.Exec != nil {
							addFromSQL(statement.Exec.Query)
						}
						if statement.Each != nil {
							walk(statement.Each.Body)
						}
					}
				}
				walk(f.To.Transaction.Statements)
			}
		}
		for _, to := range f.MultiTo {
			if to != nil && sqlConnectors[to.Connector] {
				named(to.Connector, to.GetTarget())
				named(to.Connector, to.GetOperation())
				addFromSQL(to.GetQuery())
			}
		}
		for _, step := range f.Steps {
			if sqlConnectors[step.Connector] {
				named(step.Connector, step.GetTarget())
				named(step.Connector, step.GetOperation())
				addFromSQL(step.GetQuery())
			}
		}
		for _, enrich := range f.Enrichments {
			if !sqlConnectors[enrich.Connector] {
				continue
			}
			if target, ok := enrich.ConnectorParams["target"].(string); ok {
				add(target)
			}
			if query, ok := enrich.ConnectorParams["query"].(string); ok {
				addFromSQL(query)
			}
		}
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// tablesCreated collects every table the example's own SQL creates.
func tablesCreated(t *testing.T, dir string) map[string]bool {
	t.Helper()
	created := map[string]bool{}

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".sql") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for _, match := range createTable.FindAllStringSubmatch(string(content), -1) {
			created[strings.ToLower(match[1])] = true
		}
		return nil
	})

	return created
}
