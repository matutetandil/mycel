package grpc

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Authentication on a gRPC server is an interceptor, and an interceptor tested
// on its own proves it makes the right decision — not that anything asks it.
// The wiring between "the configuration says jwt" and "this call was refused"
// is what these cover, over a real connection.

const authSecret = "test-secret-key-32-bytes-long!!"

func servingWithAuth(t *testing.T, dir string, auth *AuthConfig) string {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	port := freePort(t)

	server := NewServerConnector("api", &ServerConfig{
		Host: "127.0.0.1", Port: port,
		ProtoPath: dir, ProtoFiles: []string{"user.proto"},
		Auth: auth,
	}, logger)

	if err := server.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	server.RegisterRoute("UserService/GetUser",
		func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"id": 1, "name": "Ada"}, nil
		})
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = server.Close(context.Background()) })

	address := net.JoinHostPort("127.0.0.1", itoa(port))
	waitForListener(t, address)
	return address
}

func clientWithAuth(t *testing.T, dir, address string, auth *ClientAuthConfig) *ClientConnector {
	t.Helper()
	client := NewClientConnector("api", &ClientConfig{
		Target: address, Insecure: true,
		ProtoPath: dir, ProtoFiles: []string{"user.proto"},
		Timeout: 5 * time.Second, ConnectTimeout: 5 * time.Second,
		Auth: auth,
	})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	return client
}

func token(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(authSecret))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

func TestACallWithNoCredentialsIsRefusedByAServerThatWantsThem(t *testing.T) {
	dir := protoDir(t)
	address := servingWithAuth(t, dir, &AuthConfig{
		Type: "jwt",
		JWT:  &JWTAuthConfig{Secret: authSecret, Algorithms: []string{"HS256"}},
	})

	client := dialing(t, dir, address)
	_, err := client.Call(context.Background(), "UserService/GetUser", map[string]interface{}{"id": 1})
	if err == nil {
		t.Fatal("a call with no credentials was answered")
	}
	// And it says so, rather than failing as something else.
	if !strings.Contains(strings.ToLower(err.Error()), "auth") &&
		!strings.Contains(err.Error(), "Unauthenticated") {
		t.Errorf("error = %q, want it to say the call was not authenticated", err)
	}
}

func TestACallCarryingATokenIsAnswered(t *testing.T) {
	dir := protoDir(t)
	address := servingWithAuth(t, dir, &AuthConfig{
		Type: "jwt",
		JWT:  &JWTAuthConfig{Secret: authSecret, Algorithms: []string{"HS256"}},
	})

	client := clientWithAuth(t, dir, address, &ClientAuthConfig{
		Type:  "bearer",
		Token: token(t, jwt.MapClaims{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()}),
	})

	answer, err := client.Call(context.Background(), "UserService/GetUser", map[string]interface{}{"id": 1})
	if err != nil {
		t.Fatalf("a call carrying a valid token was refused: %v", err)
	}
	if fields, _ := answer.(map[string]interface{}); fields["name"] != "Ada" {
		t.Errorf("answer = %v", answer)
	}
}

func TestATokenSignedWithSomethingElseIsRefused(t *testing.T) {
	// The check is only worth having if it turns something away.
	dir := protoDir(t)
	address := servingWithAuth(t, dir, &AuthConfig{
		Type: "jwt",
		JWT:  &JWTAuthConfig{Secret: authSecret, Algorithms: []string{"HS256"}},
	})

	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte("a secret nobody configured!!!!!!"))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	client := clientWithAuth(t, dir, address, &ClientAuthConfig{Type: "bearer", Token: forged})
	if _, err := client.Call(context.Background(), "UserService/GetUser", map[string]interface{}{"id": 1}); err == nil {
		t.Error("a token signed with a key nobody configured was accepted")
	}
}

func TestAnExpiredTokenIsRefused(t *testing.T) {
	dir := protoDir(t)
	address := servingWithAuth(t, dir, &AuthConfig{
		Type: "jwt",
		JWT:  &JWTAuthConfig{Secret: authSecret, Algorithms: []string{"HS256"}},
	})

	client := clientWithAuth(t, dir, address, &ClientAuthConfig{
		Type:  "bearer",
		Token: token(t, jwt.MapClaims{"sub": "user-1", "exp": time.Now().Add(-time.Hour).Unix()}),
	})
	if _, err := client.Call(context.Background(), "UserService/GetUser", map[string]interface{}{"id": 1}); err == nil {
		t.Error("a token that expired an hour ago was accepted")
	}
}

func TestAMethodListedAsPublicIsAnsweredWithoutCredentials(t *testing.T) {
	// Health checks and the like, which have to answer before anything has a
	// token to send.
	dir := protoDir(t)
	address := servingWithAuth(t, dir, &AuthConfig{
		Type:   "jwt",
		JWT:    &JWTAuthConfig{Secret: authSecret, Algorithms: []string{"HS256"}},
		Public: []string{"/testing.UserService/GetUser"},
	})

	client := dialing(t, dir, address)
	if _, err := client.Call(context.Background(), "UserService/GetUser", map[string]interface{}{"id": 1}); err != nil {
		t.Errorf("a method listed as public was refused: %v", err)
	}
}

func TestAnApiKeyIsCheckedOverTheWireToo(t *testing.T) {
	dir := protoDir(t)
	address := servingWithAuth(t, dir, &AuthConfig{
		Type:   "api_key",
		APIKey: &APIKeyConfig{Keys: []string{"the-right-key"}, Metadata: "api-key"},
	})

	right := clientWithAuth(t, dir, address, &ClientAuthConfig{
		Type: "api_key", APIKey: &ClientAPIKeyConfig{Key: "the-right-key", Metadata: "api-key"},
	})
	if _, err := right.Call(context.Background(), "UserService/GetUser", map[string]interface{}{"id": 1}); err != nil {
		t.Errorf("a call with the configured key was refused: %v", err)
	}

	wrong := clientWithAuth(t, dir, address, &ClientAuthConfig{
		Type: "api_key", APIKey: &ClientAPIKeyConfig{Key: "a-key-nobody-issued", Metadata: "api-key"},
	})
	if _, err := wrong.Call(context.Background(), "UserService/GetUser", map[string]interface{}{"id": 1}); err == nil {
		t.Error("a key nobody issued was accepted")
	}
}
