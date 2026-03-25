package ide

import "fmt"

// CEL completion support for transform blocks, filter expressions, and accept conditions.

// celFunctions returns completion items for all built-in CEL functions.
func celFunctions() []CompletionItem {
	fns := []struct {
		name, sig, doc string
	}{
		{"uuid", "uuid()", "Generate a new UUID v4"},
		{"now", "now()", "Current timestamp (RFC3339)"},
		{"lower", "lower(s)", "Convert string to lowercase"},
		{"upper", "upper(s)", "Convert string to uppercase"},
		{"trim", "trim(s)", "Remove leading and trailing whitespace"},
		{"replace", "replace(s, old, new)", "Replace occurrences of old with new"},
		{"split", "split(s, sep)", "Split string by separator"},
		{"join", "join(list, sep)", "Join list elements with separator"},
		{"contains", "contains(s, substr)", "Check if string contains substring"},
		{"starts_with", "starts_with(s, prefix)", "Check if string starts with prefix"},
		{"ends_with", "ends_with(s, suffix)", "Check if string ends with suffix"},
		{"len", "len(s)", "Length of string or list"},
		{"int", "int(v)", "Convert to integer"},
		{"double", "double(v)", "Convert to double/float"},
		{"string", "string(v)", "Convert to string"},
		{"has", "has(field)", "Check if field exists"},
		{"size", "size(v)", "Size of string, list, or map"},
		{"matches", "matches(s, pattern)", "Regex match"},
		{"timestamp", "timestamp(s)", "Parse string as timestamp"},
		{"duration", "duration(s)", "Parse string as duration"},
		{"base64_encode", "base64_encode(s)", "Encode string as base64"},
		{"base64_decode", "base64_decode(s)", "Decode base64 string"},
		{"json_encode", "json_encode(v)", "Encode value as JSON string"},
		{"json_decode", "json_decode(s)", "Decode JSON string to value"},
		{"hash_md5", "hash_md5(s)", "MD5 hash of string"},
		{"hash_sha256", "hash_sha256(s)", "SHA-256 hash of string"},
		{"coalesce", "coalesce(a, b)", "Return first non-null value"},
		{"default", "default(v, fallback)", "Return fallback if v is null"},
		{"has_field", "has_field(field)", "Check if field was requested (GraphQL)"},
		{"field_requested", "field_requested(field)", "Check if field was requested (GraphQL)"},
		{"first", "first(list)", "First element of a list"},
		{"last", "last(list)", "Last element of a list"},
		{"unique", "unique(list)", "Remove duplicates from list"},
		{"pluck", "pluck(list, field)", "Extract field from each item in list"},
		{"sort_by", "sort_by(list, field)", "Sort list by field"},
		{"sum", "sum(list)", "Sum of numeric list"},
		{"avg", "avg(list)", "Average of numeric list"},
		{"min", "min(a, b)", "Minimum of two values"},
		{"max", "max(a, b)", "Maximum of two values"},
	}

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
