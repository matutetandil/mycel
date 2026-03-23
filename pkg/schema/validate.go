package schema

import "fmt"

// ValidateParams validates parameters against a block schema.
// - If an attribute has a Default and is missing, the default is applied.
// - If an attribute is Required and has no Default, an error is returned if missing.
func ValidateParams(block *Block, params map[string]interface{}) error {
	if block == nil {
		return nil
	}

	for _, attr := range block.Attrs {
		_, exists := params[attr.Name]

		if !exists {
			if attr.Default != nil {
				// Apply default value
				params[attr.Name] = attr.Default
			} else if attr.Required {
				return fmt.Errorf("'%s' is required (%s)", attr.Name, attr.Doc)
			}
		}
	}

	return nil
}
