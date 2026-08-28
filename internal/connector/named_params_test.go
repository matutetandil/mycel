package connector

import (
	"reflect"
	"strings"
	"testing"
)

// The reports that produced this scanner, first. An apostrophe in a comment
// used to open a string literal that never closed, so every placeholder after
// it reached the driver as literal text with nothing bound; and a colon inside
// a comment used to be bound as if it were a placeholder, consuming one of the
// statement's arguments.
func TestBindNamedParams_CommentsAreNotCode(t *testing.T) {
	params := map[string]interface{}{"sku": "X1", "orp": "X1-ORP"}

	for _, tc := range []struct {
		name    string
		dialect SQLDialect
		sql     string
		want    string
		wantN   int
	}{
		{
			// "the item's parent" — the exact shape reported.
			name:    "apostrophe in a line comment",
			dialect: MySQLDialect,
			sql:     "-- the item's parent\nSELECT id FROM t WHERE sku = :sku",
			want:    "-- the item's parent\nSELECT id FROM t WHERE sku = ?",
			wantN:   1,
		},
		{
			name:    "apostrophe in a block comment",
			dialect: MySQLDialect,
			sql:     "/* doesn't matter */ SELECT id FROM t WHERE sku = :sku",
			want:    "/* doesn't matter */ SELECT id FROM t WHERE sku = ?",
			wantN:   1,
		},
		{
			name:    "apostrophe in a comment between two placeholders",
			dialect: MySQLDialect,
			sql:     "SELECT 1 FROM t WHERE sku = :sku -- it's the parent\n  OR sku = :orp",
			want:    "SELECT 1 FROM t WHERE sku = ? -- it's the parent\n  OR sku = ?",
			wantN:   2,
		},
		{
			// The other direction: a comment must not consume an argument.
			name:    "a colon inside a comment is not a placeholder",
			dialect: MySQLDialect,
			sql:     "-- ratio:sku\nSELECT id FROM t WHERE sku = :sku",
			want:    "-- ratio:sku\nSELECT id FROM t WHERE sku = ?",
			wantN:   1,
		},
		{
			name:    "hash comment (MySQL)",
			dialect: MySQLDialect,
			sql:     "# it's fine\nSELECT :sku",
			want:    "# it's fine\nSELECT ?",
			wantN:   1,
		},
		{
			name:    "postgres numbers its placeholders across a comment",
			dialect: PostgresDialect,
			sql:     "-- the item's parent\nSELECT 1 WHERE sku = :sku OR sku = :orp",
			want:    "-- the item's parent\nSELECT 1 WHERE sku = $1 OR sku = $2",
			wantN:   2,
		},
		{
			name:    "sqlite",
			dialect: SQLiteDialect,
			sql:     "/* the item's parent */ SELECT 1 WHERE sku = :sku",
			want:    "/* the item's parent */ SELECT 1 WHERE sku = ?",
			wantN:   1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, args, err := BindNamedParams(tc.sql, params, tc.dialect)
			if err != nil {
				t.Fatalf("bind: %v", err)
			}
			if got != tc.want {
				t.Errorf("sql:\n  got  %q\n  want %q", got, tc.want)
			}
			if len(args) != tc.wantN {
				t.Errorf("args = %d, want %d (%v)", len(args), tc.wantN, args)
			}
		})
	}
}

