package rest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// A JWKS is how a service checks tokens it did not issue: the identity
// provider publishes its public keys, the token names which one signed it, and
// the signature is verified against that key. Every request to an
// Auth0-, Cognito- or Keycloak-backed service goes through it.
//
// It returned an anonymous struct holding the modulus and exponent rather than
// a key any signature library can use, so every token was refused with "key is
// of invalid type". Failing closed is the safe direction and the reason this
// could be mistaken for a configuration problem for a long time.

func rsaJWK(t *testing.T, kid string) (*rsa.PrivateKey, JWK) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	return key, JWK{
		Kty: "RSA", Kid: kid, Alg: "RS256", Use: "sig",
		N: base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

func ecJWK(t *testing.T, kid string, curve elliptic.Curve, name string) (*ecdsa.PrivateKey, JWK) {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	return key, JWK{
		Kty: "EC", Kid: kid, Use: "sig", Crv: name,
		X: base64.RawURLEncoding.EncodeToString(key.X.Bytes()),
		Y: base64.RawURLEncoding.EncodeToString(key.Y.Bytes()),
	}
}

func signedWith(t *testing.T, method jwt.SigningMethod, key interface{}, kid string) string {
	t.Helper()
	token := jwt.NewWithClaims(method, jwt.MapClaims{"sub": "user-1"})
	if kid != "" {
		token.Header["kid"] = kid
	}
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

// verifying is what the connector does: hand the token's header to the key
// lookup and check the signature against whatever comes back.
func verifying(t *testing.T, c *Connector, token string) error {
	t.Helper()
	_, err := jwt.Parse(token, func(tok *jwt.Token) (interface{}, error) {
		return c.getJWKSKey(tok)
	})
	return err
}

func connectorFor(t *testing.T, jwksURL string) *Connector {
	t.Helper()
	return &Connector{authConfig: &AuthConfig{
		Type: "jwt",
		JWT:  &JWTAuthConfig{JWKSURL: jwksURL},
	}}
}

// publishing serves a key set and counts how often it is asked for.
func publishing(t *testing.T, keys *atomic.Value) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var fetches atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		set, _ := keys.Load().(JWKS)
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(server.Close)
	return server, &fetches
}

func TestATokenSignedWithAPublishedRSAKeyIsAccepted(t *testing.T) {
	private, jwk := rsaJWK(t, "k1")
	var keys atomic.Value
	keys.Store(JWKS{Keys: []JWK{jwk}})
	server, _ := publishing(t, &keys)

	err := verifying(t, connectorFor(t, server.URL), signedWith(t, jwt.SigningMethodRS256, private, "k1"))
	if err != nil {
		t.Fatalf("a token signed with a published key was refused: %v", err)
	}
}

func TestATokenSignedWithAPublishedECKeyIsAccepted(t *testing.T) {
	for name, curve := range map[string]struct {
		curve  elliptic.Curve
		method jwt.SigningMethod
	}{
		"P-256": {elliptic.P256(), jwt.SigningMethodES256},
		"P-384": {elliptic.P384(), jwt.SigningMethodES384},
		"P-521": {elliptic.P521(), jwt.SigningMethodES512},
	} {
		t.Run(name, func(t *testing.T) {
			private, jwk := ecJWK(t, "k1", curve.curve, name)
			var keys atomic.Value
			keys.Store(JWKS{Keys: []JWK{jwk}})
			server, _ := publishing(t, &keys)

			err := verifying(t, connectorFor(t, server.URL), signedWith(t, curve.method, private, "k1"))
			if err != nil {
				t.Fatalf("a token on %s was refused: %v", name, err)
			}
		})
	}
}

func TestATokenSignedWithAnotherKeyIsRefused(t *testing.T) {
	// The whole point. A key that verifies nothing would be as bad as one that
	// verifies everything.
	_, jwk := rsaJWK(t, "k1")
	other, _ := rsaJWK(t, "k1") // same identifier, different key
	var keys atomic.Value
	keys.Store(JWKS{Keys: []JWK{jwk}})
	server, _ := publishing(t, &keys)

	err := verifying(t, connectorFor(t, server.URL), signedWith(t, jwt.SigningMethodRS256, other, "k1"))
	if err == nil {
		t.Fatal("a token signed with a key nobody published was accepted")
	}
}

func TestTheRightKeyIsChosenFromSeveral(t *testing.T) {
	// A provider publishes the outgoing key beside the incoming one during a
	// rotation, so picking by identifier rather than by position matters.
	first, jwkOne := rsaJWK(t, "old")
	second, jwkTwo := rsaJWK(t, "new")
	var keys atomic.Value
	keys.Store(JWKS{Keys: []JWK{jwkOne, jwkTwo}})
	server, _ := publishing(t, &keys)
	connector := connectorFor(t, server.URL)

	if err := verifying(t, connector, signedWith(t, jwt.SigningMethodRS256, first, "old")); err != nil {
		t.Errorf("a token signed with the outgoing key was refused: %v", err)
	}
	if err := verifying(t, connector, signedWith(t, jwt.SigningMethodRS256, second, "new")); err != nil {
		t.Errorf("a token signed with the incoming key was refused: %v", err)
	}

	// And a token signed with one key while naming the other is not accepted.
	if err := verifying(t, connector, signedWith(t, jwt.SigningMethodRS256, first, "new")); err == nil {
		t.Error("a token naming a key it was not signed with was accepted")
	}
}

func TestTheKeySetIsFetchedOnceAndKept(t *testing.T) {
	// Fetching per request would put an HTTP call in front of every
	// authenticated one.
	private, jwk := rsaJWK(t, "k1")
	var keys atomic.Value
	keys.Store(JWKS{Keys: []JWK{jwk}})
	server, fetches := publishing(t, &keys)
	connector := connectorFor(t, server.URL)

	for i := 0; i < 5; i++ {
		if err := verifying(t, connector, signedWith(t, jwt.SigningMethodRS256, private, "k1")); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	if got := fetches.Load(); got != 1 {
		t.Errorf("the key set was fetched %d times for five requests", got)
	}
}

func TestAKeyPublishedAfterTheSetWasCachedIsFound(t *testing.T) {
	// A rotation: the provider starts signing with a key that did not exist
	// when the set was cached. Holding the old set for ever would refuse every
	// request from that moment until somebody restarted the service.
	oldKey, oldJWK := rsaJWK(t, "old")
	var keys atomic.Value
	keys.Store(JWKS{Keys: []JWK{oldJWK}})
	server, fetches := publishing(t, &keys)
	connector := connectorFor(t, server.URL)

	if err := verifying(t, connector, signedWith(t, jwt.SigningMethodRS256, oldKey, "old")); err != nil {
		t.Fatalf("before the rotation: %v", err)
	}

	newKey, newJWK := rsaJWK(t, "new")
	keys.Store(JWKS{Keys: []JWK{oldJWK, newJWK}})

	if err := verifying(t, connector, signedWith(t, jwt.SigningMethodRS256, newKey, "new")); err != nil {
		t.Fatalf("a token signed with a rotated-in key was refused: %v", err)
	}
	if got := fetches.Load(); got != 2 {
		t.Errorf("the set was fetched %d times, want one refresh for the unknown key", got)
	}
}

func TestAnUnknownKeyIsNotFetchedOverAndOver(t *testing.T) {
	// A token naming a key nobody publishes — a forgery, or one from another
	// issuer — must not turn every request into a fetch, which is how a
	// service is made to attack its own identity provider.
	private, jwk := rsaJWK(t, "k1")
	var keys atomic.Value
	keys.Store(JWKS{Keys: []JWK{jwk}})
	server, fetches := publishing(t, &keys)
	connector := connectorFor(t, server.URL)

	for i := 0; i < 3; i++ {
		if err := verifying(t, connector, signedWith(t, jwt.SigningMethodRS256, private, "not-published")); err == nil {
			t.Fatal("a token naming a key nobody publishes was accepted")
		}
	}
	// One fetch to fill the cache, then one refresh per unknown key.
	if got := fetches.Load(); got > 4 {
		t.Errorf("the provider was asked %d times for three requests", got)
	}
}

func TestATokenNamingNoKeyIsRefusedWithoutFetching(t *testing.T) {
	// There is nothing to look up, so there is nothing to fetch.
	private, jwk := rsaJWK(t, "k1")
	var keys atomic.Value
	keys.Store(JWKS{Keys: []JWK{jwk}})
	server, fetches := publishing(t, &keys)

	err := verifying(t, connectorFor(t, server.URL), signedWith(t, jwt.SigningMethodRS256, private, ""))
	if err == nil {
		t.Fatal("a token naming no key was accepted")
	}
	if got := fetches.Load(); got != 0 {
		t.Errorf("the provider was asked %d times for a token that names no key", got)
	}
}

func TestAProviderThatCannotBeReachedIsReported(t *testing.T) {
	private, _ := rsaJWK(t, "k1")
	connector := connectorFor(t, "http://127.0.0.1:1/keys")

	err := verifying(t, connector, signedWith(t, jwt.SigningMethodRS256, private, "k1"))
	if err == nil {
		t.Fatal("a token was accepted although the key set could not be fetched")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "jwks") {
		t.Errorf("error = %q, want it to name what could not be fetched", err)
	}
}

func TestAKeySetThatIsNotOneIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>an error page</html>"))
	}))
	defer server.Close()

	private, _ := rsaJWK(t, "k1")
	if err := verifying(t, connectorFor(t, server.URL), signedWith(t, jwt.SigningMethodRS256, private, "k1")); err == nil {
		t.Error("a key set that is not JSON was accepted")
	}
}

