package ide

import (
	"fmt"

	"github.com/matutetandil/mycel/pkg/schema"
)

// complete returns completion items based on cursor context.
func complete(fi *FileIndex, idx *ProjectIndex, reg *schema.Registry, line, col int) []CompletionItem {
	ctx := findCursorContext(fi, line, col)

	if ctx.InValue {
		// CEL context — offer variables and functions
		if isCELContext(ctx.BlockPath, ctx.AttrName) {
			flowBlock := findFlowBlock(fi, ctx.BlockPath)
			items := celVariables(ctx.BlockPath, flowBlock, idx)
			items = append(items, celFunctions()...)
			return items
		}

		// Operation completions based on connector type
		if ctx.AttrName == "operation" && ctx.Block != nil {
			connName := ctx.Block.GetAttr("connector")
			if connName != "" {
				idx.mu.RLock()
				entity := idx.Connectors[connName]
				idx.mu.RUnlock()
				if entity != nil {
					if ops := completeOperation(entity.ConnType); len(ops) > 0 {
						return ops
					}
				}
			}
		}

		// Transform field suggestions in to.query and response blocks
		if ctx.Block != nil {
			flowBlock := findFlowBlock(fi, ctx.BlockPath)
			if flowBlock != nil {
				lastBlock := ""
				if len(ctx.BlockPath) > 0 {
					lastBlock = ctx.BlockPath[len(ctx.BlockPath)-1]
				}

				// In to block query attribute → suggest :fieldName params
				if lastBlock == "to" && ctx.AttrName == "query" {
					fields := collectTransformFields(flowBlock, idx)
					if len(fields) > 0 {
						var items []CompletionItem
						for _, f := range fields {
							items = append(items, CompletionItem{
								Label:      ":" + f,
								Kind:       CompletionValue,
								Detail:     "Transform output field",
								InsertText: ":" + f,
							})
						}
						return items
					}
				}

				// In response block → suggest output.fieldName
				if lastBlock == "response" {
					fields := collectTransformFields(flowBlock, idx)
					if len(fields) > 0 {
						var items []CompletionItem
						items = append(items, CompletionItem{
							Label:  "input",
							Kind:   CompletionValue,
							Detail: "Original input data",
						})
						items = append(items, CompletionItem{
							Label:  "output",
							Kind:   CompletionValue,
							Detail: "Destination result",
						})
						for _, f := range fields {
							items = append(items, CompletionItem{
								Label:      "output." + f,
								Kind:       CompletionValue,
								Detail:     "Transform output field",
								InsertText: "output." + f,
							})
						}
						return items
					}
				}
			}
		}

		return completeValue(ctx, idx)
	}

	if ctx.Block == nil {
		// At root level — offer top-level blocks
		return completeRootBlocks()
	}

	// Inside a connector block — offer type-specific attrs and children from registry
	if len(ctx.BlockPath) == 1 && ctx.BlockPath[0] == "connector" && ctx.Block != nil {
		connType := ctx.Block.GetAttr("type")
		driver := ctx.Block.GetAttr("driver")
		if connType != "" {
			// Try registry first (has full schema including children)
			extraAttrs := connectorTypeAttrsFromRegistry(reg, connType, driver)
			extraChildren := connectorTypeChildrenFromRegistry(reg, connType, driver)
			if extraAttrs == nil {
				// Fall back to static
				extraAttrs = connectorTypeAttrsStatic(connType)
			}
			if len(extraAttrs) > 0 || len(extraChildren) > 0 {
				return completeBlockContentWithExtraAndChildren(ctx, idx, extraAttrs, extraChildren)
			}
		}
	}

	// Inside a block — offer child blocks and attributes
	return completeBlockContent(ctx, idx)
}

// findFlowBlock finds the flow-level block from the file based on block path.
func findFlowBlock(fi *FileIndex, blockPath []string) *Block {
	if len(blockPath) == 0 {
		return nil
	}
	if blockPath[0] != "flow" {
		return nil
	}
	for _, b := range fi.Blocks {
		if b.Type == "flow" {
			return b
		}
	}
	return nil
}

