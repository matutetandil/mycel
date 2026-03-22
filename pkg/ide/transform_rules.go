package ide

// TransformRule represents an ordered transform rule for breakpoint placement.
type TransformRule struct {
	Index      int    `json:"index"`
	Target     string `json:"target"`     // output field name
	Expression string `json:"expression"` // CEL expression
	Stage      string `json:"stage"`      // "transform", "enrich", "response"
	Range      Range  `json:"range"`      // source position
}

// TransformRules returns the ordered transform rules for a flow.
// This is the metadata Studio needs for per-rule breakpoint placement.
func (e *Engine) TransformRules(flowName string) []TransformRule {
	e.index.mu.RLock()
	defer e.index.mu.RUnlock()

	flowEntity := e.index.Flows[flowName]
	if flowEntity == nil {
		return nil
	}

	// Find the flow block
	fi, ok := e.index.Files[flowEntity.File]
	if !ok {
		return nil
	}

	var flowBlock *Block
	for _, b := range fi.Blocks {
		if b.Type == "flow" && b.Name == flowName {
			flowBlock = b
			break
		}
	}
	if flowBlock == nil {
		return nil
	}

	var rules []TransformRule
	index := 0

	// Collect rules from transform block
	for _, child := range flowBlock.Children {
		if child.Type == "transform" {
			for _, attr := range child.Attrs {
				if attr.Name == "use" {
					continue // "use" is not a rule
				}
				rules = append(rules, TransformRule{
					Index:      index,
					Target:     attr.Name,
					Expression: attr.ValueRaw,
					Stage:      "transform",
					Range:      attr.Range,
				})
				index++
			}
		}
	}

	// Collect rules from response block
	for _, child := range flowBlock.Children {
		if child.Type == "response" {
			for _, attr := range child.Attrs {
				rules = append(rules, TransformRule{
					Index:      index,
					Target:     attr.Name,
					Expression: attr.ValueRaw,
					Stage:      "response",
					Range:      attr.Range,
				})
				index++
			}
		}
	}

	return rules
}

// FlowStages returns the pipeline stages present in a flow, in execution order.
// This is used for breakpoint placement and visualization.
func (e *Engine) FlowStages(flowName string) []string {
	e.index.mu.RLock()
	defer e.index.mu.RUnlock()

	flowEntity := e.index.Flows[flowName]
	if flowEntity == nil {
		return nil
	}

	fi, ok := e.index.Files[flowEntity.File]
	if !ok {
		return nil
	}

	var flowBlock *Block
	for _, b := range fi.Blocks {
		if b.Type == "flow" && b.Name == flowName {
			flowBlock = b
			break
		}
	}
	if flowBlock == nil {
		return nil
	}

	stages := []string{"input", "sanitize"}

	hasBlock := make(map[string]bool)
	for _, child := range flowBlock.Children {
		hasBlock[child.Type] = true
	}

	// Check from block for filter
	if hasBlock["from"] {
		for _, child := range flowBlock.Children {
			if child.Type == "from" {
				if child.GetAttr("filter") != "" || hasChildBlock(child, "filter") {
					stages = append(stages, "filter")
				}
				break
			}
		}
	}

	if hasBlock["accept"] {
		stages = append(stages, "accept")
	}
	if hasBlock["dedupe"] {
		stages = append(stages, "dedupe")
	}

	// Validate input
	for _, child := range flowBlock.Children {
		if child.Type == "validate" && child.GetAttr("input") != "" {
			stages = append(stages, "validate_input")
			break
		}
	}

	if hasBlock["enrich"] {
		stages = append(stages, "enrich")
	}
	if hasBlock["step"] {
		stages = append(stages, "step")
	}
	if hasBlock["transform"] {
		stages = append(stages, "transform")
	}

	// Validate output
	for _, child := range flowBlock.Children {
		if child.Type == "validate" && child.GetAttr("output") != "" {
			stages = append(stages, "validate_output")
			break
		}
	}

	if hasBlock["to"] {
		stages = append(stages, "write")
	}

	if hasBlock["response"] {
		stages = append(stages, "response")
	}

	return stages
}

func hasChildBlock(b *Block, blockType string) bool {
	for _, child := range b.Children {
		if child.Type == blockType {
			return true
		}
	}
	return false
}
