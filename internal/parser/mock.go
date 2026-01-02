package parser

import (
	"fmt"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/matutetandil/mycel/internal/mock"
	"github.com/zclconf/go-cty/cty"
)

// parseMockConfig parses a mocks block from environment config.
func parseMockConfig(block *hcl.Block) (*mock.Config, error) {
	config := &mock.Config{
		Connectors: make(map[string]*mock.ConnectorMockConfig),
	}

	content, _, diags := block.Body.PartialContent(&hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "enabled"},
			{Name: "path"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "connectors"},
		},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing mocks block: %s", diags.Error())
	}

	// Parse enabled
	if attr, ok := content.Attributes["enabled"]; ok {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() && val.Type() == cty.Bool {
			config.Enabled = val.True()
		}
	}

	// Parse path
	if attr, ok := content.Attributes["path"]; ok {
		val, diags := attr.Expr.Value(nil)
		if !diags.HasErrors() && val.Type() == cty.String {
			config.Path = val.AsString()
		}
	}

	// Parse per-connector settings
	for _, block := range content.Blocks {
		if block.Type == "connectors" {
			if err := parseConnectorMocks(block, config); err != nil {
				return nil, err
			}
		}
	}

	return config, nil
}

// parseConnectorMocks parses per-connector mock settings.
func parseConnectorMocks(block *hcl.Block, config *mock.Config) error {
	attrs, diags := block.Body.JustAttributes()
	if diags.HasErrors() {
		return fmt.Errorf("parsing connectors block: %s", diags.Error())
	}

	for name, attr := range attrs {
		val, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			continue
		}

		if val.Type().IsObjectType() {
			connConfig := &mock.ConnectorMockConfig{}

			if v := val.GetAttr("latency"); !v.IsNull() && v.Type() == cty.String {
				if d, err := time.ParseDuration(v.AsString()); err == nil {
					connConfig.Latency = d
				}
			}

			if v := val.GetAttr("fail_rate"); !v.IsNull() && v.Type() == cty.Number {
				n, _ := v.AsBigFloat().Int64()
				connConfig.FailRate = int(n)
			}

			if v := val.GetAttr("enabled"); !v.IsNull() && v.Type() == cty.Bool {
				enabled := v.True()
				connConfig.Enabled = &enabled
			}

			config.Connectors[name] = connConfig
		}
	}

	return nil
}

// ParseMockFlags parses CLI mock flags into config.
func ParseMockFlags(mockFlag, noMockFlag string, config *mock.Config) {
	if config == nil {
		return
	}

	// Parse --mock flag
	if mockFlag != "" {
		if mockFlag == "all" {
			config.Enabled = true
		} else {
			config.Enabled = true
			config.MockOnly = splitComma(mockFlag)
		}
	}

	// Parse --no-mock flag
	if noMockFlag != "" {
		if noMockFlag == "all" {
			config.Enabled = false
		} else {
			config.NoMock = splitComma(noMockFlag)
		}
	}
}

// splitComma splits a comma-separated string into a slice.
func splitComma(s string) []string {
	if s == "" {
		return nil
	}

	var result []string
	current := ""
	for _, c := range s {
		if c == ',' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else if c != ' ' {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
