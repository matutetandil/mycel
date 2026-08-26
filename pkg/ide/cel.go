package ide

import "fmt"

// CEL completion support for transform blocks, filter expressions, and accept conditions.

// celFunctions returns completion items for all built-in CEL functions.
// celFunction is one function offered in a transform, with a call that
// exercises it — so the list can be compiled rather than believed.
//
// Ten of the thirty-nine this used to offer did not exist: four were CEL's own
// string methods written as if they were calls, base64 and json helpers that
// were never implemented, an md5 that is not there, and min/max, which are
// min_val/max_val over a list. Somebody accepting one of those completions got
// a configuration that fails when the flow runs.
type celFunction struct {
	name, sig, doc, example string
}

// celFunctionList is every function offered, and the call each is checked with.
func celFunctionList() []celFunction {
	return []celFunction{
		{"uuid", "uuid()", "Generate a new UUID v4", `uuid()`},
		{"now", "now()", "Current timestamp (RFC3339)", `now()`},
		{"now_unix", "now_unix()", "Current time in seconds since the epoch", `now_unix()`},
		{"format_date", "format_date(t, layout)", "Format a timestamp: YYYY, MM, DD, HH, mm, ss", `format_date(now(), "YYYY-MM-DD")`},

		{"lower", "lower(s)", "Convert string to lowercase", `lower("A")`},
		{"upper", "upper(s)", "Convert string to uppercase", `upper("a")`},
		{"trim", "trim(s)", "Remove leading and trailing whitespace", `trim(" a ")`},
		{"replace", "replace(s, old, new)", "Replace occurrences of old with new", `replace("a", "a", "b")`},
		{"split", "split(s, sep)", "Split string by separator", `split("a,b", ",")`},
		{"join", "join(list, sep)", "Join list elements with separator", `join(["a"], ",")`},
		{"substring", "substring(s, start, end)", "Part of a string, by index", `substring("abcd", 1, 3)`},
		{"len", "len(v)", "Length of a string or list", `len("ab")`},

		// CEL's own string methods, which are written on the value rather
		// than called on it. They were offered here as starts_with(s, prefix)
		// and the like, which is not something this language has.
		{"startsWith", `"s".startsWith(prefix)`, "Whether a string starts with a prefix", `"abc".startsWith("a")`},
		{"endsWith", `"s".endsWith(suffix)`, "Whether a string ends with a suffix", `"abc".endsWith("c")`},
		{"contains", `"s".contains(substr)`, "Whether a string contains another", `"abc".contains("b")`},
		{"matches", "matches(s, pattern)", "Regular expression match", `matches("abc", "a.c")`},
		{"size", "size(v)", "Size of a string, list or map", `size("ab")`},

		{"int", "int(v)", "Convert to integer", `int("1")`},
		{"double", "double(v)", "Convert to double", `double(1)`},
		{"string", "string(v)", "Convert to string", `string(1)`},
		{"timestamp", "timestamp(s)", "Parse string as timestamp", `timestamp("2020-01-01T00:00:00Z")`},
		{"duration", "duration(s)", "Parse string as duration", `duration("5s")`},
		{"hash_sha256", "hash_sha256(s)", "SHA-256 hash, hex encoded (64 chars) — a fingerprint, not a password hash", `hash_sha256("a")`},

		{"coalesce", "coalesce(v, fallback)", "First value that is not null", `coalesce("", "x")`},
		{"default", "default(v, fallback)", "Fallback when v is null", `default("", "x")`},
		{"has_field", "has_field(m, key)", "Whether a map has a key", `has_field({"a": 1}, "a")`},

		{"first", "first(list)", "First element of a list", `first([1, 2])`},
		{"last", "last(list)", "Last element of a list", `last([1, 2])`},
		{"unique", "unique(list)", "Remove duplicates from a list", `unique([1, 1])`},
		{"reverse", "reverse(list)", "Reverse a list", `reverse([1, 2])`},
		{"flatten", "flatten(list)", "Flatten a list of lists", `flatten([[1], [2]])`},
		{"pluck", "pluck(list, field)", "Extract a field from each item", `pluck([{"k": 1}], "k")`},
		{"sort_by", "sort_by(list, field)", "Sort a list by a field", `sort_by([{"k": 1}], "k")`},
		{"sum", "sum(list)", "Sum of a numeric list", `sum([1, 2])`},
		{"avg", "avg(list)", "Average of a numeric list", `avg([1, 2])`},
		{"min_val", "min_val(list)", "Smallest value in a list", `min_val([1, 2])`},
		{"max_val", "max_val(list)", "Largest value in a list", `max_val([1, 2])`},

		{"merge", "merge(a, b)", "Two maps as one", `merge({"a": 1}, {"b": 2})`},
		{"pick", "pick(m, k1)", "A map with only the named keys", `pick({"a": 1}, "a")`},
		{"omit", "omit(m, k1)", "A map without the named keys", `omit({"a": 1}, "a")`},

		{"requested_fields", "requested_fields(input)", "Every field path a GraphQL query asked for", `requested_fields({"a": 1})`},
		{"requested_top_fields", "requested_top_fields(input)", "The top-level fields a GraphQL query asked for", `requested_top_fields({"a": 1})`},
		{"field_requested", "field_requested(input, name)", "Whether a GraphQL query asked for a field", `field_requested({"a": 1}, "a")`},
	}
}

