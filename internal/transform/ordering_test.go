package transform

import (
	"context"
	"testing"
)

// JSON has one number type and CEL has three, so the same field arrives as an
// integer from one record and a double from the next — a price of 30 beside a
// price of 2.5 — and every function that puts values in order has to cope with
// that. When it does not, nothing fails: the wrong element comes back, or the
// list comes back in the order it was given, and the flow carries on.

func evaluate(t *testing.T, expr string, input map[string]interface{}) interface{} {
	t.Helper()
	tr, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}
	value, err := tr.EvaluateExpression(context.Background(), input, nil, expr)
	if err != nil {
		t.Fatalf("evaluating %q: %v", expr, err)
	}
	return value
}

func TestTheSmallestAndLargestAreFoundAmongMixedNumbers(t *testing.T) {
	// Prices, totals and quantities are exactly where this happens: a whole
	// number is an integer and a fractional one is a double, in the same list.
	input := map[string]interface{}{
		"prices": []interface{}{10, 2.5, 30, 1.5},
	}

	if got := evaluate(t, "min_val(input.prices)", input); toFloat(got) != 1.5 {
		t.Errorf("min_val = %v, want 1.5", got)
	}
	if got := evaluate(t, "max_val(input.prices)", input); toFloat(got) != 30 {
		t.Errorf("max_val = %v, want 30", got)
	}

	// The order the list happens to be in must not change the answer.
	reversed := map[string]interface{}{"prices": []interface{}{1.5, 30, 2.5, 10}}
	if got := evaluate(t, "min_val(input.prices)", reversed); toFloat(got) != 1.5 {
		t.Errorf("min_val of the same values reordered = %v, want 1.5", got)
	}
	if got := evaluate(t, "max_val(input.prices)", reversed); toFloat(got) != 30 {
		t.Errorf("max_val of the same values reordered = %v, want 30", got)
	}
}

func TestOrderingWorksWhenEveryValueIsTheSameKind(t *testing.T) {
	for name, tc := range map[string]struct {
		list     []interface{}
		min, max float64
	}{
		"whole numbers":      {[]interface{}{3, 1, 2}, 1, 3},
		"fractional numbers": {[]interface{}{3.5, 1.5, 2.5}, 1.5, 3.5},
		"negatives":          {[]interface{}{-1, -30, 2}, -30, 2},
		"one element":        {[]interface{}{7}, 7, 7},
	} {
		t.Run(name, func(t *testing.T) {
			input := map[string]interface{}{"list": tc.list}
			if got := evaluate(t, "min_val(input.list)", input); toFloat(got) != tc.min {
				t.Errorf("min_val = %v, want %v", got, tc.min)
			}
			if got := evaluate(t, "max_val(input.list)", input); toFloat(got) != tc.max {
				t.Errorf("max_val = %v, want %v", got, tc.max)
			}
		})
	}
}

func TestLargeIdentifiersKeepTheirOrder(t *testing.T) {
	// Two integers beyond the range a float holds exactly must still be told
	// apart, which is why same-typed integers are not compared as floats.
	input := map[string]interface{}{
		"ids": []interface{}{int64(9007199254740993), int64(9007199254740992)},
	}
	got := evaluate(t, "max_val(input.ids)", input)
	if toFloat(got) != float64(9007199254740993) {
		t.Errorf("max_val = %v, want the larger identifier", got)
	}
}

func TestAnEmptyListHasNoSmallestOrLargest(t *testing.T) {
	input := map[string]interface{}{"list": []interface{}{}}
	if got := evaluate(t, "min_val(input.list)", input); got != nil {
		t.Errorf("min_val of nothing = %v, want nothing", got)
	}
	if got := evaluate(t, "max_val(input.list)", input); got != nil {
		t.Errorf("max_val of nothing = %v, want nothing", got)
	}
}

func TestRecordsSortByANumberTheyDoNotAllTypeTheSameWay(t *testing.T) {
	input := map[string]interface{}{
		"orders": []interface{}{
			map[string]interface{}{"id": "a", "total": 30},
			map[string]interface{}{"id": "b", "total": 2.5},
			map[string]interface{}{"id": "c", "total": 10},
			map[string]interface{}{"id": "d", "total": 10.5},
		},
	}

	got := evaluate(t, "sort_by(input.orders, 'total').map(o, o.id)", input)
	ids, ok := got.([]interface{})
	if !ok {
		t.Fatalf("sort_by returned %T", got)
	}
	want := []string{"b", "c", "d", "a"}
	if len(ids) != len(want) {
		t.Fatalf("got %v, want %v", ids, want)
	}
	for i, id := range want {
		if ids[i] != id {
			t.Fatalf("sorted order = %v, want %v", ids, want)
		}
	}
}

func TestRecordsSortByText(t *testing.T) {
	input := map[string]interface{}{
		"people": []interface{}{
			map[string]interface{}{"name": "carol"},
			map[string]interface{}{"name": "alice"},
			map[string]interface{}{"name": "bob"},
		},
	}
	got := evaluate(t, "sort_by(input.people, 'name').map(p, p.name)", input)
	names, _ := got.([]interface{})
	want := []string{"alice", "bob", "carol"}
	for i, name := range want {
		if len(names) <= i || names[i] != name {
			t.Fatalf("sorted order = %v, want %v", names, want)
		}
	}
}

func TestSortingAnEmptyListIsNotAnError(t *testing.T) {
	input := map[string]interface{}{"orders": []interface{}{}}
	got := evaluate(t, "sort_by(input.orders, 'total')", input)
	if list, ok := got.([]interface{}); !ok || len(list) != 0 {
		t.Errorf("sort_by of nothing = %v", got)
	}
}

func TestValuesWithNoOrderBetweenThemLeaveTheListAlone(t *testing.T) {
	// A key that is missing from some records, or holds something with no
	// ordering, must leave those where they were rather than shuffling them
	// somewhere arbitrary.
	input := map[string]interface{}{
		"records": []interface{}{
			map[string]interface{}{"id": "a"},
			map[string]interface{}{"id": "b"},
			map[string]interface{}{"id": "c"},
		},
	}
	got := evaluate(t, "sort_by(input.records, 'absent').map(r, r.id)", input)
	ids, _ := got.([]interface{})
	for i, id := range []string{"a", "b", "c"} {
		if len(ids) <= i || ids[i] != id {
			t.Fatalf("order = %v, want it unchanged", ids)
		}
	}
}

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	case float32:
		return float64(n)
	case float64:
		return n
	}
	return 0
}
