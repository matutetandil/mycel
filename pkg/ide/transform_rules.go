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

// BreakpointLocation represents a single position where a breakpoint can be set.
type BreakpointLocation struct {
	File      string `json:"file"`      // Source file path
	Line      int    `json:"line"`      // 1-based line number
	Flow      string `json:"flow"`      // Flow name
	Stage     string `json:"stage"`     // Pipeline stage (input, filter, accept, transform, write, etc.)
	RuleIndex int    `json:"ruleIndex"` // -1 for stage-level, 0+ for per-rule (transform/response)
	Label     string `json:"label"`     // Human-readable label (e.g., "filter", "transform: email = lower(input.email)")
}

// FlowBreakpoints returns all valid breakpoint locations for a flow.
// Studio uses this to show gutter breakpoint indicators on the exact lines
// where the user can set breakpoints.
func (e *Engine) FlowBreakpoints(flowName string) []BreakpointLocation {
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

	var bps []BreakpointLocation
	file := flowEntity.File

	// input — the flow block itself
	bps = append(bps, BreakpointLocation{
		File:      file,
		Line:      flowBlock.Range.Start.Line,
		Flow:      flowName,
		Stage:     "input",
		RuleIndex: -1,
		Label:     "input",
	})

	for _, child := range flowBlock.Children {
		switch child.Type {
		case "from":
			// filter — on the filter attribute or filter block
			if filterAttr := findAttr(child, "filter"); filterAttr != nil {
				bps = append(bps, BreakpointLocation{
					File:      file,
					Line:      filterAttr.Range.Start.Line,
					Flow:      flowName,
					Stage:     "filter",
					RuleIndex: -1,
					Label:     "filter",
				})
			}
			if filterBlock := findChildBlock(child, "filter"); filterBlock != nil {
				bps = append(bps, BreakpointLocation{
					File:      file,
					Line:      filterBlock.Range.Start.Line,
					Flow:      flowName,
					Stage:     "filter",
					RuleIndex: -1,
					Label:     "filter",
				})
			}

		case "accept":
			bps = append(bps, BreakpointLocation{
				File:      file,
				Line:      child.Range.Start.Line,
				Flow:      flowName,
				Stage:     "accept",
				RuleIndex: -1,
				Label:     "accept: " + child.GetAttr("when"),
			})

		case "validate":
			if child.HasAttr("input") {
				bps = append(bps, BreakpointLocation{
					File:      file,
					Line:      child.Range.Start.Line,
					Flow:      flowName,
					Stage:     "validate_input",
					RuleIndex: -1,
					Label:     "validate input",
				})
			}
			if child.HasAttr("output") {
				bps = append(bps, BreakpointLocation{
					File:      file,
					Line:      child.Range.Start.Line,
					Flow:      flowName,
					Stage:     "validate_output",
					RuleIndex: -1,
					Label:     "validate output",
				})
			}

		case "step":
			bps = append(bps, BreakpointLocation{
				File:      file,
				Line:      child.Range.Start.Line,
				Flow:      flowName,
				Stage:     "step",
				RuleIndex: -1,
				Label:     "step: " + child.Name,
			})

		case "enrich":
			bps = append(bps, BreakpointLocation{
				File:      file,
				Line:      child.Range.Start.Line,
				Flow:      flowName,
				Stage:     "enrich",
				RuleIndex: -1,
				Label:     "enrich: " + child.Name,
			})

		case "transform":
			// Stage-level breakpoint on the transform block
			bps = append(bps, BreakpointLocation{
				File:      file,
				Line:      child.Range.Start.Line,
				Flow:      flowName,
				Stage:     "transform",
				RuleIndex: -1,
				Label:     "transform",
			})
			// Per-rule breakpoints on each CEL mapping
			ruleIdx := 0
			for _, attr := range child.Attrs {
				if attr.Name == "use" {
					continue
				}
				label := attr.Name
				if attr.ValueRaw != "" {
					label = attr.Name + " = " + attr.ValueRaw
				}
				bps = append(bps, BreakpointLocation{
					File:      file,
					Line:      attr.Range.Start.Line,
					Flow:      flowName,
					Stage:     "transform",
					RuleIndex: ruleIdx,
					Label:     "transform: " + label,
				})
				ruleIdx++
			}

		case "to":
			bps = append(bps, BreakpointLocation{
				File:      file,
				Line:      child.Range.Start.Line,
				Flow:      flowName,
				Stage:     "write",
				RuleIndex: -1,
				Label:     "write → " + child.GetAttr("connector"),
			})

		case "response":
			// Stage-level
			bps = append(bps, BreakpointLocation{
				File:      file,
				Line:      child.Range.Start.Line,
				Flow:      flowName,
				Stage:     "response",
				RuleIndex: -1,
				Label:     "response",
			})
			// Per-rule
			ruleIdx := 0
			for _, attr := range child.Attrs {
				label := attr.Name
				if attr.ValueRaw != "" {
					label = attr.Name + " = " + attr.ValueRaw
				}
				bps = append(bps, BreakpointLocation{
					File:      file,
					Line:      attr.Range.Start.Line,
					Flow:      flowName,
					Stage:     "response",
					RuleIndex: ruleIdx,
					Label:     "response: " + label,
				})
				ruleIdx++
			}
		}
	}

	return bps
}

// AllBreakpoints returns all valid breakpoint locations across the entire project,
// grouped by file. Studio uses this to know which lines in which files can have breakpoints.
func (e *Engine) AllBreakpoints() map[string][]BreakpointLocation {
	e.index.mu.RLock()
	flowNames := make([]string, 0, len(e.index.Flows))
	for name := range e.index.Flows {
		flowNames = append(flowNames, name)
	}
	e.index.mu.RUnlock()

	result := make(map[string][]BreakpointLocation)
	for _, name := range flowNames {
		bps := e.FlowBreakpoints(name)
		for _, bp := range bps {
			result[bp.File] = append(result[bp.File], bp)
		}
	}
	return result
}

// findAttr returns the attribute with the given name, or nil.
func findAttr(b *Block, name string) *Attribute {
	for _, a := range b.Attrs {
		if a.Name == name {
			return a
		}
	}
	return nil
}

// findChildBlock returns the first child block with the given type, or nil.
func findChildBlock(b *Block, blockType string) *Block {
	for _, c := range b.Children {
		if c.Type == blockType {
			return c
		}
	}
	return nil
}

func hasChildBlock(b *Block, blockType string) bool {
	for _, child := range b.Children {
		if child.Type == blockType {
			return true
		}
	}
	return false
}
