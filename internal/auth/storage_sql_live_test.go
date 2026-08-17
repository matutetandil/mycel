package auth

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// The account stores that speak SQL.
//
// They map onto a table somebody already has rather than creating one, and
// they speak Postgres or MySQL — so nothing a unit test can stand up reaches
// them. These run against the databases in the integration stack, and skip
// themselves when there is none, the way the RabbitMQ tests do.
//
// What is checked is the part a fake cannot: that the statements are ones the
// server accepts, that a placeholder style matches the driver, and that a
// column scans into the field it is meant for.

func sqlStore(t *testing.T, driver, env, fallback string) *sql.DB {
	t.Helper()

	dsn := os.Getenv(env)
	if dsn == "" {
		dsn = fallback
	}

	// Reachable at all? A dial is cheaper and clearer than a failed query.
	host := hostOf(driver, dsn)
	conn, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		t.Skipf("no %s reachable at %s (set %s to enable): %v", driver, host, env, err)
	}
	_ = conn.Close()

	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatalf("open %s: %v", driver, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("%s is not answering: %v", driver, err)
	}
	return db
}

func hostOf(driver, dsn string) string {
	if driver == "mysql" {
		// user:pass@tcp(host:port)/db
		start := len("mycel:mycel@tcp(")
		if len(dsn) > start {
			if end := indexByte(dsn[start:], ')'); end > 0 {
				return dsn[start : start+end]
			}
		}
		return "127.0.0.1:33306"
	}
	return "127.0.0.1:55432"
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func postgresDB(t *testing.T) *sql.DB {
	return sqlStore(t, "pgx", "MYCEL_TEST_POSTGRES_DSN",
		"postgres://mycel:mycel@127.0.0.1:55432/mycel_test?sslmode=disable")
}

func mysqlDB(t *testing.T) *sql.DB {
	return sqlStore(t, "mysql", "MYCEL_TEST_MYSQL_DSN",
		"mycel:mycel@tcp(127.0.0.1:33306)/mycel_test?parseTime=true")
}

// account builds a user with everything filled in, so every column is written
// and read back.
func liveAccount(id string) *User {
	return &User{
		ID:           id,
		Email:        id + "@example.com",
		PasswordHash: "$argon2id$v=19$m=65536,t=3,p=2$abc$def",
		Roles:        []string{"admin", "engineer"},
		Metadata:     map[string]interface{}{"team": "waterworks"},
		CreatedAt:    time.Now().UTC().Truncate(time.Second),
		UpdatedAt:    time.Now().UTC().Truncate(time.Second),
	}
}

func runUserStoreTests(t *testing.T, store UserStore) {
	ctx := context.Background()
	id := fmt.Sprintf("u-%d", time.Now().UnixNano())
	user := liveAccount(id)

	if err := store.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(ctx, id) })

	// Every column, read back: a field that does not survive the round trip is
	// one the service quietly forgets — roles being the one that decides what
	// somebody can do.
	got, err := store.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Email != user.Email {
		t.Errorf("account = %+v", got)
	}
	if got.PasswordHash != user.PasswordHash {
		t.Errorf("the password hash did not survive: %q", got.PasswordHash)
	}
	if len(got.Roles) != 2 {
		t.Errorf("roles = %v, want both — this is what authorises anything", got.Roles)
	}

	// Signing in looks the account up by address.
	byEmail, err := store.FindByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if byEmail.ID != id {
		t.Errorf("the address found %q", byEmail.ID)
	}

	// An account nobody registered.
	if _, err := store.FindByEmail(ctx, "nobody-"+id+"@example.com"); err == nil {
		t.Error("an account nobody registered was found")
	}
	if _, err := store.FindByID(ctx, "an-id-nobody-has"); err == nil {
		t.Error("an id nobody has was found")
	}

	// Changing the password is the operation that has to stick, since the old
	// one must stop working.
	if err := store.UpdatePassword(ctx, id, "$argon2id$new"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	got, err = store.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.PasswordHash != "$argon2id$new" {
		t.Errorf("the new password did not stick: %q", got.PasswordHash)
	}

	// And the rest of what a sign-in writes.
	if err := store.UpdateLastLogin(ctx, id, time.Now()); err != nil {
		t.Errorf("UpdateLastLogin: %v", err)
	}

	user.Roles = []string{"admin"}
	if err := store.Update(ctx, user); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = store.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if len(got.Roles) != 1 {
		t.Errorf("the update did not stick: %+v", got)
	}

	// Registering the same address twice is refused by the database, which is
	// the only place that can be enforced.
	if err := store.Create(ctx, liveAccount(id)); err == nil {
		t.Error("the same account was created twice")
	}

	if err := store.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.FindByID(ctx, id); err == nil {
		t.Error("a deleted account is still there")
	}
}

// withRoles names the column roles live in. Naming one is what turns roles on
// for a SQL store: without it the column is neither written nor read, so a
// users table somebody already has keeps working. Both stores are exercised
// both ways below, because both are real configurations.
func withRoles() *UsersConfig {
	return &UsersConfig{Table: "auth_users", Fields: &FieldsConfig{Roles: "roles"}}
}

func TestAccountsInPostgres(t *testing.T) {
	db := postgresDB(t)
	runUserStoreTests(t, NewPostgresUserStore(db, withRoles()))
}