func TestAKeyWeCannotUseIsRefusedByName(t *testing.T) {
	for name, key := range map[string]JWK{
		"a kind of key nobody supports":       {Kty: "OKP", Kid: "k1", Crv: "Ed25519", X: "abc"},
		"a curve nobody supports":             {Kty: "EC", Kid: "k1", Crv: "P-192", X: "abc", Y: "def"},
		"an RSA key with no modulus":          {Kty: "RSA", Kid: "k1", E: "AQAB"},
		"an RSA modulus that is not base64":   {Kty: "RSA", Kid: "k1", N: "not base64!!", E: "AQAB"},
		"an EC coordinate that is not base64": {Kty: "EC", Kid: "k1", Crv: "P-256", X: "not base64!!", Y: "abc"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseJWK(key)
			if err == nil {
				t.Fatal("a key that cannot be used was accepted")
			}
			if !strings.Contains(err.Error(), "k1") && !strings.Contains(err.Error(), "OKP") {
				t.Errorf("error = %q, want it to name the key or its kind", err)
			}
		})
	}
}

func TestAParsedKeyIsTheKindASignatureLibraryUses(t *testing.T) {
	// The defect, stated directly: what comes back has to be a key, not a
	// record of the numbers a key is made of.
	_, rsaKey := rsaJWK(t, "k1")
	parsed, err := parseJWK(rsaKey)
	if err != nil {
		t.Fatalf("parseJWK: %v", err)
	}
	if _, ok := parsed.(*rsa.PublicKey); !ok {
		t.Errorf("an RSA key parsed to %T, which no signature library can verify with", parsed)
	}

	_, ecKey := ecJWK(t, "k2", elliptic.P256(), "P-256")
	parsed, err = parseJWK(ecKey)
	if err != nil {
		t.Fatalf("parseJWK: %v", err)
	}
	if _, ok := parsed.(*ecdsa.PublicKey); !ok {
		t.Errorf("an EC key parsed to %T", parsed)
	}
}

