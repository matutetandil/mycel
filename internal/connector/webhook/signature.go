package webhook

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"hash"
	"strconv"
	"strings"
	"time"
)

// SignatureVerifier verifies webhook signatures
type SignatureVerifier struct {
	secret    []byte
	algorithm string
}

// NewSignatureVerifier creates a new signature verifier
func NewSignatureVerifier(secret, algorithm string) *SignatureVerifier {
	return &SignatureVerifier{
		secret:    []byte(secret),
		algorithm: algorithm,
	}
}

// Verify checks if the signature is valid
func (v *SignatureVerifier) Verify(payload []byte, signature string) bool {
	if v.algorithm == "none" || v.secret == nil || len(v.secret) == 0 {
		return true // No verification configured
	}

	expected := v.Sign(payload)

	// Handle prefixed signatures (e.g., "sha256=...")
	signature = v.normalizeSignature(signature)

	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}

// VerifyWithTimestamp verifies signature with timestamp for replay protection
func (v *SignatureVerifier) VerifyWithTimestamp(payload []byte, signature, timestamp string, tolerance time.Duration) error {
	if v.algorithm == "none" {
		return nil
	}

	// Check timestamp
	if timestamp != "" {
		ts, err := parseTimestamp(timestamp)
		if err != nil {
			return fmt.Errorf("invalid timestamp: %w", err)
		}

		age := time.Since(ts)
		if age > tolerance || age < -tolerance {
			return fmt.Errorf("timestamp outside tolerance: age=%v, tolerance=%v", age, tolerance)
		}

		// Sign payload with timestamp
		signedPayload := fmt.Sprintf("%s.%s", timestamp, string(payload))
		if !v.Verify([]byte(signedPayload), signature) {
			// Try without timestamp prefix (some providers don't include it)
			if !v.Verify(payload, signature) {
				return fmt.Errorf("invalid signature")
			}
		}
	} else {
		if !v.Verify(payload, signature) {
			return fmt.Errorf("invalid signature")
		}
	}

	return nil
}

// Sign generates a signature for the payload
func (v *SignatureVerifier) Sign(payload []byte) string {
	if v.algorithm == "none" || v.secret == nil {
		return ""
	}

	var h hash.Hash
	switch v.algorithm {
	case "hmac-sha256":
		h = hmac.New(sha256.New, v.secret)
	case "hmac-sha1":
		h = hmac.New(sha1.New, v.secret)
	default:
		h = hmac.New(sha256.New, v.secret)
	}

	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// SignWithTimestamp generates a signature including timestamp
func (v *SignatureVerifier) SignWithTimestamp(payload []byte, timestamp string) string {
	signedPayload := fmt.Sprintf("%s.%s", timestamp, string(payload))
	return v.Sign([]byte(signedPayload))
}

// normalizeSignature removes common prefixes from signatures
func (v *SignatureVerifier) normalizeSignature(sig string) string {
	// Handle "sha256=..." format (GitHub style)
	if strings.HasPrefix(sig, "sha256=") {
		return strings.TrimPrefix(sig, "sha256=")
	}
	if strings.HasPrefix(sig, "sha1=") {
		return strings.TrimPrefix(sig, "sha1=")
	}

	// Handle "v1=..." format (Stripe style)
	if strings.Contains(sig, "=") && strings.Contains(sig, ",") {
		// Stripe format: "t=timestamp,v1=signature"
		parts := strings.Split(sig, ",")
		for _, part := range parts {
			if strings.HasPrefix(part, "v1=") {
				return strings.TrimPrefix(part, "v1=")
			}
		}
	}

	return sig
}

// parseTimestamp parses various timestamp formats
func parseTimestamp(ts string) (time.Time, error) {
	// Try Unix timestamp (seconds)
	if unix, err := strconv.ParseInt(ts, 10, 64); err == nil {
		return time.Unix(unix, 0), nil
	}

	// Try RFC3339
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t, nil
	}

	// Try RFC3339Nano
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %s", ts)
}

// StripeSignatureVerifier handles Stripe's specific signature format
type StripeSignatureVerifier struct {
	secret []byte
}

// NewStripeSignatureVerifier creates a Stripe-specific verifier
func NewStripeSignatureVerifier(secret string) *StripeSignatureVerifier {
	return &StripeSignatureVerifier{
		secret: []byte(secret),
	}
}

// Verify checks a Stripe webhook signature
// Stripe format: "t=timestamp,v1=signature"
func (v *StripeSignatureVerifier) Verify(payload []byte, signatureHeader string, tolerance time.Duration) error {
	parts := strings.Split(signatureHeader, ",")

	var timestamp string
	var signatures []string

	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}

		switch kv[0] {
		case "t":
			timestamp = kv[1]
		case "v1":
			signatures = append(signatures, kv[1])
		}
	}

	if timestamp == "" {
		return fmt.Errorf("missing timestamp in signature header")
	}

	if len(signatures) == 0 {
		return fmt.Errorf("missing signature in signature header")
	}

	// Check timestamp
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}

	if time.Since(time.Unix(ts, 0)) > tolerance {
		return fmt.Errorf("timestamp too old")
	}

	// Compute expected signature
	signedPayload := fmt.Sprintf("%s.%s", timestamp, string(payload))
	h := hmac.New(sha256.New, v.secret)
	h.Write([]byte(signedPayload))
	expected := hex.EncodeToString(h.Sum(nil))

	// Check if any signature matches
	for _, sig := range signatures {
		if subtle.ConstantTimeCompare([]byte(expected), []byte(sig)) == 1 {
			return nil
		}
	}

	return fmt.Errorf("signature mismatch")
}

// GitHubSignatureVerifier handles GitHub's specific signature format
type GitHubSignatureVerifier struct {
	secret []byte
}

// NewGitHubSignatureVerifier creates a GitHub-specific verifier
func NewGitHubSignatureVerifier(secret string) *GitHubSignatureVerifier {
	return &GitHubSignatureVerifier{
		secret: []byte(secret),
	}
}

// Verify checks a GitHub webhook signature
// GitHub format: "sha256=signature" in X-Hub-Signature-256 header
func (v *GitHubSignatureVerifier) Verify(payload []byte, signatureHeader string) error {
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return fmt.Errorf("invalid signature format")
	}

	signature := strings.TrimPrefix(signatureHeader, "sha256=")

	h := hmac.New(sha256.New, v.secret)
	h.Write(payload)
	expected := hex.EncodeToString(h.Sum(nil))

	if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}
