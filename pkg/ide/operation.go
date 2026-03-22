package ide

import (
	"fmt"
	"strings"
)

// Operation string parsing and validation for REST operations like "GET /users/:id".

var validHTTPMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// validateOperation checks REST operation strings for common issues.
func validateOperation(path string, attr *Attribute, connType string) []*Diagnostic {
	if attr.ValueRaw == "" {
		return nil
	}

	// Only validate REST-style operations
	if connType != "rest" && connType != "" {
		return nil
	}

	op := attr.ValueRaw
	parts := strings.SplitN(op, " ", 2)

	if len(parts) != 2 {
		// Not a REST operation (could be a queue name, topic, etc.)
		return nil
	}

	method := strings.ToUpper(parts[0])
	if !contains(validHTTPMethods, method) {
		return []*Diagnostic{{
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("unknown HTTP method %q (valid: %s)", parts[0], strings.Join(validHTTPMethods, ", ")),
			File:     path,
			Range:    attr.ValRange,
		}}
	}

	urlPath := parts[1]
	if !strings.HasPrefix(urlPath, "/") {
		return []*Diagnostic{{
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("operation path should start with / (got %q)", urlPath),
			File:     path,
			Range:    attr.ValRange,
		}}
	}

	return nil
}

// completeOperation returns completion items for operation values
// based on the connector type.
func completeOperation(connType string) []CompletionItem {
	switch connType {
	case "rest":
		return []CompletionItem{
			{Label: "GET /", Kind: CompletionValue, Detail: "Read endpoint", InsertText: `"GET /"`},
			{Label: "POST /", Kind: CompletionValue, Detail: "Create endpoint", InsertText: `"POST /"`},
			{Label: "PUT /", Kind: CompletionValue, Detail: "Update endpoint", InsertText: `"PUT /"`},
			{Label: "PATCH /", Kind: CompletionValue, Detail: "Partial update endpoint", InsertText: `"PATCH /"`},
			{Label: "DELETE /", Kind: CompletionValue, Detail: "Delete endpoint", InsertText: `"DELETE /"`},
		}
	case "graphql":
		return []CompletionItem{
			{Label: "Query.", Kind: CompletionValue, Detail: "GraphQL query", InsertText: `"Query."`},
			{Label: "Mutation.", Kind: CompletionValue, Detail: "GraphQL mutation", InsertText: `"Mutation."`},
			{Label: "Subscription.", Kind: CompletionValue, Detail: "GraphQL subscription", InsertText: `"Subscription."`},
		}
	case "grpc":
		return []CompletionItem{
			{Label: "ServiceName/MethodName", Kind: CompletionValue, Detail: "gRPC method", InsertText: `"/"`},
		}
	}
	return nil
}
