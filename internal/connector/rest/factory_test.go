package rest

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

func built(t *testing.T, props map[string]interface{}) *Connector {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	conn, err := NewFactory(logger).Create(context.Background(), &connector.Config{
		Name: "api", Type: "rest", Properties: props,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return conn.(*Connector)
}

func TestTheServerListensWhereItWasTold(t *testing.T) {
	if got := built(t, map[string]interface{}{"port": 18777}).port; got != 18777 {
		t.Errorf("port = %d", got)
	}
	// Written as a word, which is how it arrives from env(). This was ignored
	// once, and the banner announced the port the service was not listening on.
	if got := built(t, map[string]interface{}{"port": "18777"}).port; got != 18777 {
		t.Errorf("port from a string = %d, want 18777", got)
	}
	if got := built(t, map[string]interface{}{}).port; got != 3000 {
		t.Errorf("default port = %d, want 3000", got)
	}
}

func TestCORSIsWhatTheBlockSaid(t *testing.T) {
	conn := built(t, map[string]interface{}{
		"cors": map[string]interface{}{
			"origins": []interface{}{"https://app.example.com", "https://admin.example.com"},
			"methods": []interface{}{"GET", "POST"},
			"headers": []interface{}{"Authorization"},
		},
	})
	if conn.cors == nil {
		t.Fatal("the CORS block was written and nothing came of it")
	}
	if len(conn.cors.Origins) != 2 || conn.cors.Origins[0] != "https://app.example.com" {
		t.Errorf("origins = %v", conn.cors.Origins)
	}
	if len(conn.cors.Methods) != 2 || len(conn.cors.Headers) != 1 {
		t.Errorf("methods = %v headers = %v", conn.cors.Methods, conn.cors.Headers)
	}

	// No block means no cross-origin answers at all, which is the safe default
	// and a different thing from an empty list.
	if built(t, map[string]interface{}{}).cors != nil {
		t.Error("cross-origin answers were configured although nothing asked for them")
	}
}

func TestAnAuthTypeIsReadHoweverItIsCapitalised(t *testing.T) {
	// The word is also the name of a scheme, so it gets written the way
	// documentation spells it. Reading it strictly left the type set to
	// something the request path did not recognise, and every request was
	// turned away with "unknown auth type" — the configuration looked right.
	conn := built(t, map[string]interface{}{
		"auth": map[string]interface{}{
			"type": "JWT", "secret": "s3cret", "issuer": "https://issuer.example.com",
		},
	})
	if conn.authConfig.Type != "jwt" {
		t.Errorf("auth type = %q, want it read as jwt", conn.authConfig.Type)
	}
	if conn.authConfig.JWT == nil {
		t.Fatal("the JWT settings were not read")
	}
	if conn.authConfig.JWT.Issuer != "https://issuer.example.com" {
		t.Errorf("issuer = %q", conn.authConfig.JWT.Issuer)
	}
}

func TestAnAuthTypeTheServerCannotHonourIsRefused(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := NewFactory(logger).Create(context.Background(), &connector.Config{
		Name: "api", Type: "rest",
		Properties: map[string]interface{}{
			"auth": map[string]interface{}{"type": "jwtt", "secret": "s"},
		},
	})
	if err == nil {
		t.Fatal("a server was built that would turn away every request it received")
	}
}

func TestJWTSettingsAreReadInBothShapes(t *testing.T) {
	conn := built(t, map[string]interface{}{
		"auth": map[string]interface{}{
			"type": "jwt", "jwks_url": "https://issuer.example.com/.well-known/jwks.json",
			"audience":   []interface{}{"api.internal", "api.public"},
			"algorithms": []interface{}{"RS256"},
			"public":     []interface{}{"/health", "/metrics"},
		},
	})
	jwtCfg := conn.authConfig.JWT
	if len(jwtCfg.Audience) != 2 || len(jwtCfg.Algorithms) != 1 {
		t.Errorf("audience = %v algorithms = %v", jwtCfg.Audience, jwtCfg.Algorithms)
	}
	if len(conn.authConfig.Public) != 2 {
		t.Errorf("public paths = %v", conn.authConfig.Public)
	}

	// A single audience is written as one word rather than a list of one.
	single := built(t, map[string]interface{}{
		"auth": map[string]interface{}{"type": "jwt", "secret": "s", "audience": "api.internal"},
	})
	if len(single.authConfig.JWT.Audience) != 1 || single.authConfig.JWT.Audience[0] != "api.internal" {
		t.Errorf("audience = %v", single.authConfig.JWT.Audience)
	}
}

func TestAPIKeysAreReadInBothShapes(t *testing.T) {
	many := built(t, map[string]interface{}{
		"auth": map[string]interface{}{
			"type": "api_key", "header": "X-API-Key",
			"keys": []interface{}{"k1", "k2"},
		},
	})
	if len(many.authConfig.APIKey.Keys) != 2 || many.authConfig.APIKey.Header != "X-API-Key" {
		t.Errorf("api key config = %+v", many.authConfig.APIKey)
	}

	one := built(t, map[string]interface{}{
		"auth": map[string]interface{}{"type": "api_key", "keys": "only-one"},
	})
	if len(one.authConfig.APIKey.Keys) != 1 {
		t.Errorf("keys = %v", one.authConfig.APIKey.Keys)
	}
}

func TestKeysCanBeCheckedAgainstAConnector(t *testing.T) {
	// Keys that live in a database rather than the file are how a service
	// hands out keys without a redeploy.
	conn := built(t, map[string]interface{}{
		"auth": map[string]interface{}{
			"type": "api_key",
			"validate": map[string]interface{}{
				"connector": "connector.db",
				"query":     "SELECT * FROM api_keys WHERE key = :key",
			},
		},
	})
	apiKey := conn.authConfig.APIKey
	if apiKey.ValidateConnector != "connector.db" || apiKey.ValidateQuery == "" {
		t.Errorf("validation = %+v", apiKey)
	}
}

func TestTheDefaultFormatIsWhateverWasConfigured(t *testing.T) {
	if got := built(t, map[string]interface{}{"format": "xml"}).defaultFormat; got != "xml" {
		t.Errorf("format = %q", got)
	}
}

func TestTheFactoryAnswersForItsOwnType(t *testing.T) {
	f := NewFactory(nil)
	if !f.Supports("rest", "") || f.Supports("http", "") {
		t.Errorf("Supports(rest) = %v, Supports(http) = %v", f.Supports("rest", ""), f.Supports("http", ""))
	}
}
