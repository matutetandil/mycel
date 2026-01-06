package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestAuthInterceptor_PublicMethod(t *testing.T) {
	interceptor := NewAuthInterceptor(&AuthConfig{
		Type:   "jwt",
		Public: []string{"/health.HealthCheck/Check", "/package.Service/*"},
		JWT: &JWTAuthConfig{
			Secret: "test-secret",
		},
	})

	tests := []struct {
		method string
		public bool
	}{
		{"/health.HealthCheck/Check", true},
		{"/package.Service/GetUser", true},
		{"/package.Service/CreateUser", true},
		{"/other.Service/Method", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			result := interceptor.isPublicMethod(tt.method)
			if result != tt.public {
				t.Errorf("isPublicMethod(%s) = %v, want %v", tt.method, result, tt.public)
			}
		})
	}
}

func TestAuthInterceptor_JWT(t *testing.T) {
	secret := "test-secret-key-32-bytes-long!!"

	interceptor := NewAuthInterceptor(&AuthConfig{
		Type: "jwt",
		JWT: &JWTAuthConfig{
			Secret:     secret,
			Issuer:     "test-issuer",
			Audience:   []string{"test-audience"},
			Algorithms: []string{"HS256"},
		},
	})

	t.Run("valid token", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "user123",
			"iss": "test-issuer",
			"aud": "test-audience",
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString([]byte(secret))

		md := metadata.Pairs("authorization", "Bearer "+tokenString)
		ctx := metadata.NewIncomingContext(context.Background(), md)

		authCtx, err := interceptor.authenticateJWT(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !authCtx.Authenticated {
			t.Error("expected authenticated to be true")
		}
		if authCtx.UserID != "user123" {
			t.Errorf("expected userID 'user123', got '%s'", authCtx.UserID)
		}
		if authCtx.Method != "jwt" {
			t.Errorf("expected method 'jwt', got '%s'", authCtx.Method)
		}
	})

	t.Run("missing authorization", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})

		_, err := interceptor.authenticateJWT(ctx)
		if err == nil {
			t.Error("expected error for missing authorization")
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		md := metadata.Pairs("authorization", "Bearer invalid-token")
		ctx := metadata.NewIncomingContext(context.Background(), md)

		_, err := interceptor.authenticateJWT(ctx)
		if err == nil {
			t.Error("expected error for invalid token")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "user123",
			"iss": "test-issuer",
			"aud": "test-audience",
			"exp": time.Now().Add(-time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString([]byte(secret))

		md := metadata.Pairs("authorization", "Bearer "+tokenString)
		ctx := metadata.NewIncomingContext(context.Background(), md)

		_, err := interceptor.authenticateJWT(ctx)
		if err == nil {
			t.Error("expected error for expired token")
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

		md := metadata.Pairs("authorization", "Bearer "+tokenString)
		ctx := metadata.NewIncomingContext(context.Background(), md)

		_, err := interceptor.authenticateJWT(ctx)
		if err == nil {
			t.Error("expected error for wrong issuer")
		}
	})
}

func TestAuthInterceptor_APIKey(t *testing.T) {
	interceptor := NewAuthInterceptor(&AuthConfig{
		Type: "api_key",
		APIKey: &APIKeyConfig{
			Keys:     []string{"valid-key-1", "valid-key-2"},
			Metadata: "api-key",
		},
	})

	t.Run("valid API key", func(t *testing.T) {
		md := metadata.Pairs("api-key", "valid-key-1")
		ctx := metadata.NewIncomingContext(context.Background(), md)

		authCtx, err := interceptor.authenticateAPIKey(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !authCtx.Authenticated {
			t.Error("expected authenticated to be true")
		}
		if authCtx.Method != "api_key" {
			t.Errorf("expected method 'api_key', got '%s'", authCtx.Method)
		}
	})

	t.Run("invalid API key", func(t *testing.T) {
		md := metadata.Pairs("api-key", "invalid-key")
		ctx := metadata.NewIncomingContext(context.Background(), md)

		_, err := interceptor.authenticateAPIKey(ctx)
		if err == nil {
			t.Error("expected error for invalid API key")
		}
	})

	t.Run("missing API key", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})

		_, err := interceptor.authenticateAPIKey(ctx)
		if err == nil {
			t.Error("expected error for missing API key")
		}
	})
}

func TestAuthInterceptor_UnaryInterceptor(t *testing.T) {
	secret := "test-secret-key-32-bytes-long!!"

	interceptor := NewAuthInterceptor(&AuthConfig{
		Type:   "jwt",
		Public: []string{"/health.HealthCheck/Check"},
		JWT: &JWTAuthConfig{
			Secret:     secret,
			Algorithms: []string{"HS256"},
		},
	})

	unaryInterceptor := interceptor.UnaryInterceptor()

	t.Run("public method allowed", func(t *testing.T) {
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return "success", nil
		}

		info := &grpc.UnaryServerInfo{FullMethod: "/health.HealthCheck/Check"}
		result, err := unaryInterceptor(context.Background(), nil, info, handler)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "success" {
			t.Errorf("expected 'success', got '%v'", result)
		}
	})

	t.Run("protected method requires auth", func(t *testing.T) {
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return "success", nil
		}

		info := &grpc.UnaryServerInfo{FullMethod: "/package.Service/Method"}
		ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})
		_, err := unaryInterceptor(ctx, nil, info, handler)
		if err == nil {
			t.Error("expected error for unauthenticated request")
		}
	})

	t.Run("protected method with valid token", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": "user123",
			"exp": time.Now().Add(time.Hour).Unix(),
		})
		tokenString, _ := token.SignedString([]byte(secret))

		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			authCtx := GetAuthContext(ctx)
			if authCtx == nil {
				t.Error("expected auth context in handler")
			}
			return "success", nil
		}

		info := &grpc.UnaryServerInfo{FullMethod: "/package.Service/Method"}
		md := metadata.Pairs("authorization", "Bearer "+tokenString)
		ctx := metadata.NewIncomingContext(context.Background(), md)

		result, err := unaryInterceptor(ctx, nil, info, handler)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "success" {
			t.Errorf("expected 'success', got '%v'", result)
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
			Method:        "jwt",
		}
		ctx := withAuthContext(context.Background(), expected)

		authCtx := GetAuthContext(ctx)
		if authCtx == nil {
			t.Fatal("expected auth context")
		}
		if authCtx.UserID != "user123" {
			t.Errorf("expected userID 'user123', got '%s'", authCtx.UserID)
		}
	})
}

func TestAuthInterceptor_NoAuth(t *testing.T) {
	interceptor := NewAuthInterceptor(&AuthConfig{
		Type: "none",
	})

	ctx := context.Background()
	authCtx, err := interceptor.authenticate(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !authCtx.Authenticated {
		t.Error("expected authenticated to be true for 'none' type")
	}
}
