package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matutetandil/mycel/v3/internal/parser"
	"github.com/matutetandil/mycel/v3/internal/runtime"
	"github.com/matutetandil/mycel/v3/pkg/schema"
)

var (
	addType      string
	addDriver    string
	addFrom      string
	addTo        string
	addOperation string
	addTarget    string
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a connector, flow, type or other block to an existing project",
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
// declarationPath is where a declaration of this kind belongs, taken from the
// same map an editor reads — so the directory this command writes to and the
// one an editor expects cannot come apart, which they had: state machines were
// written to a directory the editor did not know about.
func declarationPath(blockType, name string) string {
	dir := schema.DirectoryFor(blockType)
	if dir == "" {
		return name + ".mycel"
	}
	return filepath.Join(dir, name+".mycel")
}

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

	// A type whose schema requires a driver is not one connector: the type says
	// what kind of system it is and the driver says which one. Generated
	// without it, the file parses, validates until 2.19.0, and fails to start
	// with "no factory found" — so it is refused here, where the fix is one
	// flag, rather than produced as a starting point that cannot run.
	schemaBlock := reg.ConnectorSchema(addType, addDriver)
	if err := requireDriver(addType, addDriver, schemaBlock); err != nil {
		return err
	}

	if err := ensureNameIsFree(name, "connector"); err != nil {
		return err
	}

	body := renderConnector(name, addType, addDriver, provider.ConnectorSchema())
	return writeDeclaration(declarationPath("connector", name), body)
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

	return writeDeclaration(declarationPath("flow", name),
		renderFlow(name, addFrom, addTo, addOperation, addTarget))
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
		case a.Name == "type":
			// Already emitted above.
		case a.Name == "driver" && (driver != "" || a.Required):
			// Already emitted above, or refused before we got here.
		case a.Name == "driver":
			// An optional driver decides what the connector is — a GraphQL
			// server or a GraphQL client — so a file generated without one
			// still has to say the choice exists.
			optional = append(optional, a)
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

	// A rule of the form "say it one way or the other" is answered with the
	// first way, so the generated file is complete; the alternatives are named
	// in the comment above it. Writing them all out would produce a file that
	// says the same thing twice.
	written := map[string]bool{}
	for _, group := range blk.RequiredOneOf {
		for _, a := range blk.Attrs {
			if len(group) == 0 || a.Name != group[0] || written[a.Name] {
				continue
			}
			written[a.Name] = true
			b.WriteString("\n")
			if a.Doc != "" {
				fmt.Fprintf(&b, "  // %s\n", a.Doc)
			}
			if len(group) > 1 {
				fmt.Fprintf(&b, "  // Or instead: %s\n", strings.Join(group[1:], ", "))
			}
			fmt.Fprintf(&b, "  %s = %s\n", a.Name, requiredOneOfPlaceholder(a, blk))
		}
	}

	// A block the connector cannot parse without is written out with it. A
	// profiled connector is the case: it is nothing but its profiles, so a
	// file with none is a file that fails to parse — generating it would put
	// somebody back where `mycel add` exists to save them from.
	for _, child := range blk.Children {
		if child.Labels == 0 {
			continue
		}
		b.WriteString("\n")
		if child.Doc != "" {
			fmt.Fprintf(&b, "  // %s\n", child.Doc)
		}
		fmt.Fprintf(&b, "  %s \"primary\" {\n", child.Type)
		for _, a := range child.Attrs {
			if !a.Required {
				continue
			}
			fmt.Fprintf(&b, "    %s = %s\n", a.Name, placeholderFor(a))
		}
		b.WriteString("  }\n")
	}

	if len(optional) > 0 {
		b.WriteString("\n  // Optional:\n")
		for _, a := range optional {
			if written[a.Name] {
				continue // already written above
			}
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

// renderFlow builds a flow. Everything supplied by a flag is written out;
// what is missing stays a TODO with the explanation attached, so the generated
// file is finished when the caller knew enough to finish it.
func renderFlow(name, from, to, operation, target string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "flow %q {\n", name)
	b.WriteString("  from {\n")
	if from != "" {
		fmt.Fprintf(&b, "    connector = %q\n", from)
	} else {
		b.WriteString("    connector = \"\" // TODO: a connector declared in connectors/\n")
	}
	if operation != "" {
		fmt.Fprintf(&b, "    operation = %q\n", operation)
	} else {
		b.WriteString("    // operation addresses an endpoint on request-style sources\n")
		b.WriteString("    // (REST, GraphQL, gRPC), and narrows a subscription on stream\n")
		b.WriteString("    // sources (queues, CDC) — where omitting it accepts everything.\n")
		b.WriteString("    operation = \"\" // TODO\n")
	}
	b.WriteString("  }\n")

	if to != "" {
		b.WriteString("\n  to {\n")
		fmt.Fprintf(&b, "    connector = %q\n", to)
		if target != "" {
			fmt.Fprintf(&b, "    target    = %q\n", target)
		} else {
			b.WriteString("    target    = \"\" // TODO\n")
		}
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
	case "saga":
		for _, s := range cfg.Sagas {
			if s != nil && s.Name == name {
				return fmt.Errorf("a saga named %q already exists — names are global across every .mycel file", name)
			}
		}
	case "state_machine":
		for _, m := range cfg.StateMachines {
			if m != nil && m.Name == name {
				return fmt.Errorf("a state machine named %q already exists — names are global across every .mycel file", name)
			}
		}
	case "validator":
		for _, v := range cfg.Validators {
			if v != nil && v.Name == name {
				return fmt.Errorf("a validator named %q already exists — names are global across every .mycel file", name)
			}
		}
	case "transform":
		for _, tr := range cfg.Transforms {
			if tr != nil && tr.Name == name {
				return fmt.Errorf("a transform named %q already exists — names are global across every .mycel file", name)
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

var addAspectCmd = &cobra.Command{
	Use:   "aspect <name>",
	Short: "Add an aspect in aspects/<name>.mycel",
	Long: `Generate an aspect skeleton.

An aspect attaches behaviour to flows by name pattern instead of editing each
flow — notifications, cache invalidation, audit trails.

Examples:
  mycel add aspect audit_log --on "create_*,update_*" --when after
  mycel add aspect slack_error_notifier --on sync_orders --when on_error`,
	Args: cobra.ExactArgs(1),
	RunE: runAddAspect,
}

var (
	addOn              string
	addWhen            string
	addActionConnector string
	addActionFlow      string
)

func runAddAspect(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := ensureNameIsFree(name, "aspect"); err != nil {
		return err
	}

	blk := schema.AspectSchema()
	if addWhen != "" {
		if err := validateAgainstSchema(blk, "when", addWhen); err != nil {
			return err
		}
	}

	// An aspect that matches no flow is inert, and the name is the only thing
	// tying it to one. Checking now costs nothing; discovering it later means
	// wondering why a notification never fired.
	if err := ensurePatternsMatchAFlow(addOn); err != nil {
		return err
	}

	// An aspect without an action parses but fails to register at startup
	// ("aspect must have at least one action type"), so there is no useful
	// aspect to generate without one.
	if addActionConnector == "" && addActionFlow == "" {
		return fmt.Errorf("an aspect needs an action: pass --action-connector <name> or --action-flow <name>")
	}
	if addActionConnector != "" && addActionFlow != "" {
		return fmt.Errorf("--action-connector and --action-flow are alternatives; an action calls one or the other")
	}
	if addActionConnector != "" {
		if err := ensureConnectorExists(addActionConnector); err != nil {
			return err
		}
	}
	if addActionFlow != "" {
		if err := ensureFlowExists(addActionFlow); err != nil {
			return err
		}
	}

	return writeDeclaration(declarationPath("aspect", name),
		renderAspect(name, addOn, addWhen, blk))
}

// ensurePatternsMatchAFlow rejects a pattern that matches nothing, using the
// same matcher the runtime dispatches with so the two cannot disagree.
func ensurePatternsMatchAFlow(spec string) error {
	if strings.TrimSpace(spec) == "" {
		return nil
	}
	cfg, err := parseConfigDirQuietly()
	if err != nil {
		return nil
	}

	var flows []string
	for _, f := range cfg.Flows {
		if f != nil {
			flows = append(flows, f.Name)
		}
	}
	if len(flows) == 0 {
		return nil
	}
	sort.Strings(flows)

	for _, pattern := range splitPatterns(spec) {
		matched := false
		for _, f := range flows {
			if ok, _ := filepath.Match(pattern, f); ok {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("pattern %q matches no flow — this project declares: %s",
				pattern, strings.Join(flows, ", "))
		}
	}
	return nil
}

// ensureFlowExists rejects an action invoking a flow that is not there.
func ensureFlowExists(name string) error {
	cfg, err := parseConfigDirQuietly()
	if err != nil {
		return nil
	}
	var known []string
	for _, f := range cfg.Flows {
		if f == nil {
			continue
		}
		if f.Name == name {
			return nil
		}
		known = append(known, f.Name)
	}
	sort.Strings(known)
	return fmt.Errorf("no flow %q — this project declares: %s", name, strings.Join(known, ", "))
}

// splitPatterns splits and trims a comma-separated pattern list.
func splitPatterns(spec string) []string {
	var out []string
	for _, p := range strings.Split(spec, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// validateAgainstSchema rejects a flag value the schema does not allow, so the
// mistake surfaces here rather than as a silently inert aspect.
func validateAgainstSchema(blk schema.Block, attr, value string) error {
	for _, a := range blk.Attrs {
		if a.Name != attr || len(a.Values) == 0 {
			continue
		}
		for _, v := range a.Values {
			if v == value {
				return nil
			}
		}
		return fmt.Errorf("invalid --%s %q; one of: %s", attr, value, strings.Join(a.Values, ", "))
	}
	return nil
}

// renderAspect builds the HCL for an aspect from its schema.
//
// An aspect with no action does nothing at all, so one is always generated —
// unlike the optional blocks, which are listed as comments. The `when` values
// come from the schema rather than a literal here, which is how `on_drop`
// reaches this skeleton now that the schema knows about it.
func renderAspect(name, on, when string, blk schema.Block) string {
	var b strings.Builder

	if blk.Doc != "" {
		fmt.Fprintf(&b, "// %s\n", blk.Doc)
	}
	fmt.Fprintf(&b, "aspect %q {\n", name)

	if on != "" {
		patterns := strings.Split(on, ",")
		quoted := make([]string, 0, len(patterns))
		for _, p := range patterns {
			if p = strings.TrimSpace(p); p != "" {
				quoted = append(quoted, fmt.Sprintf("%q", p))
			}
		}
		fmt.Fprintf(&b, "  // Flow name patterns this attaches to (glob)\n")
		fmt.Fprintf(&b, "  on   = [%s]\n", strings.Join(quoted, ", "))
	} else {
		fmt.Fprintf(&b, "  // Flow name patterns this attaches to (glob)\n")
		fmt.Fprintf(&b, "  on   = [] // TODO — e.g. [\"create_*\", \"sync_orders\"]\n")
	}

	if when != "" {
		fmt.Fprintf(&b, "  when = %q\n", when)
	} else {
		fmt.Fprintf(&b, "  when = \"after\" // %s\n", whenValues(blk))
	}

	// An action must name a connector or a flow — the parser rejects one that
	// names neither, so a placeholder here would generate a file that does not
	// load. Without a target, the action is written as a comment instead: an
	// aspect with no action parses, does nothing, and says how to finish it.
	b.WriteString("\n  action {\n")
	if addActionConnector != "" {
		fmt.Fprintf(&b, "    connector = %q\n", addActionConnector)
	} else {
		fmt.Fprintf(&b, "    flow = %q\n", addActionFlow)
	}
	b.WriteString("\n    transform {\n")
	b.WriteString("      // CEL. Available: input, output, error (on_error), drop (on_drop), _flow\n")
	b.WriteString("    }\n")
	b.WriteString("  }\n")

	if optional := optionalChildren(blk); len(optional) > 0 {
		b.WriteString("\n  // Also available: " + strings.Join(optional, ", ") + "\n")
	}

	b.WriteString("}\n")
	return b.String()
}

// whenValues renders the allowed `when` values as a hint.
func whenValues(blk schema.Block) string {
	for _, a := range blk.Attrs {
		if a.Name == "when" && len(a.Values) > 0 {
			return "one of: " + strings.Join(a.Values, ", ")
		}
	}
	return ""
}

// optionalChildren lists the nested blocks an aspect may carry, minus the
// action already generated.
func optionalChildren(blk schema.Block) []string {
	var names []string
	for _, c := range blk.Children {
		if c.Type != "action" {
			names = append(names, c.Type+" { }")
		}
	}
	sort.Strings(names)
	return names
}

var addTypeCmd = &cobra.Command{
	Use:   "type <name>",
	Short: "Add a type in types/<name>.mycel",
	Long: `Generate a type from a field list.

Fields are given as name:type pairs, so the generated type is complete rather
than a skeleton to fill in. An optional format follows the type.

Examples:
  mycel add type user --fields "id:number,email:string:email,name:string"
  mycel add type order --fields "id:uuid,total:number,placed_at:string:datetime"`,
	Args: cobra.ExactArgs(1),
	RunE: runAddType,
}

var addFields string

func runAddType(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := ensureNameIsFree(name, "type"); err != nil {
		return err
	}

	fields, err := parseFieldSpecs(addFields)
	if err != nil {
		return err
	}

	return writeDeclaration(declarationPath("type", name), renderType(name, fields))
}

// typeField is one parsed `name:type[:format]` spec.
type typeField struct {
	name   string
	kind   string
	format string
}

// parseFieldSpecs turns "id:number,email:string:email" into fields.
//
// Validated against the schema rather than a literal list here, so a value type
// or format added to the schema is accepted without touching this code — and a
// typo is rejected now rather than at startup.
func parseFieldSpecs(spec string) ([]typeField, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}

	valid := func(v string, allowed []string) bool {
		for _, a := range allowed {
			if a == v {
				return true
			}
		}
		return false
	}

	var fields []typeField
	for _, raw := range strings.Split(spec, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.Split(raw, ":")
		if len(parts) < 2 || parts[0] == "" {
			return nil, fmt.Errorf("field %q must be name:type — e.g. email:string, or email:string:email to add a format", raw)
		}

		f := typeField{name: parts[0], kind: parts[1]}

		// `id:uuid` is the shorthand people reach for; uuid is a format of a
		// string, not a type, so accept it and say what it became.
		if !valid(f.kind, schema.FieldTypes()) && valid(f.kind, schema.StringFormats()) {
			f.format, f.kind = f.kind, "string"
		}
		if !valid(f.kind, schema.FieldTypes()) {
			return nil, fmt.Errorf("unknown field type %q in %q; one of: %s",
				f.kind, raw, strings.Join(schema.FieldTypes(), ", "))
		}

		if len(parts) > 2 && parts[2] != "" {
			if !valid(parts[2], schema.StringFormats()) {
				return nil, fmt.Errorf("unknown format %q in %q; one of: %s",
					parts[2], raw, strings.Join(schema.StringFormats(), ", "))
			}
			f.format = parts[2]
		}
		fields = append(fields, f)
	}
	return fields, nil
}

// renderType builds the HCL for a type.
func renderType(name string, fields []typeField) string {
	var b strings.Builder

	fmt.Fprintf(&b, "// %s\n", schema.TypeSchema().Doc)
	fmt.Fprintf(&b, "type %q {\n", name)

	if len(fields) == 0 {
		b.WriteString("  // field = string\n")
		b.WriteString("  // Constraints are call arguments:\n")
		b.WriteString("  //   email = string({ format = \"email\" })\n")
		b.WriteString("  //   age   = number({ min = 0, max = 150 })\n")
		b.WriteString("}\n")
		return b.String()
	}

	width := 0
	for _, f := range fields {
		if len(f.name) > width {
			width = len(f.name)
		}
	}
	for _, f := range fields {
		if f.format != "" {
			// Constraints are call arguments, not a nested block: the brace
			// form without parentheses does not parse.
			fmt.Fprintf(&b, "  %-*s = %s({ format = %q })\n", width, f.name, f.kind, f.format)
			continue
		}
		fmt.Fprintf(&b, "  %-*s = %s\n", width, f.name, f.kind)
	}

	b.WriteString("}\n")
	return b.String()
}

// requireDriver refuses a connector type that cannot be built without one.
func requireDriver(connType, driver string, blk schema.Block) error {
	for _, a := range blk.Attrs {
		if a.Name != "driver" || !a.Required {
			continue
		}
		if strings.TrimSpace(driver) == "" {
			return fmt.Errorf("a %s connector needs --driver (one of: %s)",
				connType, strings.Join(a.Values, ", "))
		}
		if len(a.Values) > 0 && !slices.Contains(a.Values, driver) {
			return fmt.Errorf("%q is not a %s driver; expected one of: %s",
				driver, connType, strings.Join(a.Values, ", "))
		}
	}
	return nil
}

// requiredOneOfPlaceholder fills in the first of a set of alternatives.
//
// When the alternatives select one of the blocks the generator has just
// written — a profiled connector picking which profile to use — the answer is
// that block's name. Otherwise it is an ordinary placeholder, the same as any
// other attribute somebody has to fill in.
func requiredOneOfPlaceholder(a schema.Attr, blk schema.Block) string {
	for _, child := range blk.Children {
		if child.Labels > 0 && a.Type == schema.TypeString {
			return "\"primary\""
		}
	}
	return placeholderFor(a)
}
