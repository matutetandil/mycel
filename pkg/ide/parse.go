package ide

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// parseHCL parses HCL content permissively, collecting blocks and attributes
// with position info. Unlike the runtime parser, this never evaluates expressions
// and tolerates incomplete files.
func parseHCL(path string, src []byte) *FileIndex {
	fi := &FileIndex{
		Path: path,
	}

	file, diags := hclsyntax.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		for _, d := range diags {
			if d.Severity == hcl.DiagError {
				fi.ParseDiags = append(fi.ParseDiags, hclDiagToIDE(d, path))
			}
		}
	}

	if file == nil || file.Body == nil {
		return fi
	}

	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return fi
	}

	fi.Blocks = parseBody(body)
	return fi
}

// parseBody extracts blocks from an hclsyntax.Body.
func parseBody(body *hclsyntax.Body) []*Block {
	var blocks []*Block

	for _, b := range body.Blocks {
		// Use the full range (header + body) for cursor-in-block detection.
		// DefRange only covers the header line; we need the closing brace too.
		fullRange := b.DefRange()
		if b.Body != nil {
			fullRange.End = b.Body.EndRange.End
		}

		block := &Block{
			Type:  b.Type,
			Range: hclRangeToIDE(fullRange),
		}

		if len(b.Labels) > 0 {
			block.Name = b.Labels[0]
		}

		if b.Body != nil {
			block.Attrs = parseAttrs(b.Body)
			block.Children = parseBody(b.Body)
		}

		blocks = append(blocks, block)
	}

	return blocks
}

// parseAttrs extracts attributes from an hclsyntax.Body, sorted by source position.
// HCL stores attributes in a map (unordered), so we sort by line number to preserve
// the declaration order — critical for transform rule indexing and breakpoint placement.
func parseAttrs(body *hclsyntax.Body) []*Attribute {
	var attrs []*Attribute

	for _, a := range body.Attributes {
		attr := &Attribute{
			Name:     a.Name,
			Range:    hclRangeToIDE(a.SrcRange),
			ValRange: hclRangeToIDE(a.Expr.Range()),
			ValueRaw: extractRawValue(a.Expr),
		}
		attrs = append(attrs, attr)
	}

	// Sort by source position to preserve declaration order
	sort.Slice(attrs, func(i, j int) bool {
		if attrs[i].Range.Start.Line != attrs[j].Range.Start.Line {
			return attrs[i].Range.Start.Line < attrs[j].Range.Start.Line
		}
		return attrs[i].Range.Start.Col < attrs[j].Range.Start.Col
	})

	return attrs
}

// extractRawValue extracts the literal string value from an expression
// without evaluating it. Returns empty string for non-literal expressions.
func extractRawValue(expr hclsyntax.Expression) string {
	switch e := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		v := e.Val
		if v.Type() == cty.Bool {
			return fmt.Sprintf("%v", v.True())
		}
		if v.Type() == cty.Number {
			bf := v.AsBigFloat()
			return bf.Text('f', -1)
		}
		if v.Type() == cty.String {
			return v.AsString()
		}
		return ""
	case *hclsyntax.TemplateExpr:
		// Simple string literals are wrapped in a TemplateExpr with one part
		if len(e.Parts) == 1 {
			if lit, ok := e.Parts[0].(*hclsyntax.LiteralValueExpr); ok {
				if lit.Val.Type() == cty.String {
					return lit.Val.AsString()
				}
			}
		}
		return ""
	default:
		return ""
	}
}

// hclRangeToIDE converts an HCL range to an IDE range.
func hclRangeToIDE(r hcl.Range) Range {
	return Range{
		Start: Position{
			Line:   r.Start.Line,
			Col:    r.Start.Column,
			Offset: r.Start.Byte,
		},
		End: Position{
			Line:   r.End.Line,
			Col:    r.End.Column,
			Offset: r.End.Byte,
		},
	}
}

// hclDiagToIDE converts an HCL diagnostic to an IDE diagnostic.
func hclDiagToIDE(d *hcl.Diagnostic, path string) *Diagnostic {
	diag := &Diagnostic{
		Message: d.Summary,
		File:    path,
	}
	if d.Detail != "" {
		diag.Message = d.Summary + ": " + d.Detail
	}

	switch d.Severity {
	case hcl.DiagError:
		diag.Severity = SeverityError
	case hcl.DiagWarning:
		diag.Severity = SeverityWarning
	default:
		diag.Severity = SeverityInfo
	}

	if d.Subject != nil {
		diag.Range = hclRangeToIDE(*d.Subject)
	}

	return diag
}

// stripQuotes removes surrounding quotes from a string if present.
func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