// completeBlockContentWithExtraAndChildren adds connector-type-specific attrs and children.
func completeBlockContentWithExtraAndChildren(ctx *CursorContext, idx *ProjectIndex, extraAttrs []AttrSchema, extraChildren []BlockSchema) []CompletionItem {
	items := completeBlockContent(ctx, idx)

	existing := make(map[string]bool)
	if ctx.Block != nil {
		for _, a := range ctx.Block.Attrs {
			existing[a.Name] = true
		}
	}
	for _, item := range items {
		existing[item.Label] = true
	}

	for _, as := range extraAttrs {
		if existing[as.Name] {
			continue
		}
		detail := as.Doc
		if as.Required {
			detail = "(required) " + detail
		}
		insert := fmt.Sprintf("%s = ", as.Name)
		if as.Type == AttrString || as.Type == AttrDuration {
			insert = fmt.Sprintf("%s = \"\"", as.Name)
		} else if as.Type == AttrBool {
			insert = fmt.Sprintf("%s = true", as.Name)
		}
		items = append(items, CompletionItem{
			Label:      as.Name,
			Kind:       CompletionAttribute,
			Detail:     detail,
			InsertText: insert,
		})
	}

	existingBlocks := make(map[string]bool)
	if ctx.Block != nil {
		for _, c := range ctx.Block.Children {
			existingBlocks[c.Type] = true
		}
	}
	for _, cs := range extraChildren {
		if existingBlocks[cs.Type] {
			continue
		}
		insert := fmt.Sprintf("%s {\n  \n}", cs.Type)
		items = append(items, CompletionItem{
			Label:      cs.Type,
			Kind:       CompletionBlock,
			Detail:     cs.Doc,
			InsertText: insert,
		})
	}

	return items
}

// completeBlockContentWithExtra adds connector-type-specific attrs to completions.
func completeBlockContentWithExtra(ctx *CursorContext, idx *ProjectIndex, extra []AttrSchema) []CompletionItem {
	items := completeBlockContent(ctx, idx)

	existing := make(map[string]bool)
	if ctx.Block != nil {
		for _, a := range ctx.Block.Attrs {
			existing[a.Name] = true
		}
	}
	// Also skip items already in the base completions
	for _, item := range items {
		existing[item.Label] = true
	}

	for _, as := range extra {
		if existing[as.Name] {
			continue
		}
		detail := as.Doc
		if as.Required {
			detail = "(required) " + detail
		}
		insert := fmt.Sprintf("%s = ", as.Name)
		if as.Type == AttrString {
			insert = fmt.Sprintf("%s = \"\"", as.Name)
		} else if as.Type == AttrBool {
			insert = fmt.Sprintf("%s = true", as.Name)
		}

		items = append(items, CompletionItem{
			Label:      as.Name,
			Kind:       CompletionAttribute,
			Detail:     detail,
			InsertText: insert,
		})
	}

	return items
}

// completeRootBlocks returns completions for root-level block types.
func completeRootBlocks() []CompletionItem {
	var items []CompletionItem
	for _, s := range rootSchema() {
		insert := s.Type
		if s.Labels > 0 {
			insert = fmt.Sprintf("%s \"name\" {\n  \n}", s.Type)
		} else {
			insert = fmt.Sprintf("%s {\n  \n}", s.Type)
		}
		items = append(items, CompletionItem{
			Label:      s.Type,
			Kind:       CompletionBlock,
			Detail:     s.Doc,
			InsertText: insert,
		})
	}
	return items
}