func TestARequestCarryingAValidTokenIsAuthenticated(t *testing.T) {
	// The whole path a request takes, rather than the signature check alone:
	// the header is read, the scheme stripped, the algorithm checked against
	// what is allowed, the key fetched, the signature verified, and the claims
	// handed on. A fix to one step is only a fix if the request gets through.
	private, jwk := rsaJWK(t, "k1")
	var keys atomic.Value
	keys.Store(JWKS{Keys: []JWK{jwk}})
	server, _ := publishing(t, &keys)

	c := &Connector{authConfig: &AuthConfig{
		Type: "jwt",
		JWT: &JWTAuthConfig{
			JWKSURL:    server.URL,
			Algorithms: []string{"RS256"},
		},
	}}

	request := httptest.NewRequest(http.MethodGet, "/orders", nil)
	request.Header.Set("Authorization", "Bearer "+signedWith(t, jwt.SigningMethodRS256, private, "k1"))

	authContext, err := c.validateJWT(request)
	if err != nil {
		t.Fatalf("a request with a valid token was refused: %v", err)
	}
	if authContext == nil {
		t.Fatal("no identity came back for an authenticated request")
	}
	if !authContext.Authenticated {
		t.Error("the request came back unauthenticated")
	}
	// The claims have to arrive too: they are what a flow reads as auth.* and
	// what any authorization decision is made on.
	if authContext.Claims == nil || authContext.Claims["sub"] != "user-1" {
		t.Errorf("claims = %v, want the subject from the token", authContext.Claims)
	}
}

func TestARequestIsRefusedForEachWayATokenCanBeWrong(t *testing.T) {
	private, jwk := rsaJWK(t, "k1")
	forged, _ := rsaJWK(t, "k1")
	var keys atomic.Value
	keys.Store(JWKS{Keys: []JWK{jwk}})
	server, _ := publishing(t, &keys)

	c := &Connector{authConfig: &AuthConfig{
		Type: "jwt",
		JWT: &JWTAuthConfig{
			JWKSURL:    server.URL,
			Algorithms: []string{"RS256"},
		},
	}}

	for name, header := range map[string]string{
		"no header at all":             "",
		"the token without a scheme":   signedWith(t, jwt.SigningMethodRS256, private, "k1"),
		"another scheme":               "Basic " + signedWith(t, jwt.SigningMethodRS256, private, "k1"),
		"signed by somebody else":      "Bearer " + signedWith(t, jwt.SigningMethodRS256, forged, "k1"),
		"naming a key nobody has":      "Bearer " + signedWith(t, jwt.SigningMethodRS256, private, "unknown"),
		"nonsense in place of a token": "Bearer not-a-token",
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/orders", nil)
			if header != "" {
				request.Header.Set("Authorization", header)
			}
			if _, err := c.validateJWT(request); err == nil {
				t.Error("the request was authenticated")
			}
		})
	}
}
