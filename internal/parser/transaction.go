package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/matutetandil/mycel/internal/flow"
)

// parseTransactionBlock parses a `to { transaction { } }` block into an ordered
// list of statements. Order is significant (captured values flow forward), so
// it iterates the native hclsyntax body's Blocks directly rather than going
// through a schema, which would not preserve textual order across a
// heterogeneous exec/each mix.
func parseTransactionBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.TransactionConfig, error) {
	stmts, err := parseTxStatements(block.Body, ctx)
	if err != nil {
		return nil, err
	}
	if len(stmts) == 0 {
		return nil, fmt.Errorf("transaction block must contain at least one exec or each")
	}
	return &flow.TransactionConfig{Statements: stmts}, nil
}

// parseTxStatements reads the ordered exec/each children of a transaction (or
// of an each body). It requires the native hclsyntax body so block order is
// preserved.
func parseTxStatements(body hcl.Body, ctx *hcl.EvalContext) ([]flow.TxStatement, error) {
	syntaxBody, ok := body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("transaction block must use native HCL syntax")
	}

	var stmts []flow.TxStatement
	for _, nested := range syntaxBody.Blocks {
		switch nested.Type {
		case "exec":
			ex, err := parseTxExecBlock(nested.AsHCLBlock(), ctx)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, flow.TxStatement{Exec: ex})
		case "each":
			each, err := parseTxEachBlock(nested.AsHCLBlock(), ctx)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, flow.TxStatement{Each: each})
		default:
			return nil, fmt.Errorf("unsupported block %q inside transaction (expected exec or each)", nested.Type)
		}
	}
	return stmts, nil
}

// parseTxExecBlock parses a single `exec { }` statement.
func parseTxExecBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.TxExec, error) {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return nil, fmt.Errorf("exec block error: %s (exec takes attributes only: query, params, when, capture)", diags.Error())
	}

	ex := &flow.TxExec{Params: map[string]string{}}

	for name, attr := range attrs {
		switch name {
		case "query":
			val, d := attr.Expr.Value(ctx)
			if d.HasErrors() {
				return nil, fmt.Errorf("exec query error: %s", d.Error())
			}
			ex.Query = val.AsString()
		case "capture":
			val, d := attr.Expr.Value(ctx)
			if d.HasErrors() {
				return nil, fmt.Errorf("exec capture error: %s", d.Error())
			}
			ex.Capture = val.AsString()
		case "when":
			// CEL gate: prefer the evaluated string literal, fall back to the
			// raw expression text when it references runtime variables.
			val, d := attr.Expr.Value(ctx)
			if d.HasErrors() {
				ex.When = extractExpressionText(attr.Expr)
			} else {
				ex.When = val.AsString()
			}
		case "params":
			params, err := parseTxParams(attr, ctx)
			if err != nil {
				return nil, err
			}
			ex.Params = params
		default:
			return nil, fmt.Errorf("unsupported attribute %q in exec (allowed: query, params, when, capture)", name)
		}
	}

	if ex.Query == "" {
		return nil, fmt.Errorf("exec block requires a non-empty query")
	}
	return ex, nil
}

// parseTxEachBlock parses an `each "<var>" in "<listExpr>" { }` block. In native
// HCL the header is three labels — var, the literal keyword "in", and the list
// expression — which keeps the syntax readable while staying valid HCL.
func parseTxEachBlock(block *hcl.Block, ctx *hcl.EvalContext) (*flow.TxEach, error) {
	if len(block.Labels) != 3 || block.Labels[1] != "in" {
		return nil, fmt.Errorf(`each block must be written as: each "<var>" in "<listExpr>" { ... }`)
	}
	varName := block.Labels[0]
	listExpr := block.Labels[2]
	if varName == "" {
		return nil, fmt.Errorf("each block requires a non-empty loop variable name")
	}
	if listExpr == "" {
		return nil, fmt.Errorf("each %q requires a non-empty list expression", varName)
	}

	body, err := parseTxStatements(block.Body, ctx)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("each %q must contain at least one exec or each", varName)
	}

	return &flow.TxEach{Var: varName, In: listExpr, Body: body}, nil
}

// parseTxParams reads an `params = { name = "<celExpr>" }` attribute into a
// name->expression map. Values are quoted CEL strings (same convention as
// transform expressions), so they evaluate to string literals here and are
// resolved against the transaction scope at runtime.
func parseTxParams(attr *hcl.Attribute, ctx *hcl.EvalContext) (map[string]string, error) {
	val, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() {
		return nil, fmt.Errorf("exec params error: %s (param values must be quoted CEL expressions, e.g. owner = \"output.owner_id\")", diags.Error())
	}
	if !val.Type().IsObjectType() && !val.Type().IsMapType() {
		return nil, fmt.Errorf("exec params must be an object, e.g. params = { owner = \"output.owner_id\" }")
	}

	params := make(map[string]string)
	for k, v := range val.AsValueMap() {
		if v.IsNull() {
			return nil, fmt.Errorf("exec param %q is null", k)
		}
		params[k] = v.AsString()
	}
	return params, nil
}
