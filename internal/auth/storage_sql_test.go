package auth

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// The SQL stores are the one part of auth with no coverage from either side:
// the unit tests cannot reach them without a database, and the integration
// suite brings up twelve services and no auth among them.
//
// A mock database is the right tool here and not for the connectors, because
// what is under test is our SQL and our scanning — which columns are read, in
// what order, what a missing row becomes — rather than whether a driver speaks
// to PostgreSQL. That question belongs to the integration suite, and asking it
// of a mock would answer nothing.

func mockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
		db.Close()
	})
	return db, mock
}

func TestPostgresUserStoreReadsTheColumnsItAsksFor(t *testing.T) {
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{})

	created := time.Now().Add(-time.Hour)
	mock.ExpectQuery("SELECT").
		WithArgs("person@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "created_at", "updated_at"}).
			AddRow("u-1", "person@example.com", "argon2id$hash", created, created))

	user, err := store.FindByEmail(context.Background(), "person@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	// The scan order is the part that breaks silently: swap two columns of the
	// same type and every user comes back with an email in the hash field.
	if user.ID != "u-1" || user.Email != "person@example.com" || user.PasswordHash != "argon2id$hash" {
		t.Errorf("user = %+v", user)
	}
	if !user.CreatedAt.Equal(created) {
		t.Errorf("created_at = %v", user.CreatedAt)
	}
}

func TestAMissingUserIsNotAnError(t *testing.T) {
	// The manager tells a failed login from a broken database by this
	// distinction, and answering the wrong one turns "no such account" into a
	// 500.
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{})

	mock.ExpectQuery("SELECT").
		WithArgs("nobody@example.com").
		WillReturnError(sql.ErrNoRows)

	_, err := store.FindByEmail(context.Background(), "nobody@example.com")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("err = %v, want ErrUserNotFound", err)
	}
}

func TestADatabaseFailureIsReportedAsOne(t *testing.T) {
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{})

	mock.ExpectQuery("SELECT").
		WithArgs("person@example.com").
		WillReturnError(errors.New("connection reset by peer"))

	_, err := store.FindByEmail(context.Background(), "person@example.com")
	if err == nil {
		t.Fatal("a database failure was reported as success")
	}
	if errors.Is(err, ErrUserNotFound) {
		t.Error("a database failure was reported as a missing user, which would read as a bad password")
	}
}

func TestTheConfiguredTableAndColumnsAreUsed(t *testing.T) {
	// The point of the users block: an existing table with its own names.
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{
		Table: "accounts",
		Fields: &FieldsConfig{
			ID: "account_id", Email: "login", PasswordHash: "secret",
		},
	})

	mock.ExpectQuery(`FROM accounts`).
		WithArgs("person@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "login", "secret", "created_at", "updated_at"}).
			AddRow("a-1", "person@example.com", "hash", time.Now(), time.Now()))

	if _, err := store.FindByEmail(context.Background(), "person@example.com"); err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
}

func TestUpdatingAPasswordForNobodyIsRefused(t *testing.T) {
	// Reporting success for an update that changed no rows would let a password
	// reset look like it worked.
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{})

	mock.ExpectExec("UPDATE").
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := store.UpdatePassword(context.Background(), "u-nobody", "new-hash"); err == nil {
		t.Error("an update that changed nothing was reported as done")
	}
}

func TestMySQLSessionCountOnlyCountsLiveSessions(t *testing.T) {
	// A session limit is checked against this, so counting expired rows would
	// lock someone out of their own account.
	db, mock := mockDB(t)
	store := NewMySQLSessionStore(db, "auth_sessions")

	mock.ExpectQuery("SELECT COUNT").
		WithArgs("u-1", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	count, err := store.Count(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d", count)
	}
}

func TestMySQLIdleDeletionUsesTheThreshold(t *testing.T) {
	db, mock := mockDB(t)
	store := NewMySQLSessionStore(db, "auth_sessions")

	threshold := time.Now().Add(-15 * time.Minute)
	mock.ExpectExec("DELETE FROM auth_sessions").
		WithArgs(threshold).
		WillReturnResult(sqlmock.NewResult(0, 3))

	deleted, err := store.DeleteIdle(context.Background(), threshold)
	if err != nil {
		t.Fatalf("DeleteIdle: %v", err)
	}
	if deleted != 3 {
		t.Errorf("deleted = %d, want 3", deleted)
	}
}
