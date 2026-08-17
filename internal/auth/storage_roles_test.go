package auth

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// Roles decide authorization: the middleware matches them against a path rule,
// the JWT carries them to every service downstream, and a flow reads them as
// auth.roles. Neither SQL store wrote or read them, so a service that kept its
// users in memory had roles and the same service pointed at a database got
// every user back with an empty list — no error anywhere, because an empty list
// is not one.

func TestRolesAreLeftAloneUntilAColumnIsNamed(t *testing.T) {
	// A users table that already exists must keep working. Selecting a column
	// nobody created would turn a running service into one that cannot read its
	// own users, so the column is opt-in.
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{})

	mock.ExpectQuery(`SELECT id, email, password_hash, created_at, updated_at FROM users`).
		WithArgs("u-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "created_at", "updated_at"}).
			AddRow("u-1", "someone@example.com", "hash", time.Now(), time.Now()))

	user, err := store.GetByID(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(user.Roles) != 0 {
		t.Errorf("roles = %v, want none read when no column is named", user.Roles)
	}
}

func TestANamedColumnIsReadIntoTheUsersRoles(t *testing.T) {
	for name, tc := range map[string]struct {
		stored string
		want   []string
	}{
		"a JSON array, which is what we write":     {`["admin","support"]`, []string{"admin", "support"}},
		"a comma-separated list somebody else has": {"admin, support", []string{"admin", "support"}},
		"a single role":   {"admin", []string{"admin"}},
		"an empty column": {"", nil},
		"an empty array":  {"[]", nil},
	} {
		t.Run(name, func(t *testing.T) {
			db, mock := mockDB(t)
			store := NewPostgresUserStore(db, &UsersConfig{
				Fields: &FieldsConfig{
					ID: "id", Email: "email", PasswordHash: "password_hash",
					CreatedAt: "created_at", UpdatedAt: "updated_at", Roles: "roles",
				},
			})

			mock.ExpectQuery(`SELECT id, email, password_hash, created_at, updated_at, roles FROM users`).
				WithArgs("someone@example.com").
				WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "created_at", "updated_at", "roles"}).
					AddRow("u-1", "someone@example.com", "hash", time.Now(), time.Now(), tc.stored))

			user, err := store.GetByEmail(context.Background(), "someone@example.com")
			if err != nil {
				t.Fatalf("GetByEmail: %v", err)
			}
			if len(user.Roles) != len(tc.want) {
				t.Fatalf("roles = %v, want %v", user.Roles, tc.want)
			}
			for i, role := range tc.want {
				if user.Roles[i] != role {
					t.Errorf("roles = %v, want %v", user.Roles, tc.want)
				}
			}
		})
	}
}

func TestANullRolesColumnIsAUserWithNoRoles(t *testing.T) {
	// Not an error: a row written before the column existed has null in it.
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{
		Fields: &FieldsConfig{
			ID: "id", Email: "email", PasswordHash: "password_hash",
			CreatedAt: "created_at", UpdatedAt: "updated_at", Roles: "roles",
		},
	})

	mock.ExpectQuery(`SELECT`).WithArgs("u-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "created_at", "updated_at", "roles"}).
			AddRow("u-1", "someone@example.com", "hash", time.Now(), time.Now(), nil))

	user, err := store.GetByID(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("a null roles column failed the read: %v", err)
	}
	if len(user.Roles) != 0 {
		t.Errorf("roles = %v, want none", user.Roles)
	}
}