func celFunctions() []CompletionItem {
	fns := celFunctionList()

	items := make([]CompletionItem, len(fns))
	for i, f := range fns {
		items[i] = CompletionItem{
			Label:      f.name,
			Kind:       CompletionValue,
			Detail:     f.sig,
			Doc:        f.doc,
			InsertText: f.name + "(",
		}
	}
	return items
}

// celVariables returns completion items for variables available in a given context.
// blockPath determines what variables are available (e.g., inside transform, accept, response).
func celVariables(blockPath []string, flowBlock *Block, idx *ProjectIndex) []CompletionItem {
	var items []CompletionItem

	// input.* is always available
	items = append(items, CompletionItem{
		Label:  "input",
		Kind:   CompletionValue,
		Detail: "Input data from the source connector",
		Doc:    "Access request body, params, query, headers via input.*",
	})

	lastBlock := ""
	if len(blockPath) > 0 {
		lastBlock = blockPath[len(blockPath)-1]
	}

	// output.* available in response block
	if lastBlock == "response" {
		items = append(items, CompletionItem{
			Label:  "output",
			Kind:   CompletionValue,
			Detail: "Output data from the destination connector",
			Doc:    "Access the result after writing to the destination",
		})
	}

	// step.<name>.* available in transform if flow has steps
	if flowBlock != nil {
		for _, child := range flowBlock.Children {
			if child.Type == "step" && child.Name != "" {
				items = append(items, CompletionItem{
					Label:      fmt.Sprintf("step.%s", child.Name),
					Kind:       CompletionValue,
					Detail:     fmt.Sprintf("Result from step %q", child.Name),
					Doc:        "Access data returned by this intermediate connector call",
					InsertText: fmt.Sprintf("step.%s.", child.Name),
				})
			}
		}

		// enriched.<name>.* available in transform if flow has enrichments
		for _, child := range flowBlock.Children {
			if child.Type == "enrich" && child.Name != "" {
				items = append(items, CompletionItem{
					Label:      fmt.Sprintf("enriched.%s", child.Name),
					Kind:       CompletionValue,
					Detail:     fmt.Sprintf("Result from enrichment %q", child.Name),
					Doc:        "Access data from this enrichment lookup",
					InsertText: fmt.Sprintf("enriched.%s.", child.Name),
				})
			}
		}
	}

	// Transform field suggestions — available in to block (as :field) and response block (as output.field)
	if flowBlock != nil && (lastBlock == "to" || lastBlock == "response") {
		fields := collectTransformFields(flowBlock, idx)
		for _, field := range fields {
			if lastBlock == "to" {
				items = append(items, CompletionItem{
					Label:      ":" + field,
					Kind:       CompletionValue,
					Detail:     "Transform output field",
					Doc:        fmt.Sprintf("Named param from transform field %q", field),
					InsertText: ":" + field,
				})
			} else if lastBlock == "response" {
				items = append(items, CompletionItem{
					Label:      "output." + field,
					Kind:       CompletionValue,
					Detail:     "Transform output field",
					Doc:        fmt.Sprintf("Output from transform field %q", field),
					InsertText: "output." + field,
				})
			}
		}
	}

	// Transform field suggestions within transform block itself (previous fields available)
	if flowBlock != nil && lastBlock == "transform" {
		fields := collectTransformFields(flowBlock, idx)
		for _, field := range fields {
			items = append(items, CompletionItem{
				Label:      field,
				Kind:       CompletionValue,
				Detail:     "Previously defined transform field",
				Doc:        fmt.Sprintf("Field %q defined earlier in this transform", field),
				InsertText: field,
			})
		}
	}

	// error.* available in on_error aspects
	if lastBlock == "action" && len(blockPath) >= 2 && blockPath[len(blockPath)-2] == "aspect" {
		items = append(items, CompletionItem{
			Label:  "error",
			Kind:   CompletionValue,
			Detail: "Error object (code, message, type)",
			Doc:    "Access error.code, error.message, error.type in on_error aspects",
		})
	}

	return items
}

// collectTransformFields returns the field names produced by the flow's transform block.
// If the transform uses `use = "name"`, resolves the named transform from the index.
func collectTransformFields(flowBlock *Block, idx *ProjectIndex) []string {
	var fields []string

	for _, child := range flowBlock.Children {
		if child.Type != "transform" {
			continue
		}

		// Check if it references a named transform
		useName := child.GetAttr("use")
		if useName != "" && idx != nil {
			// Resolve named transform from the project index
			idx.mu.RLock()
			for _, fi := range idx.Files {
				for _, b := range fi.Blocks {
					if b.Type == "transform" && b.Name == useName {
						for _, attr := range b.Attrs {
							fields = append(fields, attr.Name)
						}
					}
				}
			}
			idx.mu.RUnlock()
		}

		// Inline mappings
		for _, attr := range child.Attrs {
			if attr.Name != "use" {
				fields = append(fields, attr.Name)
			}
		}
	}

	return fields
}

// isCELContext returns true if the cursor is in a position where CEL completions are relevant.
func isCELContext(blockPath []string, attrName string) bool {
	if len(blockPath) == 0 {
		return false
	}

	lastBlock := blockPath[len(blockPath)-1]

	// Transform block — all attributes are CEL expressions (except "use")
	if lastBlock == "transform" && attrName != "use" {
		return true
	}

	// Response block — all attributes are CEL expressions
	if lastBlock == "response" {
		return true
	}

	// Filter/accept conditions
	if attrName == "filter" || attrName == "condition" || attrName == "when" || attrName == "if" {
		return true
	}

	return false
}