// String literals and quoted identifiers keep their existing protection, and
// gain the escapes each dialect actually uses.
func TestBindNamedParams_LiteralsAndIdentifiers(t *testing.T) {
	params := map[string]interface{}{"sku": "X1", "id": 7}

	for _, tc := range []struct {
		name    string
		dialect SQLDialect
		sql     string
		want    string
		wantN   int
	}{
		{
			name:    "a colon inside a string is not a placeholder",
			dialect: MySQLDialect,
			sql:     "SELECT 'a:sku' WHERE sku = :sku",
			want:    "SELECT 'a:sku' WHERE sku = ?",
			wantN:   1,
		},
		{
			name:    "doubled quote does not end the string",
			dialect: SQLiteDialect,
			sql:     "SELECT 'it''s' WHERE sku = :sku",
			want:    "SELECT 'it''s' WHERE sku = ?",
			wantN:   1,
		},
		{
			// Without backslash escapes the literal ends at the middle quote
			// and the rest of the statement is scanned as if inside a string.
			name:    "backslash-escaped quote (MySQL)",
			dialect: MySQLDialect,
			sql:     `SELECT 'it\'s' WHERE sku = :sku`,
			want:    `SELECT 'it\'s' WHERE sku = ?`,
			wantN:   1,
		},
		{
			name:    "backtick identifier holding a colon",
			dialect: MySQLDialect,
			sql:     "SELECT `a:b` FROM t WHERE sku = :sku",
			want:    "SELECT `a:b` FROM t WHERE sku = ?",
			wantN:   1,
		},
		{
			name:    "double-quoted identifier holding an apostrophe",
			dialect: PostgresDialect,
			sql:     `SELECT "it's" FROM t WHERE sku = :sku`,
			want:    `SELECT "it's" FROM t WHERE sku = $1`,
			wantN:   1,
		},
		{
			name:    "bracket identifier (SQLite)",
			dialect: SQLiteDialect,
			sql:     "SELECT [a:b] FROM t WHERE sku = :sku",
			want:    "SELECT [a:b] FROM t WHERE sku = ?",
			wantN:   1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, args, err := BindNamedParams(tc.sql, params, tc.dialect)
			if err != nil {
				t.Fatalf("bind: %v", err)
			}
			if got != tc.want {
				t.Errorf("sql:\n  got  %q\n  want %q", got, tc.want)
			}
			if len(args) != tc.wantN {
				t.Errorf("args = %d, want %d", len(args), tc.wantN)
			}
		})
	}
}

// The behaviour every driver already had, unchanged.
func TestBindNamedParams_UnchangedBehaviour(t *testing.T) {
	params := map[string]interface{}{"sku": "X1", "n": 2}

	t.Run("ordinary binding", func(t *testing.T) {
		got, args, err := BindNamedParams("SELECT * FROM t WHERE sku = :sku AND n > :n", params, MySQLDialect)
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		if want := "SELECT * FROM t WHERE sku = ? AND n > ?"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if !reflect.DeepEqual(args, []interface{}{"X1", 2}) {
			t.Errorf("args = %v, want [X1 2]", args)
		}
	})

	t.Run("postgres numbers from one", func(t *testing.T) {
		got, _, err := BindNamedParams("SELECT :sku, :n, :sku", params, PostgresDialect)
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		if want := "SELECT $1, $2, $3"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a postgres cast is not a placeholder", func(t *testing.T) {
		got, args, err := BindNamedParams("SELECT :sku::text", params, PostgresDialect)
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		if want := "SELECT $1::text"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if len(args) != 1 {
			t.Errorf("args = %d, want 1", len(args))
		}
	})

	t.Run("an unknown placeholder is left as written", func(t *testing.T) {
		got, args, err := BindNamedParams("SELECT :nope", params, MySQLDialect)
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		if want := "SELECT :nope"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if len(args) != 0 {
			t.Errorf("args = %d, want 0", len(args))
		}
	})

	t.Run("no params is a passthrough", func(t *testing.T) {
		sql := "SELECT 1 -- it's fine"
		got, args, err := BindNamedParams(sql, nil, MySQLDialect)
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		if got != sql || args != nil {
			t.Errorf("got %q / %v", got, args)
		}
	})
}

