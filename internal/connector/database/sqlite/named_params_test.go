package sqlite

import "testing"

// A comment is not code. An apostrophe in one used to open a string literal
// that never closed, so every placeholder after it reached the driver as
// literal text with nothing bound — and `mycel validate` could not see it,
// because it does not execute SQL.
//
// The scan itself is shared and tested in internal/connector; this pins the
// driver to it and to its own placeholder style.
func TestParseNamedParams_ApostropheInComment(t *testing.T) {
	c := &Connector{}
	params := map[string]interface{}{"sku": "X1", "orp": "X1-ORP"}

	sql := "-- the item's parent\nSELECT id FROM t WHERE sku = :sku OR sku = :orp"
	want := "-- the item's parent\nSELECT id FROM t WHERE sku = ? OR sku = ?"

	got, args, err := c.parseNamedParams(sql, params)

	if err != nil {

		t.Fatalf("bind: %v", err)

	}
	if got != want {
		t.Errorf("sql:\n  got  %q\n  want %q", got, want)
	}
	if len(args) != 2 {
		t.Fatalf("args = %d, want 2 — the placeholders after the comment were not bound", len(args))
	}
	if args[0] != "X1" || args[1] != "X1-ORP" {
		t.Errorf("args = %v, want [X1 X1-ORP]", args)
	}
}

// The other direction: a comment must not consume one of the statement's
// arguments.
func TestParseNamedParams_ColonInCommentIsNotAParameter(t *testing.T) {
	c := &Connector{}
	got, args, err := c.parseNamedParams("-- ratio:sku\nSELECT :sku", map[string]interface{}{"sku": "X1"})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if want := "-- ratio:sku\nSELECT ?"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(args) != 1 {
		t.Errorf("args = %d, want 1", len(args))
	}
}
