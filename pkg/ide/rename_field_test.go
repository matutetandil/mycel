package ide

import (
	"strings"
	"testing"
)

func TestRenameFieldTransformAndQuery(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("flows/users.mycel", []byte(`
flow "create_user" {
  from {
    connector = "api"
    operation = "POST /users"
  }
  transform {
    email = "lower(input.email)"
    name  = "input.name"
  }
  to {
    connector = "db"
    target    = "users"
    query     = "INSERT INTO users (email, name) VALUES (:email, :name)"
  }
}
`)))

	result := e.RenameField("create_user", "email", "user_email")
	if result == nil {
		t.Fatal("expected RenameFieldResult")
	}

	if len(result.Edits) < 2 {
		t.Fatalf("expected at least 2 edits (transform attr + query), got %d", len(result.Edits))
		for _, ed := range result.Edits {
			t.Logf("  %s:%d → %q", ed.File, ed.Range.Start.Line, ed.NewText)
		}
	}

	// Check that transform attr rename is present
	hasTransformEdit := false
	for _, ed := range result.Edits {
		if ed.NewText == "user_email" {
			hasTransformEdit = true
		}
	}
	if !hasTransformEdit {
		t.Error("expected edit renaming transform attribute to 'user_email'")
	}

	// Check that query edit contains :user_email
	hasQueryEdit := false
	for _, ed := range result.Edits {
		if strings.Contains(ed.NewText, ":user_email") && strings.Contains(ed.NewText, "user_email,") {
			hasQueryEdit = true
		}
	}
	if !hasQueryEdit {
		t.Error("expected edit updating query with :user_email and user_email column")
	}

	// Check affected locations
	if len(result.AffectedLocations) < 2 {
		t.Errorf("expected at least 2 affected locations, got %d: %v", len(result.AffectedLocations), result.AffectedLocations)
	}
}

func TestRenameFieldMultipleOccurrencesInQuery(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("flows/upsert.mycel", []byte(`
flow "upsert" {
  from { connector = "rabbit" }
  transform {
    emails = "input.body.payload.emails.join(',')"
  }
  to {
    connector = "db"
    query     = "INSERT INTO t (emails) VALUES (:emails) ON DUPLICATE KEY UPDATE emails = :emails"
  }
}
`)))

	result := e.RenameField("upsert", "emails", "email_list")
	if result == nil {
		t.Fatal("expected result")
	}

	// Query should have all occurrences replaced
	for _, ed := range result.Edits {
		if strings.Contains(ed.NewText, ":email_list") {
			// Count occurrences of :email_list
			count := strings.Count(ed.NewText, ":email_list")
			if count != 2 {
				t.Errorf("expected 2 occurrences of :email_list in query, got %d: %s", count, ed.NewText)
			}
			// Column name should also be replaced
			if !strings.Contains(ed.NewText, "(email_list)") {
				t.Errorf("expected column name replaced in query: %s", ed.NewText)
			}
		}
	}
}

func TestRenameFieldResponse(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("flows/test.mycel", []byte(`
flow "test" {
  from { connector = "api" }
  transform {
    total = "input.price * input.qty"
  }
  to { connector = "db" }
  response {
    result = "output.total"
  }
}
`)))

	result := e.RenameField("test", "total", "grand_total")
	if result == nil {
		t.Fatal("expected result")
	}

	hasResponseEdit := false
	for _, ed := range result.Edits {
		if strings.Contains(ed.NewText, "output.grand_total") {
			hasResponseEdit = true
		}
	}
	if !hasResponseEdit {
		t.Error("expected response block to update output.total → output.grand_total")
	}
}

func TestRenameFieldSameName(t *testing.T) {
	e := NewEngine("")
	result := e.RenameField("any", "email", "email")
	if result != nil {
		t.Error("expected nil when old and new name are the same")
	}
}

func TestRenameFieldNotFound(t *testing.T) {
	e := NewEngine("")
	e.index.updateFile(parseHCL("flows/test.mycel", []byte(`
flow "test" {
  from { connector = "api" }
  transform {
    name = "input.name"
  }
  to { connector = "db" }
}
`)))

	result := e.RenameField("test", "nonexistent", "new_name")
	if result != nil {
		t.Error("expected nil when field doesn't exist")
	}
}

func TestReplaceSQLIdentifier(t *testing.T) {
	tests := []struct {
		sql, old, new, expected string
	}{
		{
			"INSERT INTO t (email, name) VALUES (:email, :name)",
			"email", "user_email",
			"INSERT INTO t (user_email, name) VALUES (:user_email, :name)",
		},
		{
			"UPDATE t SET email = :email WHERE email_verified = true",
			"email", "user_email",
			// "email_verified" should NOT be renamed (it's a different identifier)
			"UPDATE t SET user_email = :user_email WHERE email_verified = true",
		},
		{
			"INSERT INTO t (emails) VALUES (:emails) ON DUPLICATE KEY UPDATE emails = :emails",
			"emails", "email_list",
			"INSERT INTO t (email_list) VALUES (:email_list) ON DUPLICATE KEY UPDATE email_list = :email_list",
		},
	}

	for _, tt := range tests {
		result := replaceInSQL(tt.sql, tt.old, tt.new)
		if result != tt.expected {
			t.Errorf("replaceInSQL(%q, %q, %q)\n  got:    %q\n  expect: %q", tt.sql, tt.old, tt.new, result, tt.expected)
		}
	}
}
