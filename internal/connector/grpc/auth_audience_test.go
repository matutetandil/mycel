package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/metadata"
)

// A list of audiences means any of them is acceptable — that is what it means
// in the REST connector, and what a service fronting two audiences during a
// migration needs. The gRPC copy handed only the first to the parser, so tokens
// carrying the second were refused and the second entry did nothing.

func signedFor(t *testing.T, secret, audience string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user-1",
		"aud": audience,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

func TestAnyOfTheConfiguredAudiencesIsAccepted(t *testing.T) {
	const secret = "test-secret-key-32-bytes-long!!"
	interceptor := NewAuthInterceptor(&AuthConfig{
		Type: "jwt",
		JWT: &JWTAuthConfig{
			Secret:     secret,
			Audience:   []string{"api.internal", "api.public"},
			Algorithms: []string{"HS256"},
		},
	})

	for _, audience := range []string{"api.internal", "api.public"} {
		t.Run(audience, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(context.Background(),
				metadata.Pairs("authorization", "Bearer "+signedFor(t, secret, audience)))

			if _, err := interceptor.authenticateJWT(ctx); err != nil {
				t.Errorf("a token for a configured audience was refused: %v", err)
			}
		})
	}
}

func TestAnAudienceNobodyConfiguredIsStillRefused(t *testing.T) {
	// The check is only worth having if it turns something away: a token minted
	// for another service must not open this one.
	const secret = "test-secret-key-32-bytes-long!!"
	interceptor := NewAuthInterceptor(&AuthConfig{
		Type: "jwt",
		JWT: &JWTAuthConfig{
			Secret:     secret,
			Audience:   []string{"api.internal", "api.public"},
			Algorithms: []string{"HS256"},
		},
	})

	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer "+signedFor(t, secret, "someone.else")))

	if _, err := interceptor.authenticateJWT(ctx); err == nil {
		t.Error("a token minted for another service was accepted")
	}
}
