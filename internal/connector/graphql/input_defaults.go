package graphql

import (
	"sort"
	"strconv"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/kinds"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/printer"
	"github.com/graphql-go/graphql/language/source"
	"github.com/graphql-go/graphql/language/visitor"
)

// A value the schema gives a default is a value the caller may leave out.
//
// The spec says so in two places — an input object field is required only when
// it is non-null AND has no default, and the same holds for a field argument —
// and the library checks both by looking at the type alone. So `loud: Boolean!
// = false` omitted was reported as a missing required value and the request
// failed during validation, before any flow ran. Everything short of a real
// call looked green.
//
// The default is filled in here, before the request is validated, which is
// where the spec puts it conceptually: a field with a default that was not
// provided takes the default. Only non-null values are filled in, because
// those are the ones validation refuses; a nullable field's default is applied
// by the library at coercion time and needs no help.

// defaultFiller fills in the values a schema says may be left out.
//
// needed is answered once per schema: a schema that declares no such default —
// most of them — pays nothing beyond the flag.
type defaultFiller struct {
	schema *graphql.Schema
	needed bool
}

func newDefaultFiller(schema *graphql.Schema) *defaultFiller {
	f := &defaultFiller{schema: schema}
	if schema == nil {
		return f
	}
	for _, ttype := range schema.TypeMap() {
		switch t := ttype.(type) {
		case *graphql.InputObject:
			for _, field := range t.Fields() {
				if omissible(field.Type, field.DefaultValue) {
					f.needed = true
					return f
				}
			}
		case *graphql.Object:
			for _, field := range t.Fields() {
				for _, arg := range field.Args {
					if omissible(arg.Type, arg.DefaultValue) {
						f.needed = true
						return f
					}
				}
			}
		}
	}
	return f
}

// omissible reports whether a field or argument is one the caller may leave
// out but the library would refuse: non-null, and with a default to fall back
// on.
func omissible(ttype graphql.Input, defaultValue interface{}) bool {
	if defaultValue == nil {
		return false
	}
	_, nonNull := ttype.(*graphql.NonNull)
	return nonNull
}

// apply returns the query and variables to execute, with omitted defaults
// filled in. Anything it cannot make sense of is handed back untouched, so a
// request the library would have answered is never turned into an error here.
func (f *defaultFiller) apply(query string, variables map[string]interface{}, operationName string) (string, map[string]interface{}) {
	if f == nil || !f.needed || query == "" {
		return query, variables
	}

	doc, err := parseRequest(query)
	if err != nil {
		// A query that does not parse is reported by the library, with its
		// own message and location.
		return query, variables
	}

	variables = f.fillVariables(doc, operationName, variables)

	if !f.fillLiterals(doc) {
		return query, variables
	}

	printed, ok := printer.Print(doc).(string)
	if !ok {
		return query, variables
	}
	// Printing the document back is only worth it if the result is still the
	// same request: a rewrite that no longer parses would turn a working call
	// into a parse error.
	if _, err := parseRequest(printed); err != nil {
		return query, variables
	}
	return printed, variables
}

func parseRequest(query string) (*ast.Document, error) {
	return parser.Parse(parser.ParseParams{Source: source.NewSource(&source.Source{
		Body: []byte(query),
		Name: "GraphQL request",
	})})
}