// MySQL needs whitespace after the dashes for a comment. Reading `a--b` as one
// would swallow the rest of the statement, which is the failure this scanner
// exists to prevent.
func TestBindNamedParams_DashDashNeedsSpaceInMySQL(t *testing.T) {
	params := map[string]interface{}{"sku": "X1"}

	got, args, err := BindNamedParams("SELECT 1--2, :sku", params, MySQLDialect)

	if err != nil {

		t.Fatalf("bind: %v", err)

	}
	if want := "SELECT 1--2, ?"; got != want {
		t.Errorf("MySQL: got %q, want %q", got, want)
	}
	if len(args) != 1 {
		t.Errorf("MySQL: args = %d, want 1", len(args))
	}

	// Postgres and SQLite have no such rule.
	got, args, err = BindNamedParams("SELECT 1--2, :sku", params, PostgresDialect)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if want := "SELECT 1--2, :sku"; got != want {
		t.Errorf("Postgres: got %q, want %q", got, want)
	}
	if len(args) != 0 {
		t.Errorf("Postgres: args = %d, want 0", len(args))
	}
}

// Postgres block comments nest; the others end at the first close.
func TestBindNamedParams_NestedBlockComments(t *testing.T) {
	params := map[string]interface{}{"sku": "X1"}

	got, _, err := BindNamedParams("/* a /* b */ :sku */ SELECT :sku", params, PostgresDialect)

	if err != nil {

		t.Fatalf("bind: %v", err)

	}
	if want := "/* a /* b */ :sku */ SELECT $1"; got != want {
		t.Errorf("Postgres: got %q, want %q", got, want)
	}

	got, _, err = BindNamedParams("/* a /* b */ :sku */ SELECT :sku", params, MySQLDialect)

	if err != nil {

		t.Fatalf("bind: %v", err)

	}
	if want := "/* a /* b */ ? */ SELECT ?"; got != want {
		t.Errorf("MySQL: got %q, want %q", got, want)
	}
}

// An unterminated comment or literal runs to the end of the statement, which
// is what the driver does with it too. The scanner must not loop or panic.
func TestBindNamedParams_Unterminated(t *testing.T) {
	params := map[string]interface{}{"sku": "X1"}
	for _, sql := range []string{
		"SELECT :sku /* unterminated",
		"SELECT :sku 'unterminated",
		"SELECT :sku `unterminated",
		"-- only a comment",
		"",
		":",
		"::",
	} {
		if _, _, _ = BindNamedParams(sql, params, MySQLDialect); false {
			t.Fatal("unreachable")
		}
		_ = NamedParamsIn(sql, GenericSQLDialect)
	}
}

