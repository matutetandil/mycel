package rest

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTAuth(t *testing.T) {
	secret := "test-secret-key-32-bytes-long!!"

	// Create a test connector with JWT auth
	conn := New("test", 3000, nil, nil)
	conn.SetAuthConfig(&AuthConfig{
		Type: "jwt",
		JWT: &JWTAuthConfig{
			Secret:     secret,
			Issuer:     "test-issuer",
			Audience:   []string{"test-audience"},
			Algorithms: []string{"HS256"},
		},
		Public: []string{"/health", "/public/*"},
	})

	// Test handler that checks for auth context on protected routes
	// For protected routes, this will have an auth context set by middleware
	// For public routes, we just return OK
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handler always returns OK - the middleware handles auth rejection
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	// Wrap with auth middleware
	authHandler := conn.authMiddleware(handler)

	t.Run("valid JWT", func(t *testing.T) {
		// Create valid token
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "user123",
			"iss": "test-issuer",
			"aud": "test-audience",
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString([]byte(secret))

		req := httptest.NewRequest("GET", "/api/users", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}
	})

	t.Run("missing authorization header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/users", nil)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rr.Code)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/users", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rr.Code)
		}
	})

	t.Run("expired token", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "user123",
			"iss": "test-issuer",
			"aud": "test-audience",
			"exp": time.Now().Add(-time.Hour).Unix(), // Expired
		})
		tokenString, _ := token.SignedString([]byte(secret))

		req := httptest.NewRequest("GET", "/api/users", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rr.Code)
		}
	})

	t.Run("wrong issuer", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "user123",
			"iss": "wrong-issuer",
			"aud": "test-audience",
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString([]byte(secret))

		req := httptest.NewRequest("GET", "/api/users", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rr.Code)
		}
	})

	t.Run("public path allowed without auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200 for public path, got %d", rr.Code)
		}
	})

	t.Run("public wildcard path allowed", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/public/docs", nil)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200 for public wildcard path, got %d", rr.Code)
		}
	})
}

func TestAPIKeyAuth(t *testing.T) {
	conn := New("test", 3000, nil, nil)
	conn.SetAuthConfig(&AuthConfig{
		Type: "api_key",
		APIKey: &APIKeyAuthConfig{
			Keys:   []string{"valid-key-1", "valid-key-2"},
			Header: "X-API-Key",
		},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	authHandler := conn.authMiddleware(handler)

	t.Run("valid API key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/data", nil)
		req.Header.Set("X-API-Key", "valid-key-1")
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}
	})

	t.Run("invalid API key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/data", nil)
		req.Header.Set("X-API-Key", "invalid-key")
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rr.Code)
		}
	})

	t.Run("missing API key", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/data", nil)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rr.Code)
		}
	})
}

func TestAPIKeyQueryParam(t *testing.T) {
	conn := New("test", 3000, nil, nil)
	conn.SetAuthConfig(&AuthConfig{
		Type: "api_key",
		APIKey: &APIKeyAuthConfig{
			Keys:       []string{"valid-key"},
			QueryParam: "api_key",
		},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	authHandler := conn.authMiddleware(handler)

	t.Run("valid API key in query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/data?api_key=valid-key", nil)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}
	})
}

func TestBasicAuth(t *testing.T) {
	conn := New("test", 3000, nil, nil)
	conn.SetAuthConfig(&AuthConfig{
		Type: "basic",
		Basic: &BasicAuthConfig{
			Users: map[string]string{
				"admin": "secret123",
				"user":  "password",
			},
			Realm: "Test Realm",
		},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	authHandler := conn.authMiddleware(handler)

	t.Run("valid credentials", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/data", nil)
		auth := base64.StdEncoding.EncodeToString([]byte("admin:secret123"))
		req.Header.Set("Authorization", "Basic "+auth)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}
	})

	t.Run("invalid password", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/data", nil)
		auth := base64.StdEncoding.EncodeToString([]byte("admin:wrongpassword"))
		req.Header.Set("Authorization", "Basic "+auth)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rr.Code)
		}

		// Check WWW-Authenticate header
		wwwAuth := rr.Header().Get("WWW-Authenticate")
		if wwwAuth == "" {
			t.Error("expected WWW-Authenticate header")
		}
	})

	t.Run("invalid username", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/data", nil)
		auth := base64.StdEncoding.EncodeToString([]byte("unknown:secret123"))
		req.Header.Set("Authorization", "Basic "+auth)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rr.Code)
		}
	})

	t.Run("missing auth header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/data", nil)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected status 401, got %d", rr.Code)
		}
	})
}

func TestRequiredHeaders(t *testing.T) {
	conn := New("test", 3000, nil, nil)
	conn.SetAuthConfig(&AuthConfig{
		RequiredHeaders: []string{"X-Request-ID", "X-Correlation-ID"},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	authHandler := conn.authMiddleware(handler)

	t.Run("all headers present", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/data", nil)
		req.Header.Set("X-Request-ID", "123")
		req.Header.Set("X-Correlation-ID", "456")
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}
	})

	t.Run("missing required header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/data", nil)
		req.Header.Set("X-Request-ID", "123")
		// Missing X-Correlation-ID
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", rr.Code)
		}
	})
}

func TestResponseHeaders(t *testing.T) {
	conn := New("test", 3000, nil, nil)
	conn.SetAuthConfig(&AuthConfig{
		ResponseHeaders: map[string]string{
			"X-Powered-By": "Mycel",
			"X-Version":    "1.0.0",
		},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	authHandler := conn.authMiddleware(handler)

	t.Run("response headers added", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/data", nil)
		rr := httptest.NewRecorder()

		authHandler.ServeHTTP(rr, req)

		if rr.Header().Get("X-Powered-By") != "Mycel" {
			t.Errorf("expected X-Powered-By header")
		}
		if rr.Header().Get("X-Version") != "1.0.0" {
			t.Errorf("expected X-Version header")
		}
	})
}

func TestGetAuthContext(t *testing.T) {
	t.Run("no auth context", func(t *testing.T) {
		ctx := context.Background()
		authCtx := GetAuthContext(ctx)
		if authCtx != nil {
			t.Error("expected nil auth context")
		}
	})

	t.Run("with auth context", func(t *testing.T) {
		expected := &AuthContext{
			Authenticated: true,
			UserID:        "user123",
		}
		ctx := context.WithValue(context.Background(), authContextKey, expected)
		authCtx := GetAuthContext(ctx)
		if authCtx == nil {
			t.Error("expected auth context")
		}
		if authCtx.UserID != "user123" {
			t.Errorf("expected user123, got %s", authCtx.UserID)
		}
	})
}
