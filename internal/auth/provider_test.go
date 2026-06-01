package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// activeProvider returns a ProviderConfig whose `validate` points at url and
// maps a typical "introspection" JSON response.
func activeProvider(name, url string) *ProviderConfig {
	return &ProviderConfig{
		Name:     name,
		Type:     "http",
		Validate: url,
		Request:  map[string]string{"Authorization": "Bearer {token}"},
		Response: &ProviderResponseConfig{
			Success: "status == 200 && body.active == true",
			UserID:  "body.sub",
			Email:   "body.email",
			Roles:   "body.roles",
		},
	}
}

func TestProviderValidator_Success(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"active":true,"sub":"u1","email":"ada@example.com","roles":["admin","user"]}`))
	}))
	defer srv.Close()

	v, err := NewProviderValidator([]*ProviderConfig{activeProvider("keys", srv.URL)}, srv.Client(), nil)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}

	user, claims, err := v.Validate(context.Background(), "tok123")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("token not templated into header: got %q", gotAuth)
	}
	if user.ID != "u1" || claims.UserID != "u1" {
		t.Errorf("expected user id u1, got user=%q claims=%q", user.ID, claims.UserID)
	}
	if user.Email != "ada@example.com" {
		t.Errorf("expected email mapped, got %q", user.Email)
	}
	if len(claims.Roles) != 2 || claims.Roles[0] != "admin" || claims.Roles[1] != "user" {
		t.Errorf("expected roles [admin user], got %v", claims.Roles)
	}
	// Full body should be available as custom claims.
	if claims.Custom["sub"] != "u1" {
		t.Errorf("expected response body exposed via claims.Custom, got %v", claims.Custom)
	}
}

func TestProviderValidator_SuccessFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"active":false}`))
	}))
	defer srv.Close()

	v, _ := NewProviderValidator([]*ProviderConfig{activeProvider("keys", srv.URL)}, srv.Client(), nil)
	if _, _, err := v.Validate(context.Background(), "tok"); err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken when success is false, got %v", err)
	}
}

func TestProviderValidator_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	v, _ := NewProviderValidator([]*ProviderConfig{activeProvider("keys", srv.URL)}, srv.Client(), nil)
	if _, _, err := v.Validate(context.Background(), "tok"); err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken on 5xx, got %v", err)
	}
}

func TestProviderValidator_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"active":true,"sub":"u1"}`))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 20 * time.Millisecond}
	v, _ := NewProviderValidator([]*ProviderConfig{activeProvider("keys", srv.URL)}, client, nil)
	if _, _, err := v.Validate(context.Background(), "tok"); err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken on timeout, got %v", err)
	}
}

func TestProviderValidator_RolesCSV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"active":true,"sub":"u1","roles":"admin, user , ops"}`))
	}))
	defer srv.Close()

	v, _ := NewProviderValidator([]*ProviderConfig{activeProvider("keys", srv.URL)}, srv.Client(), nil)
	_, claims, err := v.Validate(context.Background(), "tok")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	want := []string{"admin", "user", "ops"}
	if len(claims.Roles) != len(want) {
		t.Fatalf("expected %v, got %v", want, claims.Roles)
	}
	for i := range want {
		if claims.Roles[i] != want[i] {
			t.Errorf("role %d: want %q, got %q", i, want[i], claims.Roles[i])
		}
	}
}

func TestProviderValidator_ConstructFailFast(t *testing.T) {
	cases := []struct {
		name string
		cfg  *ProviderConfig
	}{
		{"unsupported type", &ProviderConfig{Name: "x", Type: "grpc", Validate: "http://x", Response: &ProviderResponseConfig{Success: "true"}}},
		{"missing validate", &ProviderConfig{Name: "x", Type: "http", Response: &ProviderResponseConfig{Success: "true"}}},
		{"missing success", &ProviderConfig{Name: "x", Type: "http", Validate: "http://x"}},
		{"bad CEL", &ProviderConfig{Name: "x", Type: "http", Validate: "http://x", Response: &ProviderResponseConfig{Success: "this is not && valid"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewProviderValidator([]*ProviderConfig{tc.cfg}, nil, nil); err == nil {
				t.Errorf("expected construction to fail for %s", tc.name)
			}
		})
	}
}

// TestManager_ValidateToken_ProviderFallthrough verifies the end-to-end path:
// a non-JWT credential falls through local validation to the HTTP provider.
func TestManager_ValidateToken_ProviderFallthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer apikey-xyz" {
			http.Error(w, "no key", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"active":true,"sub":"svc-1","email":"svc@acme.io","roles":["service"]}`))
	}))
	defer srv.Close()

	mgr, err := NewManager(&Config{
		Secret:    "test-secret-please-change",
		Providers: []*ProviderConfig{activeProvider("keys", srv.URL)},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	user, claims, err := mgr.ValidateToken(context.Background(), "apikey-xyz")
	if err != nil {
		t.Fatalf("expected provider fallthrough to validate, got %v", err)
	}
	if user.ID != "svc-1" || len(claims.Roles) != 1 || claims.Roles[0] != "service" {
		t.Errorf("unexpected identity: user=%q roles=%v", user.ID, claims.Roles)
	}

	// A bad credential the provider rejects must still fail.
	if _, _, err := mgr.ValidateToken(context.Background(), "wrong-key"); err != ErrInvalidToken {
		t.Errorf("expected ErrInvalidToken for rejected key, got %v", err)
	}
}
