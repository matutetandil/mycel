package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matutetandil/mycel/internal/parser"
	"github.com/matutetandil/mycel/internal/runtime"
	"github.com/matutetandil/mycel/pkg/schema"
)

var (
	addType   string
	addDriver string
	addFrom   string
	addTo     string
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a connector or flow to an existing project",
	Long: `Add a piece to an existing project, in its own file.

Mycel merges every .mycel file under the config directory, so where a
declaration lives is a choice about readability, not behaviour. These commands
make the readable choice the default one: connectors/<name>.mycel and
flows/<name>.mycel, one declaration per file.

The skeleton is generated from the connector's own schema, so required
attributes are always the ones the runtime actually requires.`,
}

var addConnectorCmd = &cobra.Command{
	Use:   "connector <name>",
	Short: "Add a connector in connectors/<name>.mycel",
	Long: `Generate a connector with the attributes its type requires.

Examples:
  mycel add connector orders_db --type database --driver postgres
  mycel add connector rabbit --type mq --driver rabbitmq
  mycel add connector api --type rest

  # See what is available
  mycel add connector --list`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAddConnector,
}

var addFlowCmd = &cobra.Command{
	Use:   "flow <name>",
	Short: "Add a flow in flows/<name>.mycel",
	Long: `Generate a flow skeleton wired to connectors that already exist.

Examples:
  mycel add flow order_created --from rabbit --to orders_db
  mycel add flow list_orders --from api`,
	Args: cobra.ExactArgs(1),
	RunE: runAddFlow,
}

var addListTypes bool

// runAddConnector writes connectors/<name>.mycel.
func runAddConnector(cmd *cobra.Command, args []string) error {
	reg := runtime.NewSchemaRegistry()

	if addListTypes {
		return listConnectorTypes(reg)
	}
	if len(args) == 0 {
		return fmt.Errorf("a connector name is required (or use --list to see available types)")
	}
	name := args[0]

	if addType == "" {
		return fmt.Errorf("--type is required; run `mycel add connector --list` to see the options")
	}

	provider := reg.Lookup(addType, addDriver)
	if provider == nil {
		return fmt.Errorf("unknown connector type %q (driver %q); run `mycel add connector --list`",
			addType, addDriver)
	}

	if err := ensureNameIsFree(name, "connector"); err != nil {
		return err
	}

	body := renderConnector(name, addType, addDriver, provider.ConnectorSchema())
	return writeDeclaration(filepath.Join("connectors", name+".mycel"), body)
}

// runAddFlow writes flows/<name>.mycel.
func runAddFlow(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := ensureNameIsFree(name, "flow"); err != nil {
		return err
	}

	// A flow referencing a connector that does not exist parses fine and then
	// fails at startup, so check now while the fix is one flag away.
	if addFrom != "" {
		if err := ensureConnectorExists(addFrom); err != nil {
			return err
		}
	}
	if addTo != "" {
		if err := ensureConnectorExists(addTo); err != nil {
			return err
		}
	}

	return writeDeclaration(filepath.Join("flows", name+".mycel"), renderFlow(name, addFrom, addTo))
}

