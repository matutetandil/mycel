package auth

import (
	"testing"
)

func TestMySQLUserStoreFields(t *testing.T) {
	t.Run("default fields", func(t *testing.T) {
		store := NewMySQLUserStore(nil, &UsersConfig{})

		fields := store.getFields()
		if fields.ID != "id" {
			t.Errorf("expected ID field 'id', got %q", fields.ID)
		}
		if fields.Email != "email" {
			t.Errorf("expected Email field 'email', got %q", fields.Email)
		}
		if fields.PasswordHash != "password_hash" {
			t.Errorf("expected PasswordHash field 'password_hash', got %q", fields.PasswordHash)
		}
	})

	t.Run("custom fields", func(t *testing.T) {
		store := NewMySQLUserStore(nil, &UsersConfig{
			Table: "custom_users",
			Fields: &UserFieldsConfig{
				ID:           "user_id",
				Email:        "user_email",
				PasswordHash: "pwd_hash",
				CreatedAt:    "creation_date",
				UpdatedAt:    "modification_date",
			},
		})

		if store.getTableName() != "custom_users" {
			t.Errorf("expected table 'custom_users', got %q", store.getTableName())
		}

		fields := store.getFields()
		if fields.ID != "user_id" {
			t.Errorf("expected ID field 'user_id', got %q", fields.ID)
		}
	})

	t.Run("default table name", func(t *testing.T) {
		store := NewMySQLUserStore(nil, &UsersConfig{})

		if store.getTableName() != "users" {
			t.Errorf("expected default table 'users', got %q", store.getTableName())
		}
	})
}

func TestMySQLPasswordHistoryStore(t *testing.T) {
	t.Run("default table name", func(t *testing.T) {
		store := NewMySQLPasswordHistoryStore(nil, "")
		if store.table != "password_history" {
			t.Errorf("expected default table 'password_history', got %q", store.table)
		}
	})

	t.Run("custom table name", func(t *testing.T) {
		store := NewMySQLPasswordHistoryStore(nil, "custom_pwd_history")
		if store.table != "custom_pwd_history" {
			t.Errorf("expected table 'custom_pwd_history', got %q", store.table)
		}
	})
}

func TestMySQLAuditStore(t *testing.T) {
	t.Run("default table name", func(t *testing.T) {
		store := NewMySQLAuditStore(nil, "", nil)
		if store.table != "auth_audit_log" {
			t.Errorf("expected default table 'auth_audit_log', got %q", store.table)
		}
	})

	t.Run("custom table and events", func(t *testing.T) {
		events := []string{"login", "logout"}
		store := NewMySQLAuditStore(nil, "security_log", events)
		if store.table != "security_log" {
			t.Errorf("expected table 'security_log', got %q", store.table)
		}
		if len(store.events) != 2 {
			t.Errorf("expected 2 events, got %d", len(store.events))
		}
	})
}

func TestMySQLSessionStore(t *testing.T) {
	t.Run("default table name", func(t *testing.T) {
		store := NewMySQLSessionStore(nil, "")
		if store.table != "auth_sessions" {
			t.Errorf("expected default table 'auth_sessions', got %q", store.table)
		}
	})

	t.Run("custom table name", func(t *testing.T) {
		store := NewMySQLSessionStore(nil, "user_sessions")
		if store.table != "user_sessions" {
			t.Errorf("expected table 'user_sessions', got %q", store.table)
		}
	})
}

func TestMySQLTokenStore(t *testing.T) {
	t.Run("default table name", func(t *testing.T) {
		store := NewMySQLTokenStore(nil, "")
		if store.table != "auth_tokens" {
			t.Errorf("expected default table 'auth_tokens', got %q", store.table)
		}
	})

	t.Run("custom table name", func(t *testing.T) {
		store := NewMySQLTokenStore(nil, "jwt_tokens")
		if store.table != "jwt_tokens" {
			t.Errorf("expected table 'jwt_tokens', got %q", store.table)
		}
	})
}

func TestIsMySQLUniqueViolation(t *testing.T) {
	tests := []struct {
		name     string
		errStr   string
		expected bool
	}{
		{
			name:     "duplicate entry error",
			errStr:   "Error 1062: Duplicate entry 'test@test.com' for key 'email'",
			expected: true,
		},
		{
			name:     "error code 1062",
			errStr:   "1062",
			expected: true,
		},
		{
			name:     "unique constraint",
			errStr:   "unique constraint violated",
			expected: true,
		},
		{
			name:     "generic error",
			errStr:   "connection refused",
			expected: false,
		},
		{
			name:     "syntax error",
			errStr:   "You have an error in your SQL syntax",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &testError{msg: tt.errStr}
			result := isMySQLUniqueViolation(err)
			if result != tt.expected {
				t.Errorf("isMySQLUniqueViolation(%q) = %v, want %v", tt.errStr, result, tt.expected)
			}
		})
	}
}

// testError is a simple error implementation for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
