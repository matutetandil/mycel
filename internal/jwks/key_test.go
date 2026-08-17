package jwks

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"math/big"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// The one question this package answers: can a signature be checked against
// what comes back? Both copies of this code — one in the REST connector, one in
// the gRPC one — returned a record of the numbers a key is made of rather than
// a key, so every token was refused with "key is of invalid type" and a service
// pointed at an identity provider rejected every authenticated request.

func rsaPair(t *testing.T) (*rsa.PrivateKey, Key) {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	return private, Key{
		Kty: "RSA", Kid: "k1", Alg: "RS256",
		N: base64.RawURLEncoding.EncodeToString(private.N.Bytes()),
		E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(private.E)).Bytes()),
	}
}

func ecPair(t *testing.T, curve elliptic.Curve, name string) (*ecdsa.PrivateKey, Key) {
	t.Helper()
	private, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	return private, Key{
		Kty: "EC", Kid: "k1", Crv: name,
		X: base64.RawURLEncoding.EncodeToString(private.X.Bytes()),
		Y: base64.RawURLEncoding.EncodeToString(private.Y.Bytes()),
	}
}

func TestASignatureCanBeCheckedAgainstWhatComesBack(t *testing.T) {
	// The whole point, and what neither copy could do.
	private, published := rsaPair(t)

	public, err := PublicKey(published)
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	if _, ok := public.(*rsa.PublicKey); !ok {
		t.Fatalf("an RSA key came back as %T, which no signature library can verify with", public)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "user-1"})
	signed, err := token.SignedString(private)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if _, err := jwt.Parse(signed, func(*jwt.Token) (interface{}, error) { return public, nil }); err != nil {
		t.Fatalf("a token signed with the published key was refused: %v", err)
	}
}

func TestEveryCurveAProviderPublishes(t *testing.T) {
	for name, tc := range map[string]struct {
		curve  elliptic.Curve
		method jwt.SigningMethod
	}{
		"P-256": {elliptic.P256(), jwt.SigningMethodES256},
		"P-384": {elliptic.P384(), jwt.SigningMethodES384},
		"P-521": {elliptic.P521(), jwt.SigningMethodES512},
	} {
		t.Run(name, func(t *testing.T) {
			private, published := ecPair(t, tc.curve, name)

			public, err := PublicKey(published)
			if err != nil {
				t.Fatalf("PublicKey: %v", err)
			}
			if _, ok := public.(*ecdsa.PublicKey); !ok {
				t.Fatalf("an EC key came back as %T", public)
			}

			token := jwt.NewWithClaims(tc.method, jwt.MapClaims{"sub": "user-1"})
			signed, err := token.SignedString(private)
			if err != nil {
				t.Fatalf("signing: %v", err)
			}
			if _, err := jwt.Parse(signed, func(*jwt.Token) (interface{}, error) { return public, nil }); err != nil {
				t.Fatalf("a token on %s was refused: %v", name, err)
			}
		})
	}
}

func TestATokenSignedWithAnotherKeyIsRefused(t *testing.T) {
	// A key that verifies nothing would be as useless as one that verifies
	// everything, and only this direction tells them apart.
	_, published := rsaPair(t)
	other, _ := rsaPair(t)

	public, err := PublicKey(published)
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "user-1"})
	signed, err := token.SignedString(other)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if _, err := jwt.Parse(signed, func(*jwt.Token) (interface{}, error) { return public, nil }); err == nil {
		t.Error("a token signed with a key nobody published was accepted")
	}
}

func TestPaddedBase64IsAccepted(t *testing.T) {
	// The specification says unpadded and some providers pad anyway. Refusing
	// theirs would look like a broken provider rather than a strict reader.
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	padded := Key{
		Kty: "RSA", Kid: "k1",
		N: base64.URLEncoding.EncodeToString(private.N.Bytes()),
		E: base64.URLEncoding.EncodeToString(big.NewInt(int64(private.E)).Bytes()),
	}

	public, err := PublicKey(padded)
	if err != nil {
		t.Fatalf("a padded key was refused: %v", err)
	}
	if got := public.(*rsa.PublicKey); got.N.Cmp(private.N) != 0 {
		t.Error("the padded key did not come back as the key it is")
	}
}

func TestAKeyThatCannotBeUsedIsRefusedByName(t *testing.T) {
	for name, key := range map[string]Key{
		"a kind nobody supports":       {Kty: "OKP", Kid: "k1", Crv: "Ed25519", X: "abc"},
		"no kind at all":               {Kid: "k1", N: "abc", E: "AQAB"},
		"a curve nobody supports":      {Kty: "EC", Kid: "k1", Crv: "P-192", X: "abc", Y: "def"},
		"an RSA key with no modulus":   {Kty: "RSA", Kid: "k1", E: "AQAB"},
		"an RSA key with no exponent":  {Kty: "RSA", Kid: "k1", N: "abc"},
		"a modulus that is not base64": {Kty: "RSA", Kid: "k1", N: "not base64!!", E: "AQAB"},
		"an EC key missing an axis":    {Kty: "EC", Kid: "k1", Crv: "P-256", X: "abc"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := PublicKey(key)
			if err == nil {
				t.Fatal("a key that cannot be used was accepted")
			}
			if !strings.Contains(err.Error(), "k1") {
				t.Errorf("error = %q, want it to name the key", err)
			}
		})
	}
}

func TestAKeyIsFoundByTheNameATokenGivesIt(t *testing.T) {
	// A provider publishes the outgoing key beside the incoming one during a
	// rotation, so the identifier is what picks between them.
	set := &Set{Keys: []Key{
		{Kty: "RSA", Kid: "old", N: "a", E: "AQAB"},
		{Kty: "RSA", Kid: "new", N: "b", E: "AQAB"},
	}}

	key, found := set.Find("new")
	if !found || key.N != "b" {
		t.Errorf("found = %v key = %+v", found, key)
	}
	if _, found := set.Find("neither"); found {
		t.Error("a key nobody published was found")
	}
	var absent *Set
	if _, found := absent.Find("old"); found {
		t.Error("a key was found in a set that does not exist")
	}
}
