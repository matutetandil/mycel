package openapi

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/parser"
	"github.com/matutetandil/mycel/internal/validate"
	"gopkg.in/yaml.v3"
)

// Generator generates OpenAPI specifications from Mycel configuration.
type Generator struct {
	config  *parser.Configuration
	types   map[string]*validate.TypeSchema
	baseURL string
}

// NewGenerator creates a new OpenAPI generator.
func NewGenerator(config *parser.Configuration) *Generator {
	types := make(map[string]*validate.TypeSchema)
	for _, t := range config.Types {
		types[t.Name] = t
	}

	return &Generator{
		config: config,
		types:  types,
	}
}

// SetBaseURL sets the base URL for the API server.
func (g *Generator) SetBaseURL(url string) {
	g.baseURL = url
}

// Generate creates an OpenAPI 3.0 specification.
func (g *Generator) Generate() (*Spec, error) {
	spec := &Spec{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:   "Mycel API",
			Version: "1.0.0",
		},
		Paths: make(map[string]PathItem),
		Components: &Components{
			Schemas: make(map[string]*Schema),
		},
	}

	// Set service info
	if g.config.ServiceConfig != nil {
		if g.config.ServiceConfig.Name != "" {
			spec.Info.Title = g.config.ServiceConfig.Name + " API"
		}
		if g.config.ServiceConfig.Version != "" {
			spec.Info.Version = g.config.ServiceConfig.Version
		}
	}

	// Set server URL
	if g.baseURL != "" {
		spec.Servers = []Server{{URL: g.baseURL}}
	} else {
		// Try to get port from REST connector
		for _, conn := range g.config.Connectors {
			if conn.Type == "rest" {
				if port, ok := conn.Properties["port"].(int); ok {
					spec.Servers = []Server{{
						URL:         fmt.Sprintf("http://localhost:%d", port),
						Description: "Local development server",
					}}
					break
				}
			}
		}
	}

	// Generate paths from flows
	tags := make(map[string]bool)
	for _, f := range g.config.Flows {
		if err := g.addFlowToSpec(spec, f, tags); err != nil {
			return nil, fmt.Errorf("processing flow %s: %w", f.Name, err)
		}
	}

	// Add tags
	for tag := range tags {
		spec.Tags = append(spec.Tags, Tag{Name: tag})
	}

	// Generate component schemas from types
	for _, t := range g.config.Types {
		schema := g.typeToSchema(t)
		spec.Components.Schemas[t.Name] = schema
	}

	return spec, nil
}

// addFlowToSpec adds a flow as an OpenAPI path operation.
func (g *Generator) addFlowToSpec(spec *Spec, f *flow.Config, tags map[string]bool) error {
	// Only process REST flows
	if f.From == nil || f.From.GetOperation() == "" {
		return nil
	}

	// Parse operation (e.g., "GET /users/:id")
	method, path, err := parseOperation(f.From.GetOperation())
	if err != nil {
		return nil // Skip non-REST operations
	}

	// Convert path params from :id to {id}
	openAPIPath := convertPathParams(path)

	// Get or create path item
	pathItem, ok := spec.Paths[openAPIPath]
	if !ok {
		pathItem = PathItem{}
	}

	// Create operation
	op := &Operation{
		OperationID: f.Name,
		Summary:     formatSummary(f.Name),
		Responses: map[string]Response{
			"200": {
				Description: "Successful response",
				Content: map[string]MediaType{
					"application/json": {
						Schema: g.inferResponseSchema(f),
					},
				},
			},
		},
	}

	// Add tag from path
	tag := extractTag(path)
	if tag != "" {
		op.Tags = []string{tag}
		tags[tag] = true
	}

	// Add path parameters
	params := extractPathParams(path)
	for _, param := range params {
		op.Parameters = append(op.Parameters, Parameter{
			Name:     param,
			In:       "path",
			Required: true,
			Schema:   &Schema{Type: "string"},
		})
	}

	// Add request body for POST/PUT/PATCH
	if method == "POST" || method == "PUT" || method == "PATCH" {
		op.RequestBody = &RequestBody{
			Required: true,
			Content: map[string]MediaType{
				"application/json": {
					Schema: g.inferRequestSchema(f),
				},
			},
		}
	}

	// Set operation on path item
	switch method {
	case "GET":
		pathItem.Get = op
	case "POST":
		pathItem.Post = op
	case "PUT":
		pathItem.Put = op
	case "DELETE":
		pathItem.Delete = op
	case "PATCH":
		pathItem.Patch = op
	}

	spec.Paths[openAPIPath] = pathItem
	return nil
}

