package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash"
	"image/png"
	"net/url"
	"strings"
	"time"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
)

// TOTPService handles TOTP operations
type TOTPService struct {
	config *TOTPConfig
}

// NewTOTPService creates a new TOTP service
func NewTOTPService(config *TOTPConfig) *TOTPService {
	if config == nil {
		config = &TOTPConfig{
			Issuer:    "Mycel",
			Algorithm: "SHA1",
			Digits:    6,
			Period:    30,
			Skew:      1,
		}
	}
	return &TOTPService{config: config}
}

// GenerateSecret generates a new TOTP secret
func (s *TOTPService) GenerateSecret() (string, error) {
	return GenerateSecret(20) // 160 bits
}

// GenerateURI creates an otpauth:// URI for authenticator apps
func (s *TOTPService) GenerateURI(secret, accountName string) string {
	// Format: otpauth://totp/ISSUER:ACCOUNT?secret=SECRET&issuer=ISSUER&algorithm=SHA1&digits=6&period=30
	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", s.config.Issuer)
	params.Set("algorithm", strings.ToUpper(s.config.Algorithm))
	params.Set("digits", fmt.Sprintf("%d", s.config.Digits))
	params.Set("period", fmt.Sprintf("%d", s.config.Period))

	label := url.PathEscape(fmt.Sprintf("%s:%s", s.config.Issuer, accountName))

	return fmt.Sprintf("otpauth://totp/%s?%s", label, params.Encode())
}

// GenerateQRCode generates a QR code as base64 PNG
func (s *TOTPService) GenerateQRCode(uri string) (string, error) {
	// Create QR code
	qrCode, err := qr.Encode(uri, qr.M, qr.Auto)
	if err != nil {
		return "", fmt.Errorf("failed to encode QR code: %w", err)
	}

	// Scale to reasonable size
	qrCode, err = barcode.Scale(qrCode, 200, 200)
	if err != nil {
		return "", fmt.Errorf("failed to scale QR code: %w", err)
	}

	// Encode as PNG to buffer
	var buf strings.Builder
	encoder := base64.NewEncoder(base64.StdEncoding, &buf)

	if err := png.Encode(encoder, qrCode); err != nil {
		return "", fmt.Errorf("failed to encode PNG: %w", err)
	}
	encoder.Close()

	return "data:image/png;base64," + buf.String(), nil
}

// Validate validates a TOTP code
func (s *TOTPService) Validate(secret, code string) bool {
	return s.ValidateWithTime(secret, code, time.Now())
}

// ValidateWithTime validates a TOTP code at a specific time
func (s *TOTPService) ValidateWithTime(secret, code string, t time.Time) bool {
	// Check current time and allowed skew
	for i := -s.config.Skew; i <= s.config.Skew; i++ {
		checkTime := t.Add(time.Duration(i*s.config.Period) * time.Second)
		expectedCode := s.generateCode(secret, checkTime)
		if code == expectedCode {
			return true
		}
	}
	return false
}

// GenerateCode generates a TOTP code for the current time
func (s *TOTPService) GenerateCode(secret string) string {
	return s.generateCode(secret, time.Now())
}

// generateCode generates a TOTP code for a specific time (RFC 6238)
func (s *TOTPService) generateCode(secret string, t time.Time) string {
	// Decode secret
	secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}

	// Calculate time counter
	counter := uint64(t.Unix()) / uint64(s.config.Period)

	// Convert counter to bytes (big endian)
	counterBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(counterBytes, counter)

	// Get hash function
	var h func() hash.Hash
	switch strings.ToUpper(s.config.Algorithm) {
	case "SHA256":
		h = sha256.New
	case "SHA512":
		h = sha512.New
	default:
		h = sha1.New
	}

	// Calculate HMAC
	mac := hmac.New(h, secretBytes)
	mac.Write(counterBytes)
	hmacResult := mac.Sum(nil)

	// Dynamic truncation (RFC 4226)
	offset := hmacResult[len(hmacResult)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(hmacResult[offset:offset+4]) & 0x7fffffff

	// Generate code with specified digits
	code := truncated % pow10(s.config.Digits)

	// Pad with leading zeros
	format := fmt.Sprintf("%%0%dd", s.config.Digits)
	return fmt.Sprintf(format, code)
}

// pow10 returns 10^n
func pow10(n int) uint32 {
	result := uint32(1)
	for i := 0; i < n; i++ {
		result *= 10
	}
	return result
}

// ValidateCode is a standalone function to validate TOTP
func ValidateTOTPCode(secret, code string, config *TOTPConfig) bool {
	svc := NewTOTPService(config)
	return svc.Validate(secret, code)
}
