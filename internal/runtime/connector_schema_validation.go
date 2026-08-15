package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/parser"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// ValidateConnectorSchemas checks connector settings that accept one of a fixed
// set of words against the list the connector declares.
//
// Those lists have been in the schema since connectors began describing
// themselves, and nothing read them outside the IDE. A misspelt word therefore
// validated clean and was discovered later, or not at all: the HTTP client
// turns an auth type it does not recognise into no authentication and sends
// every request without credentials, which reads from the outside like a broken
// server rather than a typo in a configuration file.
//
// Only attributes that declare a list are checked, and only when the value was
// written as a word. Connectors with no registered schema — plugins bring their
// own types — are left alone, as are values of the wrong kind entirely, which
// the connector reports with the context of what it was doing.
//
// Returns one error per offending setting, sorted so a configuration that fails
// fails the same way twice.
func ValidateConnectorSchemas(config *parser.Configuration, reg *schema.Registry) []error {
	if config == nil || reg == nil {
		return nil
	}

	var errs []error
	for _, c := range config.Connectors {
		if c == nil || c.Type == "" {
			continue
		}
		if reg.Lookup(c.Type, c.Driver) == nil {
			continue
		}

		block := reg.ConnectorSchema(c.Type, c.Driver)

		if err := checkDriver(c, &block); err != nil {
			errs = append(errs, err)
		}

		for _, problem := range checkValues(&block, c.Properties, "") {
			errs = append(errs, fmt.Errorf(
				"connector %q (%s): %s = %q is not one of: %s",
				c.Name, c.Type, problem.path, problem.value, strings.Join(problem.allowed, ", ")))
		}
	}

	sort.Slice(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
	return errs
}

type unknownValue struct {
	path    string
	value   string
	allowed []string
}

// checkValues walks a block schema alongside the properties parsed for it,
// descending into nested blocks so a setting buried two levels down is reported
// with the path that leads to it.
func checkValues(block *schema.Block, props map[string]interface{}, prefix string) []unknownValue {
	if block == nil || props == nil {
		return nil
	}

	var found []unknownValue
	for _, attr := range block.Attrs {
		if len(attr.Values) == 0 {
			continue
		}
		// Anything not written as a word is a different mistake.
		value, ok := props[attr.Name].(string)
		if !ok || value == "" {
			continue
		}
		if !allows(attr.Values, value) {
			found = append(found, unknownValue{prefix + attr.Name, value, attr.Values})
		}
	}

	for i := range block.Children {
		nested := &block.Children[i]
		// A block appears either once as a map or repeated as a list of maps.
		switch child := props[nested.Type].(type) {
		case map[string]interface{}:
			found = append(found, checkValues(nested, child, prefix+nested.Type+".")...)
		case []interface{}:
			for _, entry := range child {
				if m, ok := entry.(map[string]interface{}); ok {
					found = append(found, checkValues(nested, m, prefix+nested.Type+".")...)
				}
			}
		case []map[string]interface{}:
			for _, m := range child {
				found = append(found, checkValues(nested, m, prefix+nested.Type+".")...)
			}
		}
	}

	return found
}

// allows compares without regard to case, because several readers normalise
// before they compare — a GraphQL operation_type is written Query or query and
// means the same thing. Being stricter here than the code that reads the value
// would reject configurations that work.
func allows(values []string, value string) bool {
	for _, allowed := range values {
		if strings.EqualFold(allowed, value) {
			return true
		}
	}
	return false
}

// walkSchemaBlocks visits every attribute of a block and everything nested in
// it, naming each by the path that reaches it.
func walkSchemaBlocks(block *schema.Block, prefix string, visit func(path string, attr schema.Attr)) {
	if block == nil {
		return
	}
	for _, attr := range block.Attrs {
		visit(prefix+"."+attr.Name, attr)
	}
	for i := range block.Children {
		walkSchemaBlocks(&block.Children[i], prefix+"."+block.Children[i].Type, visit)
	}
}

// checkDriver reports a connector that does not say which implementation runs
// behind it.
//
// A `database` or `mq` connector is not one thing: the type says what kind of
// system it is, and the driver says which one. Nothing checked that a driver
// was given, so `connector "db" { type = "database" }` parsed, validated, was
// listed by `mycel validate` as a database connector, and then failed at
// start-up with `no factory found for connector type=database driver=`. Worse,
// `mycel add connector db --type database` generated exactly that file, so the
// command that exists to produce a correct starting point produced one that
// could not run.
//
// Only the driver is enforced here, not every attribute a schema marks
// Required. Several of those are alternatives rather than obligations — a
// Postgres connector needs a database name *or* a URL that contains one — and
// refusing the second form would break configurations that work today. The
// driver has no such alternative: without it there is nothing to build.
func checkDriver(c *connector.Config, block *schema.Block) error {
	var attr *schema.Attr
	for i := range block.Attrs {
		if block.Attrs[i].Name == "driver" && block.Attrs[i].Required {
			attr = &block.Attrs[i]
			break
		}
	}
	if attr == nil {
		return nil
	}
	if strings.TrimSpace(c.Driver) != "" {
		return nil
	}
	return fmt.Errorf("connector %q (%s): needs a driver — one of %s",
		c.Name, c.Type, strings.Join(attr.Values, ", "))
}
