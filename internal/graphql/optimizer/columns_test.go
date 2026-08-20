package optimizer

import (
	"sort"
	"testing"
)

// Which requested fields name a column.
//
// A query rewritten to fetch only what was asked for is the whole point of this
// package, and the list it was given was every top-level field — including the
// ones carrying a selection. `orders { id total user { name } }` asks for a
// user, not for a column called user, and the query built from that list was
// refused by the database: "no such column: user", from the example named after
// the optimisation.

func TestOnlyTheFieldsWithNothingInsideThemAreColumns(t *testing.T) {
	input := map[string]interface{}{
		"__requested_top_fields": []interface{}{"id", "total", "user", "product"},
		"__requested_columns":    []interface{}{"id", "total"},
	}

	got := ColumnsFromInput(input)
	sort.Strings(got)
	if len(got) != 2 || got[0] != "id" || got[1] != "total" {
		t.Errorf("the columns are %v; user and product are entities, not columns", got)
	}
}

func TestWithNothingSaidTheTopLevelIsUsed(t *testing.T) {
	// A caller that never published the distinction behaves as it did.
	input := map[string]interface{}{
		"__requested_top_fields": []interface{}{"id", "name"},
	}

	got := ColumnsFromInput(input)
	sort.Strings(got)
	if len(got) != 2 || got[0] != "id" || got[1] != "name" {
		t.Errorf("the columns are %v", got)
	}
}

func TestNoFieldsAtAll(t *testing.T) {
	if got := ColumnsFromInput(nil); got != nil {
		t.Errorf("with no input the columns are %v", got)
	}
	if got := ColumnsFromInput(map[string]interface{}{}); len(got) != 0 {
		t.Errorf("with nothing requested the columns are %v", got)
	}
}
