package parser

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// The connector schema is what completions and `mycel add` are built from, and
// unlike the root blocks it is not covered by the parity test — which is how
// the gRPC connector came to advertise TLS attributes the parser refused.
//
// Named operations were missing from it entirely: the feature was parsed,
// documented and exampled, and tooling could not see it. Now that it is
// declared, this renders every attribute and child the schema claims and feeds
// it to the real parser, so the two cannot drift apart again.
func TestOperationSchemaMatchesTheParser(t *testing.T) {
	op := schema.OperationSchema()

	var b strings.Builder
	b.WriteString("connector \"api\" {\n  type = \"rest\"\n  port = 8080\n\n")
	b.WriteString(renderSchemaBlock(op, []string{"everything"}, 1))
	b.WriteString("}\n")

	doc := b.String()
	if _, err := tryParse(t, doc); err != nil {
		t.Fatalf("the operation schema describes something the parser rejects: %v\n\n%s", err, doc)
	}
}

// TestOperationSchemaCoversWhatTheParserAccepts is the other direction: an
// attribute the parser takes but the schema omits is a feature tooling cannot
// offer, which is exactly the state named operations were in.
func TestOperationSchemaCoversWhatTheParserAccepts(t *testing.T) {
	declared := map[string]bool{}
	op := schema.OperationSchema()
	for _, a := range op.Attrs {
		declared[a.Name] = true
	}

	// Taken from parseOperationBlock's body schema.
	for _, name := range []string{
		"description", "input", "output", "timeout",
		"method", "path", "query", "table",
		"operation_type", "field", "service", "rpc",
		"exchange", "routing_key", "queue",
		"protocol", "action", "path_pattern",
		"key_pattern", "ttl", "command", "args",
	} {
		if !declared[name] {
			t.Errorf("the parser accepts operation attribute %q and the schema does not offer it", name)
		}
	}

	var param *schema.Block
	for i := range op.Children {
		if op.Children[i].Type == "param" {
			param = &op.Children[i]
		}
	}
	if param == nil {
		t.Fatal("the operation schema declares no param block")
	}
	declaredParam := map[string]bool{}
	for _, a := range param.Attrs {
		declaredParam[a.Name] = true
	}
	for _, name := range []string{
		"type", "required", "default", "description", "in",
		"min", "max", "min_length", "max_length", "pattern", "enum",
	} {
		if !declaredParam[name] {
			t.Errorf("the parser accepts param attribute %q and the schema does not offer it", name)
		}
	}
}

// renderSchemaBlock writes a block exercising every attribute and child a
// schema declares.
func renderSchemaBlock(blk schema.Block, labels []string, depth int) string {
	var b strings.Builder
	indent := strings.Repeat("  ", depth)

	header := blk.Type
	for _, l := range labels {
		header += fmt.Sprintf(" %q", l)
	}
	fmt.Fprintf(&b, "%s%s {\n", indent, header)

	attrs := append([]schema.Attr(nil), blk.Attrs...)
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].Name < attrs[j].Name })
	for _, a := range attrs {
		fmt.Fprintf(&b, "%s  %s = %s\n", indent, a.Name, sampleValue(a))
	}

	for _, child := range blk.Children {
		childLabels := make([]string, child.Labels)
		for i := range childLabels {
			childLabels[i] = fmt.Sprintf("%s_%d", child.Type, i)
		}
		b.WriteString(renderSchemaBlock(child, childLabels, depth+1))
	}

	fmt.Fprintf(&b, "%s}\n", indent)
	return b.String()
}
