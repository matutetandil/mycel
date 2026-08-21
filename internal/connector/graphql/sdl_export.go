package graphql

import (
	"fmt"
	"sort"
	"strings"

	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/validate"
)

// ExportSDL renders the schema a configuration describes, without starting a
// server.
//
// The generator this uses was written and never called: `mycel export
// graphql-schema` is documented in the CLI reference and in the federation
// guide, and no such command existed. Producing the schema offline is the point
// — a federation gateway needs the SDL at build time, not from a running
// service.
//
// Types and their inputs come from the type blocks. Query and Mutation are
// derived from the flows that serve them, since a field exists exactly when a
// flow answers it: `operation = "Query.users"` is the declaration.
func ExportSDL(types []*validate.TypeSchema, flows []*flow.Config) string {
	schemas := make(map[string]*validate.TypeSchema, len(types))
	for _, t := range types {
		schemas[t.Name] = t
	}

	var sb strings.Builder
	sb.WriteString(NewSDLGenerator().GenerateFromTypeSchemas(schemas))

	queries, mutations := graphQLFields(flows)
	writeRootType(&sb, "Query", queries)
	writeRootType(&sb, "Mutation", mutations)

	return sb.String()
}

// graphQLField is one entry on a root type.
type graphQLField struct {
	Name   string
	Args   string
	Result string
}

// graphQLFields collects the Query and Mutation fields the flows declare.
func graphQLFields(flows []*flow.Config) (queries, mutations []graphQLField) {
	for _, f := range flows {
		if f == nil || f.From == nil {
			continue
		}
		operation := f.From.GetOperation()
		root, name, found := strings.Cut(operation, ".")
		if !found || name == "" {
			continue
		}

		field := graphQLField{Name: name, Result: "JSON"}
		if f.Validate != nil {
			if f.Validate.Output != "" {
				field.Result = f.Validate.Output
			}
			// An input type becomes the field's argument, which is how a
			// caller is meant to pass one.
			if f.Validate.Input != "" {
				field.Args = fmt.Sprintf("(input: %sInput!)", f.Validate.Input)
			}
		}

		switch root {
		case "Query":
			queries = append(queries, field)
		case "Mutation":
			mutations = append(mutations, field)
		}
	}

	sort.Slice(queries, func(i, j int) bool { return queries[i].Name < queries[j].Name })
	sort.Slice(mutations, func(i, j int) bool { return mutations[i].Name < mutations[j].Name })
	return queries, mutations
}

// writeRootType writes one of the root types, and nothing at all when it has no
// fields — an empty `type Query {}` is not valid SDL.
func writeRootType(sb *strings.Builder, name string, fields []graphQLField) {
	if len(fields) == 0 {
		return
	}
	fmt.Fprintf(sb, "type %s {\n", name)
	for _, f := range fields {
		fmt.Fprintf(sb, "  %s%s: %s\n", f.Name, f.Args, f.Result)
	}
	sb.WriteString("}\n\n")
}