// renderConnector builds the HCL for a connector from its declared schema.
//
// Required attributes are emitted with a placeholder so the file is a checklist
// rather than a guess; optional ones are listed as comments so the reader can
// see what exists without opening the reference. Generating from the schema is
// the point: a hardcoded template would drift the first time a connector
// changed, which is the failure this project keeps finding.
func renderConnector(name, connType, driver string, blk schema.Block) string {
	var b strings.Builder

	if blk.Doc != "" {
		fmt.Fprintf(&b, "// %s\n", blk.Doc)
	}
	fmt.Fprintf(&b, "connector %q {\n", name)
	fmt.Fprintf(&b, "  type   = %q\n", connType)
	if driver != "" {
		fmt.Fprintf(&b, "  driver = %q\n", driver)
	}

	var required, optional []schema.Attr
	for _, a := range blk.Attrs {
		switch {
		case a.Name == "type" || a.Name == "driver":
			// Already emitted above.
		case a.Required:
			required = append(required, a)
		default:
			optional = append(optional, a)
		}
	}

	if len(required) > 0 {
		b.WriteString("\n")
		for _, a := range required {
			if a.Doc != "" {
				fmt.Fprintf(&b, "  // %s\n", a.Doc)
			}
			fmt.Fprintf(&b, "  %s = %s\n", a.Name, placeholderFor(a))
		}
	}

	if len(optional) > 0 {
		b.WriteString("\n  // Optional:\n")
		for _, a := range optional {
			line := "  //   " + a.Name
			if len(a.Values) > 0 {
				line += " — one of: " + strings.Join(a.Values, ", ")
			} else if a.Doc != "" {
				line += " — " + a.Doc
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("}\n")
	return b.String()
}

// placeholderFor produces a value of the right shape, preferring a real default
// or the first allowed enum value over a bare TODO.
func placeholderFor(a schema.Attr) string {
	if a.Default != nil {
		switch v := a.Default.(type) {
		case string:
			return fmt.Sprintf("%q", v)
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	if len(a.Values) > 0 {
		return fmt.Sprintf("%q", a.Values[0])
	}
	switch a.Type {
	case schema.TypeNumber:
		return "0 // TODO"
	case schema.TypeBool:
		return "false // TODO"
	case schema.TypeList:
		return "[] // TODO"
	case schema.TypeMap:
		return "{} // TODO"
	default:
		// env() rather than a literal: these are usually hosts and credentials,
		// and a committed literal is how secrets end up in git.
		return fmt.Sprintf("env(%q) // TODO", strings.ToUpper(a.Name))
	}
}

// renderFlow builds a flow skeleton.
func renderFlow(name, from, to string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "flow %q {\n", name)
	b.WriteString("  from {\n")
	if from != "" {
		fmt.Fprintf(&b, "    connector = %q\n", from)
	} else {
		b.WriteString("    connector = \"\" // TODO: a connector declared in connectors/\n")
	}
	b.WriteString("    // operation addresses an endpoint on request-style sources\n")
	b.WriteString("    // (REST, GraphQL, gRPC), and narrows a subscription on stream\n")
	b.WriteString("    // sources (queues, CDC) — where omitting it accepts everything.\n")
	b.WriteString("    operation = \"\" // TODO\n")
	b.WriteString("  }\n")

	if to != "" {
		b.WriteString("\n  to {\n")
		fmt.Fprintf(&b, "    connector = %q\n", to)
		b.WriteString("    target    = \"\" // TODO\n")
		b.WriteString("  }\n")
	} else {
		b.WriteString("\n  // Add a to { } to write somewhere, or a response { } to answer\n")
		b.WriteString("  // directly. A flow with neither echoes its input.\n")
	}

	b.WriteString("}\n")
	return b.String()
}

// ensureNameIsFree fails when the name is already taken anywhere in the config.
// Names are global across every .mycel file, so a clash is a parse error the
// user would otherwise meet after editing rather than before.
func ensureNameIsFree(name, kind string) error {
	cfg, err := parseConfigDirQuietly()
	if err != nil {
		// An unparseable config is not this command's problem to report; the
		// clash check is a courtesy, not a gate.
		return nil
	}

	switch kind {
	case "connector":
		for _, c := range cfg.Connectors {
			if c != nil && c.Name == name {
				return fmt.Errorf("a connector named %q already exists — names are global across every .mycel file", name)
			}
		}
	case "flow":
		for _, f := range cfg.Flows {
			if f != nil && f.Name == name {
				return fmt.Errorf("a flow named %q already exists — names are global across every .mycel file", name)
			}
		}
	}
	return nil
}

// ensureConnectorExists rejects a flow wired to a connector that is not there.
func ensureConnectorExists(name string) error {
	cfg, err := parseConfigDirQuietly()
	if err != nil {
		return nil
	}
	var known []string
	for _, c := range cfg.Connectors {
		if c == nil {
			continue
		}
		if c.Name == name {
			return nil
		}
		known = append(known, c.Name)
	}
	sort.Strings(known)
	if len(known) == 0 {
		return fmt.Errorf("no connector %q — this project declares none yet; add one with `mycel add connector`", name)
	}
	return fmt.Errorf("no connector %q — this project declares: %s", name, strings.Join(known, ", "))
}

// parseConfigDirQuietly parses the config directory without reporting on it.
func parseConfigDirQuietly() (*parser.Configuration, error) {
	p := parser.NewHCLParserWithRegistry(runtime.NewSchemaRegistry())
	return p.Parse(context.Background(), configDir)
}

// writeDeclaration writes one generated file, refusing to replace anything.
func writeDeclaration(relPath, body string) error {
	full := filepath.Join(configDir, relPath)
	if _, err := os.Stat(full); err == nil {
		return fmt.Errorf("%s already exists", full)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", full, err)
	}

	fmt.Printf("  created %s\n\n", full)
	fmt.Printf("Fill in the TODOs, then:\n  mycel validate\n")
	return nil
}

// listConnectorTypes prints what `--type` accepts.
func listConnectorTypes(reg *schema.Registry) error {
	types := reg.AllConnectorTypes()
	sort.Strings(types)

	fmt.Printf("Connector types:\n\n")
	for _, t := range types {
		fmt.Printf("  %s\n", t)
	}
	fmt.Printf("\nSome types take a --driver (database: postgres, mysql, sqlite, mongodb;\n")
	fmt.Printf("mq: rabbitmq, kafka, redis). See the connector reference for each.\n")
	return nil
}