// NamedParamsIn answers "which placeholders does this statement carry", and is
// read to publish GraphQL arguments. A colon in a comment used to become an
// argument nothing could ever fill.
func TestNamedParamsIn(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
		want []string
	}{
		{"plain", "SELECT * FROM t WHERE sku = :sku AND n > :n", []string{"sku", "n"}},
		{"repeats collapse", "SELECT :sku, :sku", []string{"sku"}},
		{"comment is not a source", "-- ratio:phantom\nSELECT :sku", []string{"sku"}},
		{"apostrophe in a comment does not hide the rest", "-- item's\nSELECT :sku", []string{"sku"}},
		{"string is not a source", "SELECT 'a:phantom' WHERE sku = :sku", []string{"sku"}},
		{"cast is not a placeholder", "SELECT :sku::text", []string{"sku"}},
		{"none", "SELECT 1", nil},
		{"empty", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := NamedParamsIn(tc.sql, GenericSQLDialect)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// A set is bound as a list, and `IN (:ids)` has to become as many placeholders
// as the list has members. One placeholder handed a slice is refused by
// database/sql itself — "unsupported type []interface {}, a slice of
// interface" — so the same failure reached every driver, and there is an
// example in this repository that writes exactly this.
func TestBindNamedParams_ListExpandsInsideIN(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dialect SQLDialect
		sql     string
		params  map[string]interface{}
		want    string
		wantN   int
	}{
		{
			name:    "the example in examples/steps",
			dialect: SQLiteDialect,
			sql:     "SELECT * FROM order_items WHERE order_id IN (:order_ids)",
			params:  map[string]interface{}{"order_ids": []interface{}{1, 2, 3}},
			want:    "SELECT * FROM order_items WHERE order_id IN (?, ?, ?)",
			wantN:   3,
		},
		{
			name:    "postgres numbers across the expansion",
			dialect: PostgresDialect,
			sql:     "SELECT 1 WHERE a = :a AND b IN (:bs) AND c = :c",
			params:  map[string]interface{}{"a": 1, "bs": []interface{}{"x", "y"}, "c": 2},
			want:    "SELECT 1 WHERE a = $1 AND b IN ($2, $3) AND c = $4",
			wantN:   4,
		},
		{
			name:    "NOT IN",
			dialect: MySQLDialect,
			sql:     "SELECT 1 WHERE sku NOT IN (:skus)",
			params:  map[string]interface{}{"skus": []interface{}{"A", "B"}},
			want:    "SELECT 1 WHERE sku NOT IN (?, ?)",
			wantN:   2,
		},
		{
			name:    "one member",
			dialect: MySQLDialect,
			sql:     "SELECT 1 WHERE id IN (:ids)",
			params:  map[string]interface{}{"ids": []interface{}{7}},
			want:    "SELECT 1 WHERE id IN (?)",
			wantN:   1,
		},
		{
			name:    "a typed slice, which is what a step's result gives",
			dialect: MySQLDialect,
			sql:     "SELECT 1 WHERE id IN (:ids)",
			params:  map[string]interface{}{"ids": []int{1, 2}},
			want:    "SELECT 1 WHERE id IN (?, ?)",
			wantN:   2,
		},
		{
			// A string is a sequence and is not a set: IN (:name) with a name
			// in it means one name.
			name:    "a string is not a list",
			dialect: MySQLDialect,
			sql:     "SELECT 1 WHERE name IN (:n)",
			params:  map[string]interface{}{"n": "solo"},
			want:    "SELECT 1 WHERE name IN (?)",
			wantN:   1,
		},
		{
			// The scanner knows which colons are real, so a list in a comment
			// or a literal is not one.
			name:    "a comment does not open a set",
			dialect: MySQLDialect,
			sql:     "-- ids IN (:ids)\nSELECT 1 WHERE id IN (:ids)",
			params:  map[string]interface{}{"ids": []interface{}{1, 2}},
			want:    "-- ids IN (:ids)\nSELECT 1 WHERE id IN (?, ?)",
			wantN:   2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, args, err := BindNamedParams(tc.sql, tc.params, tc.dialect)
			if err != nil {
				t.Fatalf("bind: %v", err)
			}
			if got != tc.want {
				t.Errorf("sql:\n  got  %q\n  want %q", got, tc.want)
			}
			if len(args) != tc.wantN {
				t.Errorf("args = %d, want %d (%v)", len(args), tc.wantN, args)
			}
		})
	}
}

// The two shapes that have no right expansion are named rather than guessed.
func TestBindNamedParams_ListWhereItCannotGo(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sql     string
		params  map[string]interface{}
		wantErr string
	}{
		{
			// IN () is a syntax error in MySQL, Postgres and SQLite alike, and
			// there is no expansion right for both IN and NOT IN: IN (NULL)
			// matches nothing, which is what an empty set means, but
			// NOT IN (NULL) also matches nothing, which is its opposite.
			name:    "empty list",
			sql:     "SELECT 1 WHERE id IN (:ids)",
			params:  map[string]interface{}{"ids": []interface{}{}},
			wantErr: "empty list",
		},
		{
			// Expanding here produces `id = ?, ?, ?`, which the driver rejects
			// with a position in the statement rather than the parameter.
			name:    "a list where a scalar belongs",
			sql:     "SELECT 1 WHERE id = :ids",
			params:  map[string]interface{}{"ids": []interface{}{1, 2}},
			wantErr: "not inside an IN",
		},
		{
			name:    "a list in a plain parenthesis",
			sql:     "SELECT (:ids)",
			params:  map[string]interface{}{"ids": []interface{}{1, 2}},
			wantErr: "not inside an IN",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := BindNamedParams(tc.sql, tc.params, MySQLDialect)
			if err == nil {
				t.Fatal("expected an error rather than a statement that cannot run")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not say %q", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), "ids") {
				t.Errorf("error %q does not name the parameter", err)
			}
		})
	}
}
