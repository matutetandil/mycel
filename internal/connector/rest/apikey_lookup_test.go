package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Checking an API key against a table.
//
// A key in a list is revoked by a deployment; a key in a table is revoked by
// an update. That is what the `validate` block is for, and it was parsed into
// two fields nothing read: keys were checked against the static list and
// nothing else, so a connector configured this way refused every key it was
// given.

// keyTable answers the lookup, and records what it was asked.
type keyTable struct {
	rows []map[string]interface{}
	err  error

	statement string
	filters   map[string]interface{}
}

func (k *keyTable) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	k.statement = query.RawSQL
	k.filters = query.Filters
	if k.err != nil {
		return nil, k.err
	}
	return &connector.Result{Rows: k.rows}, nil
}

func TestAKeyThatIsInTheTable(t *testing.T) {
	table := &keyTable{rows: []map[string]interface{}{
		{"user_id": "user-1", "tenant": "acme"},
	}}

	validate := CreateAPIKeyValidator(table, "SELECT user_id, tenant FROM api_keys WHERE key = :key AND revoked_at IS NULL")

	valid, userID, metadata, err := validate(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !valid {
		t.Fatal("a key that is in the table was refused")
	}
	// Who the key belongs to, and whatever else the row carried: this is what
	// a flow reads to answer for the right customer.
	if userID != "user-1" {
		t.Errorf("user = %q", userID)
	}
	if metadata["tenant"] != "acme" {
		t.Errorf("metadata = %v", metadata)
	}
}

func TestTheKeyIsNeverWrittenIntoTheStatement(t *testing.T) {
	// The key is whatever the caller sent in a header. Substituted into the
	// SQL — which is what this used to do — a key of `' OR '1'='1` turns the
	// lookup into one that matches every row, and the request is
	// authenticated as whoever comes back first.
	table := &keyTable{rows: []map[string]interface{}{{"user_id": "user-1"}}}
	statement := "SELECT user_id FROM api_keys WHERE key = :key AND revoked_at IS NULL"

	validate := CreateAPIKeyValidator(table, statement)
	if _, _, _, err := validate(context.Background(), `' OR '1'='1`); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if table.statement != statement {
		t.Errorf("the statement was rewritten: %s", table.statement)
	}
	if strings.Contains(table.statement, "OR") {
		t.Errorf("the caller's key ended up inside the statement: %s", table.statement)
	}
	if table.filters["key"] != `' OR '1'='1` {
		t.Errorf("the key was not passed as a parameter: %v", table.filters)
	}
}

func TestAKeyThatIsNotInTheTable(t *testing.T) {
	// Revoked, or never issued: both are no rows, and both are a refusal.
	table := &keyTable{rows: nil}

	valid, _, _, err := CreateAPIKeyValidator(table, "SELECT 1 FROM api_keys WHERE key = :key")(
		context.Background(), "key-9")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if valid {
		t.Error("a key that is not in the table was accepted")
	}
}

func TestWhenTheTableCannotBeReached(t *testing.T) {
	// The database is down. This must be an error rather than a refusal or an
	// acceptance: the caller gets a 401 either way, but the flow's own
	// logging is the only place the real cause appears.
	table := &keyTable{err: context.DeadlineExceeded}

	valid, _, _, err := CreateAPIKeyValidator(table, "SELECT 1 FROM api_keys WHERE key = :key")(
		context.Background(), "key-1")
	if err == nil {
		t.Fatal("a database that could not be reached was reported as a decision about the key")
	}
	if valid {
		t.Error("the key was accepted while the table could not be read")
	}
}

func TestAServerCheckingKeysAgainstATable(t *testing.T) {
	// End to end through the middleware, which is what the runtime wires.
	c := New("api", 0, nil, nil)
	c.SetAuthConfig(&AuthConfig{
		Type: "api_key",
		APIKey: &APIKeyAuthConfig{
			Header:            "X-API-Key",
			ValidateConnector: "connector.orders_db",
			ValidateQuery:     "SELECT user_id FROM api_keys WHERE key = :key",
		},
	})

	// What the runtime reads to know where to look.
	name, query := c.APIKeyValidateConnector()
	if name != "connector.orders_db" || query == "" {
		t.Fatalf("the connector does not say where to check keys: %q / %q", name, query)
	}

	table := &keyTable{rows: []map[string]interface{}{{"user_id": "user-1"}}}
	c.SetAPIKeyValidator(table)

	reached := false
	handler := c.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))

	call := func(key string) int {
		reached = false
		request := httptest.NewRequest(http.MethodGet, "/orders", nil)
		request.Header.Set("X-API-Key", key)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Code
	}

	if code := call("key-1"); !reached {
		t.Errorf("a key that is in the table was refused with %d", code)
	}

	// And one the table does not have.
	table.rows = nil
	if code := call("key-9"); reached || code != http.StatusUnauthorized {
		t.Errorf("a key that is not in the table was answered %d", code)
	}
}

func TestAServerWithNothingToCheckAgainst(t *testing.T) {
	// A connector with no validate block: handing it a reader must change
	// nothing, since the runtime calls this for every REST server it finds.
	c := New("api", 0, nil, nil)
	c.SetAuthConfig(&AuthConfig{
		Type:   "api_key",
		APIKey: &APIKeyAuthConfig{Header: "X-API-Key", Keys: []string{"key-1"}},
	})

	name, query := c.APIKeyValidateConnector()
	if name != "" || query != "" {
		t.Errorf("a connector with no validate block named %q / %q", name, query)
	}

	c.SetAPIKeyValidator(&keyTable{})

	reached := false
	handler := c.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	request := httptest.NewRequest(http.MethodGet, "/orders", nil)
	request.Header.Set("X-API-Key", "key-1")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if !reached {
		t.Error("the static key list stopped working")
	}
}