// parseOperation parses "GET /path" into method and path.
func parseOperation(op string) (method, path string, err error) {
	parts := strings.SplitN(strings.TrimSpace(op), " ", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid operation format: %s", op)
	}

	method = strings.ToUpper(parts[0])
	path = parts[1]

	// Validate HTTP method
	switch method {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS":
		return method, path, nil
	default:
		return "", "", fmt.Errorf("unknown HTTP method: %s", method)
	}
}

// convertPathParams converts :id to {id} for OpenAPI.
func convertPathParams(path string) string {
	re := regexp.MustCompile(`:(\w+)`)
	return re.ReplaceAllString(path, "{$1}")
}

// extractPathParams extracts parameter names from a path.
func extractPathParams(path string) []string {
	re := regexp.MustCompile(`:(\w+)`)
	matches := re.FindAllStringSubmatch(path, -1)
	params := make([]string, 0, len(matches))
	for _, m := range matches {
		params = append(params, m[1])
	}
	return params
}

// extractTag extracts a tag name from the path (first segment).
func extractTag(path string) string {
	path = strings.TrimPrefix(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return ""
}

// formatSummary formats a flow name into a readable summary.
func formatSummary(name string) string {
	// Replace underscores with spaces and capitalize
	words := strings.Split(name, "_")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

// inferResponseSchema infers response schema from flow configuration.
func (g *Generator) inferResponseSchema(f *flow.Config) *Schema {
	// Check if flow has a returns type
	if f.Returns != "" {
		if _, ok := g.types[f.Returns]; ok {
			return &Schema{
				Type: "array",
				Items: &Schema{
					Ref: "#/components/schemas/" + f.Returns,
				},
			}
		}
	}

	// Default to generic object
	return &Schema{Type: "object"}
}

// inferRequestSchema infers request schema from flow configuration.
func (g *Generator) inferRequestSchema(f *flow.Config) *Schema {
	// Check if flow has a validation schema
	if f.Validate != nil && f.Validate.Input != "" {
		if _, ok := g.types[f.Validate.Input]; ok {
			return &Schema{
				Ref: "#/components/schemas/" + f.Validate.Input,
			}
		}
	}

	// Default to generic object
	return &Schema{Type: "object"}
}

// typeToSchema converts a Mycel type schema to OpenAPI schema.
func (g *Generator) typeToSchema(t *validate.TypeSchema) *Schema {
	schema := &Schema{
		Type:       "object",
		Properties: make(map[string]*Schema),
	}

	for _, field := range t.Fields {
		propSchema := g.fieldToSchema(&field)
		schema.Properties[field.Name] = propSchema

		if field.Required {
			schema.Required = append(schema.Required, field.Name)
		}
	}

	return schema
}

// fieldToSchema converts a field schema to OpenAPI schema.
func (g *Generator) fieldToSchema(f *validate.FieldSchema) *Schema {
	schema := &Schema{}

	switch f.Type {
	case "string":
		schema.Type = "string"
		// Extract constraints
		for _, c := range f.Constraints {
			g.applyConstraintToSchema(c, schema)
		}
	case "number":
		schema.Type = "number"
		for _, c := range f.Constraints {
			g.applyConstraintToSchema(c, schema)
		}
	case "integer":
		schema.Type = "integer"
		for _, c := range f.Constraints {
			g.applyConstraintToSchema(c, schema)
		}
	case "boolean":
		schema.Type = "boolean"
	case "array":
		schema.Type = "array"
		schema.Items = &Schema{Type: "object"}
	case "object":
		schema.Type = "object"
	default:
		// Could be a reference to another type
		if _, ok := g.types[f.Type]; ok {
			schema.Ref = "#/components/schemas/" + f.Type
		} else {
			schema.Type = "string"
		}
	}

	return schema
}

// applyConstraintToSchema applies a constraint to an OpenAPI schema.
func (g *Generator) applyConstraintToSchema(c validate.Constraint, schema *Schema) {
	// Extract constraint details based on name
	switch c.Name() {
	case "format":
		// Would need to access constraint value
	case "min":
		// Would need to access constraint value
	case "max":
		// Would need to access constraint value
	case "pattern":
		// Would need to access constraint value
	}
}

// ToJSON serializes the spec to JSON.
func (s *Spec) ToJSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// ToYAML serializes the spec to YAML.
func (s *Spec) ToYAML() ([]byte, error) {
	return yaml.Marshal(s)
}
