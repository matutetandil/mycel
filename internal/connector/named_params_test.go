package connector

import (
	"reflect"
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
			got, args := BindNamedParams(tc.sql, params, tc.dialect)
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
			got, args := BindNamedParams(tc.sql, params, tc.dialect)
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
		got, args := BindNamedParams("SELECT * FROM t WHERE sku = :sku AND n > :n", params, MySQLDialect)
		if want := "SELECT * FROM t WHERE sku = ? AND n > ?"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if !reflect.DeepEqual(args, []interface{}{"X1", 2}) {
			t.Errorf("args = %v, want [X1 2]", args)
		}
	})

	t.Run("postgres numbers from one", func(t *testing.T) {
		got, _ := BindNamedParams("SELECT :sku, :n, :sku", params, PostgresDialect)
		if want := "SELECT $1, $2, $3"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a postgres cast is not a placeholder", func(t *testing.T) {
		got, args := BindNamedParams("SELECT :sku::text", params, PostgresDialect)
		if want := "SELECT $1::text"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if len(args) != 1 {
			t.Errorf("args = %d, want 1", len(args))
		}
	})

	t.Run("an unknown placeholder is left as written", func(t *testing.T) {
		got, args := BindNamedParams("SELECT :nope", params, MySQLDialect)
		if want := "SELECT :nope"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		if len(args) != 0 {
			t.Errorf("args = %d, want 0", len(args))
		}
	})

	t.Run("no params is a passthrough", func(t *testing.T) {
		sql := "SELECT 1 -- it's fine"
		got, args := BindNamedParams(sql, nil, MySQLDialect)
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

	got, args := BindNamedParams("SELECT 1--2, :sku", params, MySQLDialect)
	if want := "SELECT 1--2, ?"; got != want {
		t.Errorf("MySQL: got %q, want %q", got, want)
	}
	if len(args) != 1 {
		t.Errorf("MySQL: args = %d, want 1", len(args))
	}

	// Postgres and SQLite have no such rule.
	got, args = BindNamedParams("SELECT 1--2, :sku", params, PostgresDialect)
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

	got, _ := BindNamedParams("/* a /* b */ :sku */ SELECT :sku", params, PostgresDialect)
	if want := "/* a /* b */ :sku */ SELECT $1"; got != want {
		t.Errorf("Postgres: got %q, want %q", got, want)
	}

	got, _ = BindNamedParams("/* a /* b */ :sku */ SELECT :sku", params, MySQLDialect)
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
		if _, _ = BindNamedParams(sql, params, MySQLDialect); false {
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
