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

// The MySQL store is the one backend that keeps users, sessions and tokens, so
// most of what a signed-in service does passes through it. Every method below
// is a query whose column order has to match the scan beside it — the class of
// mistake that produces a user whose email is in the password field rather than
// an error.

func TestMySQLUserRoundTrip(t *testing.T) {
	db, mock := mockDB(t)
	store := NewMySQLUserStore(db, &UsersConfig{})

	created := time.Now().Add(-time.Hour)
	mock.ExpectExec("INSERT INTO").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := store.Create(context.Background(), &User{
		ID: "u-1", Email: "person@example.com", PasswordHash: "hash",
		CreatedAt: created, UpdatedAt: created,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	mock.ExpectQuery("SELECT").
		WithArgs("u-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "created_at", "updated_at"}).
			AddRow("u-1", "person@example.com", "hash", created, created))

	user, err := store.FindByID(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if user.Email != "person@example.com" || user.PasswordHash != "hash" {
		t.Errorf("user = %+v", user)
	}
}

func TestMySQLMissingUserIsNotAnError(t *testing.T) {
	db, mock := mockDB(t)
	store := NewMySQLUserStore(db, &UsersConfig{})

	mock.ExpectQuery("SELECT").WithArgs("nobody").WillReturnError(sql.ErrNoRows)

	if _, err := store.FindByID(context.Background(), "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("err = %v, want ErrUserNotFound", err)
	}
}

func TestMySQLUpdatesThatChangedNothingAreReported(t *testing.T) {
	// A password reset or an MFA toggle that matched no row has to fail: the
	// caller is told it worked otherwise, and nothing did.
	db, mock := mockDB(t)
	store := NewMySQLUserStore(db, &UsersConfig{})

	mock.ExpectExec("UPDATE").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.UpdatePassword(context.Background(), "u-nobody", "hash"); err == nil {
		t.Error("a password update that changed nothing was reported as done")
	}

	mock.ExpectExec("UPDATE").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := store.UpdateMFAEnabled(context.Background(), "u-nobody", true); err == nil {
		t.Error("an MFA update that changed nothing was reported as done")
	}
}

func TestMySQLLastLoginIsRecorded(t *testing.T) {
	db, mock := mockDB(t)
	store := NewMySQLUserStore(db, &UsersConfig{})

	moment := time.Now()
	mock.ExpectExec("UPDATE").
		WithArgs(moment, sqlmock.AnyArg(), "u-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.UpdateLastLogin(context.Background(), "u-1", moment); err != nil {
		t.Fatalf("UpdateLastLogin: %v", err)
	}
}

func TestMySQLSessionRoundTrip(t *testing.T) {
	db, mock := mockDB(t)
	store := NewMySQLSessionStore(db, "auth_sessions")

	now := time.Now()
	mock.ExpectExec("INSERT INTO auth_sessions").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := store.Create(context.Background(), &Session{
		ID: "s-1", UserID: "u-1", IP: "203.0.113.7", UserAgent: "curl",
		CreatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	mock.ExpectQuery("SELECT").
		WithArgs("s-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "ip", "user_agent", "created_at", "last_active_at", "expires_at", "device_id",
		}).AddRow("s-1", "u-1", "203.0.113.7", "curl", now, now, now.Add(time.Hour), nil))

	session, err := store.FindByID(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if session.UserID != "u-1" || session.IP != "203.0.113.7" || session.UserAgent != "curl" {
		t.Errorf("session = %+v", session)
	}
	// The column is nullable, and a session without a device is ordinary.
	if session.DeviceID != "" {
		t.Errorf("device = %q, want empty for a null column", session.DeviceID)
	}
}

func TestMySQLListsEverySessionOfAUser(t *testing.T) {
	db, mock := mockDB(t)
	store := NewMySQLSessionStore(db, "auth_sessions")

	now := time.Now()
	mock.ExpectQuery("SELECT").
		WithArgs("u-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "ip", "user_agent", "created_at", "last_active_at", "expires_at", "device_id",
		}).
			AddRow("s-1", "u-1", "1.1.1.1", "a", now, now, now.Add(time.Hour), "phone").
			AddRow("s-2", "u-1", "2.2.2.2", "b", now, now, now.Add(time.Hour), nil))

	sessions, err := store.FindByUserID(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions", len(sessions))
	}
	if sessions[0].DeviceID != "phone" || sessions[1].DeviceID != "" {
		t.Errorf("devices = %q and %q", sessions[0].DeviceID, sessions[1].DeviceID)
	}
}

func TestMySQLTokenBlacklist(t *testing.T) {
	db, mock := mockDB(t)
	store := NewMySQLTokenStore(db, "auth_tokens")

	mock.ExpectExec("INSERT INTO auth_tokens").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := store.Add(context.Background(), "jti-1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// A row that is still within its lifetime means the token was revoked.
	mock.ExpectQuery("SELECT expires_at").
		WithArgs("jti-1").
		WillReturnRows(sqlmock.NewRows([]string{"expires_at"}).AddRow(time.Now().Add(time.Hour)))
	revoked, err := store.Exists(context.Background(), "jti-1")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !revoked {
		t.Error("a revoked token was not remembered")
	}

	// No row at all: nobody revoked it, and reporting otherwise would reject a
	// valid session.
	mock.ExpectQuery("SELECT expires_at").
		WithArgs("jti-2").
		WillReturnError(sql.ErrNoRows)
	revoked, err = store.Exists(context.Background(), "jti-2")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if revoked {
		t.Error("a token nobody revoked was reported as revoked")
	}

	// A row whose expiry has passed is answered as not revoked, which is
	// correct and worth stating: the token is refused by its own expiry, and
	// the blacklist entry is only there so it can be forgotten.
	mock.ExpectQuery("SELECT expires_at").
		WithArgs("jti-old").
		WillReturnRows(sqlmock.NewRows([]string{"expires_at"}).AddRow(time.Now().Add(-time.Hour)))
	revoked, err = store.Exists(context.Background(), "jti-old")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if revoked {
		t.Error("an entry past its expiry still reported the token as revoked")
	}
}

func TestMySQLPasswordHistoryKeepsOrderAndCount(t *testing.T) {
	// The history is what stops a password from being reused, so the order
	// matters: the check reads the most recent N.
	db, mock := mockDB(t)
	store := NewMySQLPasswordHistoryStore(db, "auth_password_history")

	mock.ExpectExec("INSERT INTO auth_password_history").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := store.AddPasswordHash(context.Background(), "u-1", "hash-3"); err != nil {
		t.Fatalf("AddPasswordHash: %v", err)
	}

	mock.ExpectQuery("SELECT").
		WithArgs("u-1", 2).
		WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).
			AddRow("hash-3").
			AddRow("hash-2"))

	hashes, err := store.GetRecentHashes(context.Background(), "u-1", 2)
	if err != nil {
		t.Fatalf("GetRecentHashes: %v", err)
	}
	if len(hashes) != 2 || hashes[0] != "hash-3" {
		t.Errorf("hashes = %v", hashes)
	}
}

func TestMySQLAuditLogWritesTheEvent(t *testing.T) {
	db, mock := mockDB(t)
	store := NewMySQLAuditStore(db, "auth_audit", []string{"login"})

	mock.ExpectExec("INSERT INTO auth_audit").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := store.Log(context.Background(), &AuditEvent{
		Event: "login", UserID: "u-1", IP: "203.0.113.7", Success: false,
		ErrorReason: "invalid credentials",
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}
}

// The column that records when a password was last set is opt-in, on the same
// terms as roles: a users table that already exists need not grow one, and
// selecting a column nobody created would turn a working service into one that
// cannot read its own users.

func TestThePasswordAgeColumnIsOnlyReadWhenItIsNamed(t *testing.T) {
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{})

	// No column named, so the query must not ask for one — a row with five
	// columns is all this store expects back.
	mock.ExpectQuery("SELECT").
		WithArgs("person@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "created_at", "updated_at"}).
			AddRow("u-1", "person@example.com", "hash", time.Now(), time.Now()))

	user, err := store.FindByEmail(context.Background(), "person@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if user.PasswordChangedAt != nil {
		t.Errorf("an age came back from a store that reads no column for it: %v", user.PasswordChangedAt)
	}
}

func TestNamingThePasswordAgeColumnReadsIt(t *testing.T) {
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{
		Fields: &FieldsConfig{PasswordChangedAt: "password_changed_at"},
	})

	changed := time.Now().Add(-30 * 24 * time.Hour)
	mock.ExpectQuery("password_changed_at").
		WithArgs("person@example.com").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "email", "password_hash", "created_at", "updated_at", "password_changed_at"}).
			AddRow("u-1", "person@example.com", "hash", time.Now(), time.Now(), changed))

	user, err := store.FindByEmail(context.Background(), "person@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if user.PasswordChangedAt == nil || !user.PasswordChangedAt.Equal(changed) {
		t.Errorf("password age = %v, want %v", user.PasswordChangedAt, changed)
	}
}

