package analyzer

import (
	"sort"
	"testing"
)

// A field with a selection inside it is another entity, not a column.

func TestLeavesLeavesOutWhatHasChildren(t *testing.T) {
	fields := &RequestedFields{tree: &FieldTree{Fields: map[string]*FieldNode{
		"id":    {Name: "id", IsLeaf: true},
		"total": {Name: "total", IsLeaf: true},
		"user": {Name: "user", Children: &FieldTree{Fields: map[string]*FieldNode{
			"name": {Name: "name", IsLeaf: true},
		}}},
	}}}

	leaves := fields.Leaves()
	sort.Strings(leaves)
	if len(leaves) != 2 || leaves[0] != "id" || leaves[1] != "total" {
		t.Errorf("the leaves are %v; user carries a selection", leaves)
	}

	all := fields.List()
	if len(all) != 3 {
		t.Errorf("the top level is %v; all three were asked for", all)
	}
}

func TestLeavesOfNothing(t *testing.T) {
	empty := &RequestedFields{}
	if got := empty.Leaves(); len(got) != 0 {
		t.Errorf("an empty request has leaves %v", got)
	}
}