// completeBlockContent returns completions for blocks and attributes inside a block.
func completeBlockContent(ctx *CursorContext, idx *ProjectIndex) []CompletionItem {
	schema := lookupBlockSchema(ctx.BlockPath)
	if schema == nil {
		return nil
	}

	var items []CompletionItem

	// Present attributes already defined
	existing := make(map[string]bool)
	if ctx.Block != nil {
		for _, a := range ctx.Block.Attrs {
			existing[a.Name] = true
		}
	}

	// Offer attributes not yet present
	for _, as := range schema.Attrs {
		if existing[as.Name] {
			continue
		}
		detail := as.Doc
		if as.Required {
			detail = "(required) " + detail
		}
		insert := fmt.Sprintf("%s = ", as.Name)
		if as.Type == AttrString {
			insert = fmt.Sprintf("%s = \"\"", as.Name)
		} else if as.Type == AttrBool {
			insert = fmt.Sprintf("%s = true", as.Name)
		}

		items = append(items, CompletionItem{
			Label:      as.Name,
			Kind:       CompletionAttribute,
			Detail:     detail,
			InsertText: insert,
		})
	}

	// Present child blocks already defined
	existingBlocks := make(map[string]bool)
	if ctx.Block != nil {
		for _, c := range ctx.Block.Children {
			existingBlocks[c.Type] = true
		}
	}

	// Offer child blocks
	for _, cs := range schema.Children {
		// Some blocks can appear multiple times (step, enrich, to)
		multipleAllowed := cs.Type == "step" || cs.Type == "enrich" || cs.Type == "to"
		if existingBlocks[cs.Type] && !multipleAllowed {
			continue
		}

		insert := cs.Type
		if cs.Labels > 0 {
			insert = fmt.Sprintf("%s \"name\" {\n  \n}", cs.Type)
		} else {
			insert = fmt.Sprintf("%s {\n  \n}", cs.Type)
		}

		items = append(items, CompletionItem{
			Label:      cs.Type,
			Kind:       CompletionBlock,
			Detail:     cs.Doc,
			InsertText: insert,
		})
	}

	return items
}

// completeValue returns completions for attribute values.
func completeValue(ctx *CursorContext, idx *ProjectIndex) []CompletionItem {
	if ctx.Block == nil || ctx.AttrName == "" {
		return nil
	}

	schema := lookupBlockSchema(ctx.BlockPath)
	if schema == nil {
		return nil
	}

	as := findAttrSchema(schema.Attrs, ctx.AttrName)
	if as == nil {
		return nil
	}

	var items []CompletionItem

	// Static enum values
	for _, v := range as.Values {
		items = append(items, CompletionItem{
			Label:      v,
			Kind:       CompletionValue,
			InsertText: fmt.Sprintf(`"%s"`, v),
		})
	}

	// Dynamic reference values from project index
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	switch as.Ref {
	case RefConnector:
		for name, e := range idx.Connectors {
			detail := e.ConnType
			if e.Driver != "" {
				detail += "/" + e.Driver
			}
			items = append(items, CompletionItem{
				Label:      name,
				Kind:       CompletionValue,
				Detail:     detail,
				InsertText: fmt.Sprintf(`"%s"`, name),
			})
		}
	case RefType:
		for name := range idx.Types {
			items = append(items, CompletionItem{
				Label:      name,
				Kind:       CompletionValue,
				InsertText: fmt.Sprintf(`"%s"`, name),
			})
		}
	case RefTransform:
		for name := range idx.Transforms {
			items = append(items, CompletionItem{
				Label:      name,
				Kind:       CompletionValue,
				InsertText: fmt.Sprintf(`"%s"`, name),
			})
		}
	case RefFlow:
		for name := range idx.Flows {
			items = append(items, CompletionItem{
				Label:      name,
				Kind:       CompletionValue,
				InsertText: fmt.Sprintf(`"%s"`, name),
			})
		}
	case RefCache:
		for name := range idx.Caches {
			items = append(items, CompletionItem{
				Label:      name,
				Kind:       CompletionValue,
				InsertText: fmt.Sprintf(`"cache.%s"`, name),
			})
		}
	}

	return items
}
