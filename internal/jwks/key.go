// Package jwks turns a published JSON Web Key into a key a signature can be
// checked against.
//
// This existed twice — once in the REST connector, once in the gRPC one — and
// both copies returned an anonymous struct holding the numbers a key is made
// of rather than a key. No signature library can verify with that, so every
// token checked against a JWKS was refused with "key is of invalid type", and a
// service pointed at Auth0, Cognito or Keycloak rejected every authenticated
// request. The second copy is why this is now one: the same defect written
// twice is a defect that will be written a third time.
//
// It fails closed, which is the safe direction and the reason it could be taken
// for a configuration problem for a long time.
package jwks

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"math/big"
)

// Key is one entry of a published key set, in the shape a provider publishes it.
type Key struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`   // RSA modulus
	E   string `json:"e"`   // RSA exponent
	X   string `json:"x"`   // EC x coordinate
	Y   string `json:"y"`   // EC y coordinate
	Crv string `json:"crv"` // EC curve
}

// Set is what a provider publishes at its JWKS address.
type Set struct {
	Keys []Key `json:"keys"`
}

// Find returns the key a token names, if the set holds it.
func (s *Set) Find(kid string) (Key, bool) {
	if s == nil {
		return Key{}, false
	}
	for _, key := range s.Keys {
		if key.Kid == kid {
			return key, true
		}
	}
	return Key{}, false
}

// PublicKey builds the key a signature is checked against.
func PublicKey(key Key) (interface{}, error) {
	switch key.Kty {
	case "RSA":
		return rsaKey(key)
	case "EC":
		return ecKey(key)
	case "":
		return nil, fmt.Errorf("key %q says nothing about what kind of key it is", key.Kid)
	default:
		return nil, fmt.Errorf("key %q is a %s key, and only RSA and EC can be verified against", key.Kid, key.Kty)
	}
}

func rsaKey(key Key) (interface{}, error) {
	modulus, err := decode(key.N)
	if err != nil {
		return nil, fmt.Errorf("invalid RSA modulus in key %q: %w", key.Kid, err)
	}
	if len(modulus) == 0 {
		return nil, fmt.Errorf("key %q has no RSA modulus", key.Kid)
	}

	exponentBytes, err := decode(key.E)
	if err != nil {
		return nil, fmt.Errorf("invalid RSA exponent in key %q: %w", key.Kid, err)
	}
	if len(exponentBytes) == 0 {
		return nil, fmt.Errorf("key %q has no RSA exponent", key.Kid)
	}

	// A big-endian integer, almost always 65537.
	exponent := 0
	for _, b := range exponentBytes {
		exponent = exponent<<8 + int(b)
	}
	if exponent <= 0 {
		return nil, fmt.Errorf("key %q has an unusable RSA exponent", key.Kid)
	}

	return &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: exponent}, nil
}

func ecKey(key Key) (interface{}, error) {
	// The curve is named in the key rather than implied, and it decides how the
	// coordinates are read — so one we do not know is refused rather than
	// assumed to be P-256, which would verify nothing and report a bad
	// signature instead.
	var curve elliptic.Curve
	switch key.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("key %q names curve %q, which is not one of P-256, P-384 or P-521", key.Kid, key.Crv)
	}

	x, err := decode(key.X)
	if err != nil {
		return nil, fmt.Errorf("invalid EC x coordinate in key %q: %w", key.Kid, err)
	}
	y, err := decode(key.Y)
	if err != nil {
		return nil, fmt.Errorf("invalid EC y coordinate in key %q: %w", key.Kid, err)
	}
	if len(x) == 0 || len(y) == 0 {
		return nil, fmt.Errorf("key %q is missing a coordinate", key.Kid)
	}

	return &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(x),
		Y:     new(big.Int).SetBytes(y),
	}, nil
}

// decode reads a base64url value, with or without the padding some providers
// leave on.
func decode(value string) ([]byte, error) {
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(value)
}