func TestAccountsInMySQL(t *testing.T) {
	db := mysqlDB(t)
	runUserStoreTests(t, NewMySQLUserStore(db, withRoles()))
}

func TestATableWithNoRolesColumnStillWorks(t *testing.T) {
	// The reason roles are opt-in: pointing auth at a users table that
	// somebody else owns must not fail on a column it does not have.
	db := postgresDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{Table: "auth_users"})
	ctx := context.Background()

	id := fmt.Sprintf("u-noroles-%d", time.Now().UnixNano())
	user := liveAccount(id)
	if err := store.Create(ctx, user); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(ctx, id) })

	got, err := store.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Email != user.Email {
		t.Errorf("account = %+v", got)
	}
	// Roles are absent rather than empty-because-of-an-error, which is the
	// distinction that makes this safe.
	if len(got.Roles) != 0 {
		t.Errorf("roles = %v, want none read from a table nobody named a column in", got.Roles)
	}
}

func TestSessionsInMySQL(t *testing.T) {
	// MySQL is the one with a session store of its own; Postgres keeps
	// sessions in memory, which is recorded by the warning at startup.
	db := mysqlDB(t)
	store := NewMySQLSessionStore(db, "auth_sessions")
	ctx := context.Background()

	userID := fmt.Sprintf("u-%d", time.Now().UnixNano())
	now := time.Now().UTC().Truncate(time.Second)

	for _, id := range []string{userID + "-s1", userID + "-s2"} {
		if err := store.Create(ctx, &Session{
			ID: id, UserID: userID, IP: "10.0.0.1", UserAgent: "browser",
			CreatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	t.Cleanup(func() { _ = store.DeleteByUserID(ctx, userID) })

	got, err := store.FindByID(ctx, userID+"-s1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.UserID != userID || got.IP != "10.0.0.1" {
		t.Errorf("session = %+v", got)
	}

	// The account's own list, which is what "signed in on two devices" reads
	// and what a limit on concurrent sessions counts.
	open, err := store.FindByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if len(open) != 2 {
		t.Errorf("%d sessions, want both", len(open))
	}

	count, err := store.Count(ctx, userID)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d", count)
	}

	if err := store.Delete(ctx, userID+"-s1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.FindByID(ctx, userID+"-s1"); err == nil {
		t.Error("a session that was ended is still there")
	}

	if err := store.DeleteByUserID(ctx, userID); err != nil {
		t.Fatalf("DeleteByUserID: %v", err)
	}
	if count, _ := store.Count(ctx, userID); count != 0 {
		t.Errorf("%d sessions left after signing out everywhere", count)
	}
}

func TestRevokedTokensInMySQL(t *testing.T) {
	// A JWT is valid until it expires whatever anybody thinks, so this list is
	// the only way to stop one early.
	db := mysqlDB(t)
	store := NewMySQLTokenStore(db, "auth_tokens")
	ctx := context.Background()

	jti := fmt.Sprintf("jti-%d", time.Now().UnixNano())

	listed, err := store.Exists(ctx, jti)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if listed {
		t.Error("a token nobody revoked is on the list")
	}

	if err := store.Add(ctx, jti, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(ctx, jti) })

	listed, err = store.Exists(ctx, jti)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !listed {
		t.Error("a revoked token is not on the list, so it still works")
	}

	// Revoking the same one twice is what a retry does, and it must not fail.
	if err := store.Add(ctx, jti, time.Now().Add(2*time.Hour)); err != nil {
		t.Errorf("revoking twice failed: %v", err)
	}

	// An entry only has to outlive the token it refers to; the sweep is what
	// stops the table growing for ever.
	if err := store.Cleanup(ctx); err != nil {
		t.Errorf("Cleanup: %v", err)
	}
	if listed, _ := store.Exists(ctx, jti); !listed {
		t.Error("the sweep removed an entry whose token has not expired")
	}

	// And one that has.
	expired := jti + "-expired"
	if err := store.Add(ctx, expired, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := store.Cleanup(ctx); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if listed, _ := store.Exists(ctx, expired); listed {
		t.Error("an entry for a token that has expired is still there")
	}
}

func TestNamingOneColumnDoesNotEmptyTheRest(t *testing.T) {
	// The block was taken whole, so naming one column left the others blank —
	// and the ordinary reason to write it at all is to turn roles on, which
	// produced INSERT INTO users (, , , , , roles) and a syntax error on the
	// first registration, from a configuration that reads exactly like the
	// documentation.
	db := postgresDB(t)
	store := NewPostgresUserStore(db, &UsersConfig{
		Table:  "auth_users",
		Fields: &FieldsConfig{Roles: "roles"},
	})
	ctx := context.Background()

	id := fmt.Sprintf("u-onecolumn-%d", time.Now().UnixNano())
	if err := store.Create(ctx, liveAccount(id)); err != nil {
		t.Fatalf("naming only the roles column broke every other one: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(ctx, id) })

	got, err := store.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Email == "" || got.PasswordHash == "" {
		t.Errorf("account = %+v, want the columns nobody renamed", got)
	}
	if len(got.Roles) != 2 {
		t.Errorf("roles = %v, want the ones written to the column that was named", got.Roles)
	}
}
