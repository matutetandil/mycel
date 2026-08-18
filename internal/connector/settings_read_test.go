package connector_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// A setting nobody reads.
//
// This is the shape that has produced more bugs in this repository than any
// other: an attribute is accepted by the parser, carried into a config struct
// by the factory, and read by nothing. The configuration says the thing is on
// and the runtime never asks. It has been found by hand five times —
// sender_id and sms_type on SNS, ca_cert on MQTT, known_hosts and password on
// ssh, the API key validate block — and every one of them was silent: no
// error, no warning, the service running and the setting doing nothing.
//
// So it is checked mechanically. For every connector package, every field of
// every Config struct that is written somewhere and read nowhere is a
// candidate, and a candidate has to be either wired up or written down here
// with a reason.

// notRead lists fields deliberately assigned and never read, and why.
//
// An entry that starts being read fails the test, so the list cannot go stale,
// and adding one takes a reason somebody has to write.
var notRead = map[string]string{
	"email/Config.Driver": "the factory switches on the driver it was given and builds one of two " +
		"connectors from it; the field is kept so a connector can say what it is",
	"push/Config.Driver": "same as email: the factory switches on it and the field is kept for reporting",
	"sms/Config.Driver":  "same as email",

	"graphql/EntityConfig.TypeName":     "federation entity metadata used by the schema builder through its own map",
	"graphql/SchemaConfig.AutoGenerate": "decided before the config is built, from whether a schema file was given",

	"http/AuthConfig.GrantType": "only client_credentials is implemented, and the value is checked " +
		"by the parser rather than here",

	"mq/DLQConfig.RetryDelay":                     "delay between retries is the broker's, set by the message TTL on the retry queue",
	"mq/Config.ClientID":                          "used by the MQTT connector, which keeps its own config",
	"mq/SchemaRegistryConfig.Format":              "only Avro is implemented",
	"mq/SchemaRegistryConfig.AutoRegister":        "schemas are looked up, never registered",
	"mq/SchemaRegistryConfig.SubjectNameStrategy": "only the default strategy is implemented",

	"s3/Config.Timeout": "the AWS SDK takes its timeout from the context the operation is given",
}

func TestEverySettingIsReadBySomething(t *testing.T) {
	root := "."

	packages, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}

	var unread []string
	stillUnread := map[string]bool{}

	for _, entry := range packages {
		if !entry.IsDir() {
			continue
		}
		pkg := entry.Name()
		for _, field := range unreadFields(t, filepath.Join(root, pkg), pkg) {
			if _, allowed := notRead[field]; allowed {
				stillUnread[field] = true
				continue
			}
			unread = append(unread, field)
		}
	}
	sort.Strings(unread)

	// An entry that is read now is an entry that should go, or the list stops
	// describing anything and starts hiding the next one.
	var stale []string
	for field := range notRead {
		if !stillUnread[field] {
			stale = append(stale, field)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("these are listed as unread and are read now — take them out of notRead:\n  %s",
			strings.Join(stale, "\n  "))
	}

	if len(unread) > 0 {
		t.Errorf("these settings are carried from the configuration into a struct and read by nothing, "+
			"so a service configured with them does not do what the file says:\n  %s",
			strings.Join(unread, "\n  "))
	}
}

// unreadFields returns Config-struct fields that are written and never read in
// the package, as "pkg/Struct.Field".
func unreadFields(t *testing.T, dir, pkg string) []string {
	t.Helper()

	fset := token.NewFileSet()
	files := map[string]*ast.File{}

	// The whole tree under the connector, subpackages included: a driver in
	// mq/kafka reading a setting declared in mq is still the connector
	// reading it, and that is the question being asked.
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, perr := parser.ParseFile(fset, path, nil, 0)
		if perr == nil {
			files[path] = parsed
		}
		return nil
	})
	if len(files) == 0 {
		return nil
	}

	// Fields declared on any *Config struct.
	declared := map[string]string{} // field name -> struct name
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok || !strings.HasSuffix(spec.Name.Name, "Config") {
				return true
			}
			structType, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range structType.Fields.List {
				for _, name := range field.Names {
					if name.IsExported() {
						declared[name.Name] = spec.Name.Name
					}
				}
			}
			return true
		})
	}

	// Which selector expressions are assignment targets, by identity: any
	// other use of a field is a read. Doing it by name instead would call a
	// field read as soon as it is assigned anywhere, which is the mistake
	// this test exists to catch in other people's code.
	targets := map[ast.Node]bool{}
	written := map[string]bool{}

	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.AssignStmt:
				for _, lhs := range node.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok {
						targets[sel] = true
						written[sel.Sel.Name] = true
					}
				}
			case *ast.KeyValueExpr:
				// Field: value inside a struct literal is a write, and the
				// key is not a read of the field.
				if key, ok := node.Key.(*ast.Ident); ok {
					targets[key] = true
					written[key.Name] = true
				}
			}
			return true
		})
	}

	read := map[string]bool{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && !targets[sel] {
				read[sel.Sel.Name] = true
			}
			return true
		})
	}

	var out []string
	for field, structName := range declared {
		if written[field] && !read[field] {
			out = append(out, pkg+"/"+structName+"."+field)
		}
	}
	return out
}