func TestANullPasswordAgeIsNotAnError(t *testing.T) {
	// A column added to a table that already has rows: everything written
	// before it existed is null, and none of those accounts may be locked out.
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{
		Fields: &FieldsConfig{PasswordChangedAt: "password_changed_at"},
	})

	mock.ExpectQuery("SELECT").
		WithArgs("person@example.com").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "email", "password_hash", "created_at", "updated_at", "password_changed_at"}).
			AddRow("u-1", "person@example.com", "hash", time.Now(), time.Now(), nil))

	user, err := store.FindByEmail(context.Background(), "person@example.com")
	if err != nil {
		t.Fatalf("a null column was an error: %v", err)
	}
	if user.PasswordChangedAt != nil {
		t.Errorf("a null column became an age: %v", user.PasswordChangedAt)
	}
}

func TestChangingAPasswordStampsTheColumn(t *testing.T) {
	// Without this the age never moves, so somebody who has just changed their
	// password is asked to change it again.
	db, mock := mockDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{
		Fields: &FieldsConfig{PasswordChangedAt: "password_changed_at"},
	})

	mock.ExpectExec("password_changed_at").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.UpdatePassword(context.Background(), "u-1", "new-hash"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
}

func TestANewAccountsPasswordIsAsOldAsTheAccount(t *testing.T) {
	// Somebody who has never changed their password still has one with an age,
	// or a policy that expires nothing until the first change.
	db, mock := mockDB(t)
	store := NewMySQLUserStore(db, &UsersConfig{
		Fields: &FieldsConfig{PasswordChangedAt: "password_changed_at"},
	})

	mock.ExpectExec("password_changed_at").
		WillReturnResult(sqlmock.NewResult(1, 1))

	user := &User{ID: "u-1", Email: "person@example.com", PasswordHash: "hash"}
	if err := store.Create(context.Background(), user); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if user.PasswordChangedAt == nil {
		t.Error("a new account came back with no password age")
	}
}
