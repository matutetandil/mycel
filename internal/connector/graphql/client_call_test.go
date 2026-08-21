package graphql

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Calling a GraphQL API somebody else runs. A GraphQL server answers 200 with
// the errors inside the body, so "did it work" is a question about the payload
// rather than the status — and a client that reads only the status reports
// success for every failure.

// graphqlServer answers with the given body and records what it was sent.
func graphqlServer(t *testing.T, handler http.HandlerFunc) (*ClientConnector, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	client := NewClient("orders_api", &ClientConfig{
		Endpoint:   server.URL,
		RetryCount: 1,
		RetryDelay: time.Millisecond,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return client, &calls
}

func answers(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func TestAQueryAnswersWithWhatTheServerSent(t *testing.T) {
	client, _ := graphqlServer(t, answers(`{"data":{"orders":[{"id":"1"},{"id":"2"}]}}`))

	result, err := client.Read(context.Background(), connector.Query{
		Target:  `query { orders { id } }`,
		Filters: map[string]interface{}{"limit": 2},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Errorf("%d rows, want both", len(result.Rows))
	}
}

func TestAMutationSendsItsVariables(t *testing.T) {
	var sent map[string]interface{}
	client, _ := graphqlServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"createOrder":{"id":"1"}}}`))
	})

	if _, err := client.Write(context.Background(), &connector.Data{
		Target:  `mutation ($sku: String!) { createOrder(sku: $sku) { id } }`,
		Payload: map[string]interface{}{"sku": "WIDGET-1"},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if !strings.Contains(sent["query"].(string), "createOrder") {
		t.Errorf("query = %v", sent["query"])
	}
	variables, ok := sent["variables"].(map[string]interface{})
	if !ok || variables["sku"] != "WIDGET-1" {
		t.Errorf("variables = %v, want the payload", sent["variables"])
	}
}

func TestAnErrorInTheBodyIsAFailure(t *testing.T) {
	// The whole point: a GraphQL server answers 200 and puts the failure in
	// the body, so reading the status alone reports success for everything.
	client, _ := graphqlServer(t, answers(`{"errors":[{"message":"field orders not found"}]}`))

	_, err := client.Read(context.Background(), connector.Query{Target: `query { orders { id } }`})
	if err == nil {
		t.Fatal("a query the server refused was read as a success")
	}
	if !strings.Contains(err.Error(), "orders not found") {
		t.Errorf("error = %q, want what the server said", err)
	}
}

func TestACallReachesTheServerForEnrichment(t *testing.T) {
	// enrich and step both go through Call, which answers with the payload
	// rather than rows.
	client, _ := graphqlServer(t, answers(`{"data":{"customer":{"name":"Ada"}}}`))

	result, err := client.Call(context.Background(), `query { customer { name } }`,
		map[string]interface{}{"id": "c-1"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result == nil {
		t.Error("the call answered with nothing")
	}
}

func TestHealthAsksTheServerSomethingItCanAnswer(t *testing.T) {
	client, calls := graphqlServer(t, answers(`{"data":{"__typename":"Query"}}`))

	if err := client.Health(context.Background()); err != nil {
		t.Errorf("a server that is answering was reported unhealthy: %v", err)
	}
	if atomic.LoadInt32(calls) == 0 {
		t.Error("health answered without asking the server anything")
	}
}

func TestAServerThatIsNotThereIsNotHealthy(t *testing.T) {
	client := NewClient("orders_api", &ClientConfig{
		Endpoint:   "http://127.0.0.1:1/graphql",
		RetryCount: 1,
		RetryDelay: time.Millisecond,
		Timeout:    time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := client.Health(context.Background()); err == nil {
		t.Error("a server nobody is running was reported healthy")
	}
}

func TestAFailedRequestIsRetried(t *testing.T) {
	// A GraphQL API behind a load balancer drops a connection now and then,
	// and a retry is the difference between a failed flow and a slow one.
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"orders":[]}}`))
	}))
	defer server.Close()

	client := NewClient("orders_api", &ClientConfig{
		Endpoint:   server.URL,
		RetryCount: 3,
		RetryDelay: time.Millisecond,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := client.Read(context.Background(), connector.Query{Target: `query { orders { id } }`}); err != nil {
		t.Fatalf("a request that succeeded on the second attempt failed: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("%d attempts, want the retry", attempts)
	}
}

// --- Credentials -------------------------------------------------------------

func TestATokenIsFetchedOnceAndSentWithEveryRequest(t *testing.T) {
	var exchanges int32
	var secretSeen string
	var authorization string

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&exchanges, 1)
		_ = r.ParseForm()
		secretSeen = r.Form.Get("client_secret")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "issued-token", "expires_in": 3600,
		})
	}))
	defer tokenServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"orders":[]}}`))
	}))
	defer apiServer.Close()

	client := NewClient("orders_api", &ClientConfig{
		Endpoint:   apiServer.URL,
		RetryCount: 1,
		RetryDelay: time.Millisecond,
		Auth: &AuthConfig{
			Type:     "oauth2",
			TokenURL: tokenServer.URL,
			ClientID: "mycel",
			// A secret with the characters that used to end the field early.
			ClientSecret: "s3cret&with+trouble",
			Scopes:       []string{"orders:read", "orders:write"},
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for i := 0; i < 2; i++ {
		if _, err := client.Read(context.Background(), connector.Query{Target: `query { orders { id } }`}); err != nil {
			t.Fatalf("Read: %v", err)
		}
	}

	// The secret arrived whole. Concatenated into the form, everything from
	// the & onwards became another parameter and the server saw a truncated
	// secret — answering with something that named neither.
	if secretSeen != "s3cret&with+trouble" {
		t.Errorf("the server received %q, want the secret as configured", secretSeen)
	}
	if authorization != "Bearer issued-token" {
		t.Errorf("authorization = %q", authorization)
	}
	if got := atomic.LoadInt32(&exchanges); got != 1 {
		t.Errorf("%d token exchanges, want one: the token is not being reused", got)
	}
}

func TestAnAuthorisationServerThatRefusesStopsTheRequest(t *testing.T) {
	// Rather than a query going out unauthenticated and failing at the API.
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer refusing.Close()

	client := NewClient("orders_api", &ClientConfig{
		Endpoint:   "http://127.0.0.1:1/graphql",
		RetryCount: 1,
		RetryDelay: time.Millisecond,
		Auth: &AuthConfig{
			Type: "oauth2", TokenURL: refusing.URL, ClientID: "mycel", ClientSecret: "wrong",
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := client.Read(context.Background(), connector.Query{Target: `query { orders { id } }`}); err == nil {
		t.Error("the query went out although the credentials were refused")
	}
}