func TestRolesAreWrittenWhenAUserIsCreated(t *testing.T) {
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{
		Fields: &FieldsConfig{
			ID: "id", Email: "email", PasswordHash: "password_hash",
			CreatedAt: "created_at", UpdatedAt: "updated_at", Roles: "roles",
		},
	})

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users (id, email, password_hash, created_at, updated_at, roles)`)).
		WithArgs("u-1", "someone@example.com", "hash", sqlmock.AnyArg(), sqlmock.AnyArg(), `["admin","support"]`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := store.Create(context.Background(), &User{
		ID: "u-1", Email: "someone@example.com", PasswordHash: "hash",
		Roles: []string{"admin", "support"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestRolesAreNotWrittenWhenNoColumnIsNamed(t *testing.T) {
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{})

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users (id, email, password_hash, created_at, updated_at)`)).
		WithArgs("u-1", "someone@example.com", "hash", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := store.Create(context.Background(), &User{
		ID: "u-1", Email: "someone@example.com", PasswordHash: "hash",
		Roles: []string{"admin"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestChangingAUsersRolesIsPersisted(t *testing.T) {
	// Otherwise a promotion or a revocation is accepted and lost, which for a
	// revocation means the old roles keep working.
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{
		Fields: &FieldsConfig{
			ID: "id", Email: "email", PasswordHash: "password_hash",
			CreatedAt: "created_at", UpdatedAt: "updated_at", Roles: "roles",
		},
	})

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET email = $1, password_hash = $2, updated_at = $3, roles = $4 WHERE id = $5`)).
		WithArgs("someone@example.com", "hash", sqlmock.AnyArg(), `["support"]`, "u-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.Update(context.Background(), &User{
		ID: "u-1", Email: "someone@example.com", PasswordHash: "hash", Roles: []string{"support"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
}

func TestRemovingEveryRoleIsPersistedRatherThanIgnored(t *testing.T) {
	// The dangerous direction: an empty list has to overwrite what is stored,
	// or revoking someone's last role leaves them with it.
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{
		Fields: &FieldsConfig{
			ID: "id", Email: "email", PasswordHash: "password_hash",
			CreatedAt: "created_at", UpdatedAt: "updated_at", Roles: "roles",
		},
	})

	mock.ExpectExec(`UPDATE users SET`).
		WithArgs("someone@example.com", "hash", sqlmock.AnyArg(), "[]", "u-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := store.Update(context.Background(), &User{
		ID: "u-1", Email: "someone@example.com", PasswordHash: "hash", Roles: nil,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
}

func TestTheMySQLStoreAgreesWithThePostgresOne(t *testing.T) {
	// The two are written separately and have drifted before; a service that
	// gets roles on one database and not the other is the worst version of
	// this bug, because it works in development.
	db, mock := mockDB(t)
	store := NewMySQLUserStore(db, &UsersConfig{
		Fields: &FieldsConfig{
			ID: "id", Email: "email", PasswordHash: "password_hash",
			CreatedAt: "created_at", UpdatedAt: "updated_at", Roles: "roles",
		},
	})

	mock.ExpectQuery(`SELECT id, email, password_hash, created_at, updated_at, roles FROM users`).
		WithArgs("u-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "created_at", "updated_at", "roles"}).
			AddRow("u-1", "someone@example.com", "hash", time.Now(), time.Now(), `["admin"]`))

	user, err := store.GetByID(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(user.Roles) != 1 || user.Roles[0] != "admin" {
		t.Errorf("roles = %v, want the same as PostgreSQL reads", user.Roles)
	}

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users (id, email, password_hash, created_at, updated_at, roles)`)).
		WithArgs("u-2", "other@example.com", "hash", sqlmock.AnyArg(), sqlmock.AnyArg(), `["support"]`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := store.Create(context.Background(), &User{
		ID: "u-2", Email: "other@example.com", PasswordHash: "hash", Roles: []string{"support"},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestARoundTripThroughStorageKeepsTheRoles(t *testing.T) {
	// The encoding and decoding are a pair, so they are checked as one: what
	// goes in is what comes back, including the awkward cases.
	for _, roles := range [][]string{
		{"admin"},
		{"admin", "support", "billing"},
		{"role with spaces"},
		{"role,with,commas"},
		nil,
	} {
		got := decodeRoles(encodeRoles(roles))
		if len(got) != len(roles) {
			t.Errorf("%v came back as %v", roles, got)
			continue
		}
		for i := range roles {
			if got[i] != roles[i] {
				t.Errorf("%v came back as %v", roles, got)
				break
			}
		}
	}
}

func TestRolesReachTheTokenTheServiceIssues(t *testing.T) {
	// The end of the chain: what a store reads has to arrive in the claims,
	// since that is what every service downstream decides on.
	tm := hmacManager(t)
	pair, err := tm.GenerateTokenPair(&User{
		ID: "u-1", Email: "someone@example.com", Roles: []string{"admin", "support"},
	}, "s", nil)
	if err != nil {
		t.Fatalf("GenerateTokenPair: %v", err)
	}

	claims, err := tm.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if len(claims.Roles) != 2 || claims.Roles[0] != "admin" {
		t.Errorf("claims carried roles %v", claims.Roles)
	}
}
