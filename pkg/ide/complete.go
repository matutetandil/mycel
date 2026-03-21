package ide

import "fmt"

// complete returns completion items based on cursor context.
func complete(fi *FileIndex, idx *ProjectIndex, line, col int) []CompletionItem {
	ctx := findCursorContext(fi, line, col)

	if ctx.InValue {
		return completeValue(ctx, idx)
	}

	if ctx.Block == nil {
		// At root level — offer top-level blocks
		return completeRootBlocks()
	}

	// Inside a block — offer child blocks and attributes
	return completeBlockContent(ctx, idx)
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
