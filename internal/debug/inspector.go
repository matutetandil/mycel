package debug

import (
	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/transform"
	"github.com/matutetandil/mycel/internal/validate"
)

// RuntimeInspector provides read-only access to runtime configuration.
// Implemented by Runtime to keep the debug package decoupled.
type RuntimeInspector interface {
	ListFlows() []string
	GetFlowConfig(name string) (*flow.Config, bool)
	ListConnectors() []string
	GetConnectorConfig(name string) (*connector.Config, bool)
	ListTypes() []*validate.TypeSchema
	ListTransforms() []*transform.Config
	GetCELTransformer() *transform.CELTransformer
}

// buildFlowInfo converts a flow config into an IDE-friendly representation.
func buildFlowInfo(cfg *flow.Config) *FlowInfo {
	info := &FlowInfo{
		Name: cfg.Name,
	}

	if cfg.From != nil {
		info.From = &FlowEndpoint{
			Connector: cfg.From.Connector,
			Operation: cfg.From.GetOperation(),
		}
	}

	if cfg.To != nil {
		info.To = &FlowEndpoint{
			Connector: cfg.To.Connector,
			Operation: cfg.To.GetOperation(),
		}
	}

	if cfg.Steps != nil {
		info.HasSteps = true
		info.StepCount = len(cfg.Steps)
	}

	if cfg.Transform != nil && cfg.Transform.Mappings != nil {
		info.Transform = cfg.Transform.Mappings
	}

	info.Response = cfg.Response

	if cfg.Validate != nil {
		info.Validate = &ValidateInfo{}
		if cfg.Validate.Input != "" {
			info.Validate.Input = cfg.Validate.Input
		}
		if cfg.Validate.Output != "" {
			info.Validate.Output = cfg.Validate.Output
		}
	}

	info.HasCache = cfg.Cache != nil
	info.HasRetry = cfg.ErrorHandling != nil

	return info
}

// buildConnectorInfo converts a connector config into an IDE-friendly representation.
func buildConnectorInfo(cfg *connector.Config) *ConnectorInfo {
	return &ConnectorInfo{
		Name:   cfg.Name,
		Type:   cfg.Type,
		Driver: cfg.Driver,
	}
}

// buildTypeInfo converts a type schema into an IDE-friendly representation.
func buildTypeInfo(schema *validate.TypeSchema) *TypeInfo {
	info := &TypeInfo{
		Name:   schema.Name,
		Fields: make([]FieldInfo, len(schema.Fields)),
	}
	for i, f := range schema.Fields {
		info.Fields[i] = FieldInfo{
			Name:     f.Name,
			Type:     f.Type,
			Required: f.Required,
		}
	}
	return info
}

// buildTransformInfo converts a transform config into an IDE-friendly representation.
func buildTransformInfo(cfg *transform.Config) *TransformInfo {
	return &TransformInfo{
		Name:     cfg.Name,
		Mappings: cfg.Mappings,
	}
}