// fillLiterals writes the missing defaults into the document itself, which is
// how a value written inline — `echo(input: {name: "hello"})` — gets one. It
// reports whether anything was added.
func (f *defaultFiller) fillLiterals(doc *ast.Document) bool {
	info := graphql.NewTypeInfo(&graphql.TypeInfoConfig{Schema: f.schema})

	// The document is mutated after the walk rather than during it, so the
	// visitor never sees the nodes being added.
	var fills []func()

	opts := &visitor.VisitorOptions{
		KindFuncMap: map[string]visitor.NamedVisitFuncs{
			kinds.ObjectValue: {Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
				node, ok := p.Node.(*ast.ObjectValue)
				if !ok {
					return visitor.ActionNoChange, nil
				}
				obj, ok := graphql.GetNamed(info.InputType()).(*graphql.InputObject)
				if !ok {
					return visitor.ActionNoChange, nil
				}

				written := map[string]bool{}
				for _, field := range node.Fields {
					if field != nil && field.Name != nil {
						written[field.Name.Value] = true
					}
				}
				for _, name := range sortedInputFields(obj) {
					field := obj.Fields()[name]
					if written[name] || !omissible(field.Type, field.DefaultValue) {
						continue
					}
					value, ok := astFromDefault(field.DefaultValue, field.Type)
					if !ok {
						continue
					}
					fills = append(fills, func() {
						node.Fields = append(node.Fields, objectField(name, value))
					})
				}
				return visitor.ActionNoChange, nil
			}},
			kinds.Field: {Kind: func(p visitor.VisitFuncParams) (string, interface{}) {
				node, ok := p.Node.(*ast.Field)
				if !ok {
					return visitor.ActionNoChange, nil
				}
				fieldDef := info.FieldDef()
				if fieldDef == nil {
					return visitor.ActionNoChange, nil
				}

				written := map[string]bool{}
				for _, arg := range node.Arguments {
					if arg != nil && arg.Name != nil {
						written[arg.Name.Value] = true
					}
				}
				for _, arg := range fieldDef.Args {
					if arg == nil || written[arg.Name()] || !omissible(arg.Type, arg.DefaultValue) {
						continue
					}
					value, ok := astFromDefault(arg.DefaultValue, arg.Type)
					if !ok {
						continue
					}
					name := arg.Name()
					fills = append(fills, func() {
						node.Arguments = append(node.Arguments, ast.NewArgument(&ast.Argument{
							Name:  ast.NewName(&ast.Name{Value: name}),
							Value: value,
						}))
					})
				}
				return visitor.ActionNoChange, nil
			}},
		},
	}

	visitor.Visit(doc, visitor.VisitWithTypeInfo(info, opts), nil)
	for _, fill := range fills {
		fill()
	}
	return len(fills) > 0
}

// fillVariables does the same for values that arrive as variables, which the
// document does not describe: there the value is a plain map and the type
// comes from the variable's declaration.
func (f *defaultFiller) fillVariables(doc *ast.Document, operationName string, variables map[string]interface{}) map[string]interface{} {
	if len(variables) == 0 {
		return variables
	}
	op := operationFor(doc, operationName)
	if op == nil {
		return variables
	}

	filled := variables
	copied := false
	for _, def := range op.VariableDefinitions {
		if def == nil || def.Variable == nil || def.Variable.Name == nil {
			continue
		}
		name := def.Variable.Name.Value
		value, present := variables[name]
		if !present {
			// An absent variable falls back to the default written on the
			// declaration, which is the library's business.
			continue
		}
		ttype := inputTypeFromAST(f.schema, def.Type)
		if ttype == nil {
			continue
		}
		newValue, changed := fillValue(ttype, value)
		if !changed {
			continue
		}
		if !copied {
			// The caller's map is left alone until there is something to
			// change in it.
			filled = copyMap(variables)
			copied = true
		}
		filled[name] = newValue
	}
	return filled
}

// operationFor picks the operation whose variables are being sent: the one
// named, or the only one there is.
func operationFor(doc *ast.Document, operationName string) *ast.OperationDefinition {
	var only *ast.OperationDefinition
	count := 0
	for _, def := range doc.Definitions {
		op, ok := def.(*ast.OperationDefinition)
		if !ok {
			continue
		}
		count++
		only = op
		if operationName != "" && op.Name != nil && op.Name.Value == operationName {
			return op
		}
	}
	if operationName == "" && count == 1 {
		return only
	}
	return nil
}

// fillValue walks a variable's value against its declared type, filling in the
// fields left out. It reports whether anything changed, so the caller's map is
// copied only when it has to be.
func fillValue(ttype graphql.Input, value interface{}) (interface{}, bool) {
	switch t := ttype.(type) {
	case *graphql.NonNull:
		inner, ok := t.OfType.(graphql.Input)
		if !ok {
			return value, false
		}
		return fillValue(inner, value)
	case *graphql.List:
		inner, ok := t.OfType.(graphql.Input)
		if !ok {
			return value, false
		}
		items, ok := value.([]interface{})
		if !ok {
			return fillValue(inner, value)
		}
		out := make([]interface{}, len(items))
		changed := false
		for i, item := range items {
			filled, itemChanged := fillValue(inner, item)
			out[i] = filled
			changed = changed || itemChanged
		}
		if !changed {
			return value, false
		}
		return out, true
	case *graphql.InputObject:
		given, ok := value.(map[string]interface{})
		if !ok {
			return value, false
		}
		out := copyMap(given)
		changed := false
		for name, field := range t.Fields() {
			if field == nil {
				continue
			}
			if inner, present := given[name]; present {
				filled, innerChanged := fillValue(field.Type, inner)
				if innerChanged {
					out[name] = filled
					changed = true
				}
				continue
			}
			if !omissible(field.Type, field.DefaultValue) {
				continue
			}
			// A default that is itself an object may leave out its own
			// defaults, so it goes through the same walk.
			filled, _ := fillValue(field.Type, field.DefaultValue)
			out[name] = filled
			changed = true
		}
		if !changed {
			return value, false
		}
		return out, true
	}
	return value, false
}

// astFromDefault renders a default value as the literal a caller would have
// written. The library has its own version of this, used by introspection, but
// it is unexported and gives up on input objects.
func astFromDefault(value interface{}, ttype graphql.Input) (ast.Value, bool) {
	switch t := ttype.(type) {
	case *graphql.NonNull:
		inner, ok := t.OfType.(graphql.Input)
		if !ok {
			return nil, false
		}
		return astFromDefault(value, inner)
	case *graphql.List:
		inner, ok := t.OfType.(graphql.Input)
		if !ok {
			return nil, false
		}
		items, ok := value.([]interface{})
		if !ok {
			// A single value where a list is expected is a list of one.
			item, ok := astFromDefault(value, inner)
			if !ok {
				return nil, false
			}
			return ast.NewListValue(&ast.ListValue{Values: []ast.Value{item}}), true
		}
		values := make([]ast.Value, 0, len(items))
		for _, item := range items {
			node, ok := astFromDefault(item, inner)
			if !ok {
				return nil, false
			}
			values = append(values, node)
		}
		return ast.NewListValue(&ast.ListValue{Values: values}), true
	case *graphql.InputObject:
		given, ok := value.(map[string]interface{})
		if !ok {
			return nil, false
		}
		var fields []*ast.ObjectField
		for _, name := range sortedInputFields(t) {
			field := t.Fields()[name]
			inner, present := given[name]
			if !present {
				if !omissible(field.Type, field.DefaultValue) {
					continue
				}
				inner = field.DefaultValue
			}
			node, ok := astFromDefault(inner, field.Type)
			if !ok {
				return nil, false
			}
			fields = append(fields, objectField(name, node))
		}
		return ast.NewObjectValue(&ast.ObjectValue{Fields: fields}), true
	case *graphql.Enum:
		name, ok := value.(string)
		if !ok {
			return nil, false
		}
		return ast.NewEnumValue(&ast.EnumValue{Value: name}), true
	case *graphql.Scalar:
		return scalarFromDefault(value, t)
	}
	return nil, false
}

func scalarFromDefault(value interface{}, ttype *graphql.Scalar) (ast.Value, bool) {
	switch v := value.(type) {
	case bool:
		return ast.NewBooleanValue(&ast.BooleanValue{Value: v}), true
	case int:
		return ast.NewIntValue(&ast.IntValue{Value: strconv.Itoa(v)}), true
	case int32:
		return ast.NewIntValue(&ast.IntValue{Value: strconv.FormatInt(int64(v), 10)}), true
	case int64:
		return ast.NewIntValue(&ast.IntValue{Value: strconv.FormatInt(v, 10)}), true
	case float32:
		return ast.NewFloatValue(&ast.FloatValue{Value: strconv.FormatFloat(float64(v), 'g', -1, 32)}), true
	case float64:
		if ttype == graphql.Int && v == float64(int64(v)) {
			return ast.NewIntValue(&ast.IntValue{Value: strconv.FormatInt(int64(v), 10)}), true
		}
		return ast.NewFloatValue(&ast.FloatValue{Value: strconv.FormatFloat(v, 'g', -1, 64)}), true
	case string:
		return ast.NewStringValue(&ast.StringValue{Value: v}), true
	}
	return nil, false
}

// inputTypeFromAST resolves a written type — `[EchoInput!]!` — against the
// schema. The library has this too, and it is unexported as well.
func inputTypeFromAST(schema *graphql.Schema, written ast.Type) graphql.Input {
	switch t := written.(type) {
	case *ast.NonNull:
		inner := inputTypeFromAST(schema, t.Type)
		if inner == nil {
			return nil
		}
		return graphql.NewNonNull(inner)
	case *ast.List:
		inner := inputTypeFromAST(schema, t.Type)
		if inner == nil {
			return nil
		}
		return graphql.NewList(inner)
	case *ast.Named:
		if t.Name == nil {
			return nil
		}
		named, ok := schema.Type(t.Name.Value).(graphql.Input)
		if !ok {
			return nil
		}
		return named
	}
	return nil
}

// sortedInputFields keeps a rendered default stable: a map walked in Go's
// order would write the same request differently on every call.
func sortedInputFields(obj *graphql.InputObject) []string {
	fields := obj.Fields()
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func objectField(name string, value ast.Value) *ast.ObjectField {
	return ast.NewObjectField(&ast.ObjectField{
		Name:  ast.NewName(&ast.Name{Value: name}),
		Value: value,
	})
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
