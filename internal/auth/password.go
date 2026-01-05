package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/crypto/argon2"
)

// PasswordHasher handles password hashing and verification
type PasswordHasher struct {
	config *PasswordConfig
}

// NewPasswordHasher creates a new password hasher
func NewPasswordHasher(config *PasswordConfig) *PasswordHasher {
	if config == nil {
		config = &PasswordConfig{
			Algorithm:   "argon2id",
			Memory:      65536,
			Iterations:  3,
			Parallelism: 2,
			SaltLength:  16,
			KeyLength:   32,
		}
	}

	// Set defaults if not provided
	if config.Memory == 0 {
		config.Memory = 65536
	}
	if config.Iterations == 0 {
		config.Iterations = 3
	}
	if config.Parallelism == 0 {
		config.Parallelism = 2
	}
	if config.SaltLength == 0 {
		config.SaltLength = 16
	}
	if config.KeyLength == 0 {
		config.KeyLength = 32
	}

	return &PasswordHasher{config: config}
}

// Hash hashes a password using argon2id
func (h *PasswordHasher) Hash(password string) (string, error) {
	// Generate random salt
	salt := make([]byte, h.config.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	// Hash the password
	hash := argon2.IDKey(
		[]byte(password),
		salt,
		h.config.Iterations,
		h.config.Memory,
		h.config.Parallelism,
		h.config.KeyLength,
	)

	// Encode as PHC string format
	// $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.config.Memory,
		h.config.Iterations,
		h.config.Parallelism,
		b64Salt,
		b64Hash,
	)

	return encoded, nil
}

// Verify verifies a password against a hash
func (h *PasswordHasher) Verify(password, encodedHash string) (bool, error) {
	// Parse the encoded hash
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, errors.New("invalid hash format")
	}

	if parts[1] != "argon2id" {
		return false, errors.New("unsupported algorithm")
	}

	var version int
	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil {
		return false, fmt.Errorf("invalid version: %w", err)
	}

	var memory, iterations uint32
	var parallelism uint8
	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism)
	if err != nil {
		return false, fmt.Errorf("invalid parameters: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("invalid salt: %w", err)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("invalid hash: %w", err)
	}

	// Compute hash with same parameters
	computedHash := argon2.IDKey(
		[]byte(password),
		salt,
		iterations,
		memory,
		parallelism,
		uint32(len(expectedHash)),
	)

	// Constant-time comparison
	return subtle.ConstantTimeCompare(computedHash, expectedHash) == 1, nil
}

// PasswordValidator validates passwords against policy
type PasswordValidator struct {
	config *PasswordConfig
}

// NewPasswordValidator creates a new password validator
func NewPasswordValidator(config *PasswordConfig) *PasswordValidator {
	if config == nil {
		config = &PasswordConfig{
			MinLength: 8,
			MaxLength: 128,
		}
	}
	return &PasswordValidator{config: config}
}

// ValidationError contains password validation errors
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return strings.Join(e.Errors, "; ")
}

// Validate validates a password against the policy
func (v *PasswordValidator) Validate(password string, user *User) error {
	var errors []string

	// Length check
	if len(password) < v.config.MinLength {
		errors = append(errors, fmt.Sprintf("password must be at least %d characters", v.config.MinLength))
	}
	if v.config.MaxLength > 0 && len(password) > v.config.MaxLength {
		errors = append(errors, fmt.Sprintf("password must be at most %d characters", v.config.MaxLength))
	}

	// Complexity checks
	if v.config.RequireUpper && !hasUppercase(password) {
		errors = append(errors, "password must contain at least one uppercase letter")
	}
	if v.config.RequireLower && !hasLowercase(password) {
		errors = append(errors, "password must contain at least one lowercase letter")
	}
	if v.config.RequireNumber && !hasNumber(password) {
		errors = append(errors, "password must contain at least one number")
	}
	if v.config.RequireSpecial && !hasSpecial(password) {
		errors = append(errors, "password must contain at least one special character")
	}

	// Reject patterns
	for _, pattern := range v.config.RejectPatterns {
		// Handle dynamic patterns referencing user fields
		actualPattern := pattern
		if user != nil {
			actualPattern = strings.ReplaceAll(actualPattern, "user.email", user.Email)
		}

		// Check if it's a regex or literal
		if strings.HasPrefix(actualPattern, "^") || strings.HasSuffix(actualPattern, "$") {
			re, err := regexp.Compile("(?i)" + actualPattern)
			if err == nil && re.MatchString(password) {
				errors = append(errors, "password matches a rejected pattern")
			}
		} else {
			// Literal substring check (case insensitive)
			if strings.Contains(strings.ToLower(password), strings.ToLower(actualPattern)) {
				errors = append(errors, fmt.Sprintf("password cannot contain '%s'", actualPattern))
			}
		}
	}

	if len(errors) > 0 {
		return &ValidationError{Errors: errors}
	}

	return nil
}

// ValidateStrength returns a password strength score (0-100)
func (v *PasswordValidator) ValidateStrength(password string) int {
	score := 0

	// Length score (up to 30 points)
	length := len(password)
	if length >= 8 {
		score += 10
	}
	if length >= 12 {
		score += 10
	}
	if length >= 16 {
		score += 10
	}

	// Character variety (up to 40 points)
	if hasLowercase(password) {
		score += 10
	}
	if hasUppercase(password) {
		score += 10
	}
	if hasNumber(password) {
		score += 10
	}
	if hasSpecial(password) {
		score += 10
	}

	// Uniqueness (up to 30 points)
	uniqueChars := countUniqueChars(password)
	if uniqueChars >= 6 {
		score += 10
	}
	if uniqueChars >= 10 {
		score += 10
	}
	if uniqueChars >= 14 {
		score += 10
	}

	return score
}

func hasUppercase(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func hasLowercase(s string) bool {
	for _, r := range s {
		if unicode.IsLower(r) {
			return true
		}
	}
	return false
}

func hasNumber(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func hasSpecial(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func countUniqueChars(s string) int {
	seen := make(map[rune]bool)
	for _, r := range s {
		seen[r] = true
	}
	return len(seen)
}

// GenerateRandomPassword generates a random password meeting the policy
func GenerateRandomPassword(length int) (string, error) {
	const (
		lowercase = "abcdefghijklmnopqrstuvwxyz"
		uppercase = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		numbers   = "0123456789"
		special   = "!@#$%^&*()_+-=[]{}|;:,.<>?"
	)

	if length < 8 {
		length = 8
	}

	// Ensure at least one of each type
	password := make([]byte, length)

	// Fill with random characters from all sets
	allChars := lowercase + uppercase + numbers + special

	randomBytes := make([]byte, length)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	for i := 0; i < length; i++ {
		password[i] = allChars[randomBytes[i]%byte(len(allChars))]
	}

	// Ensure at least one of each required type
	randomBytes = make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	password[0] = lowercase[randomBytes[0]%byte(len(lowercase))]
	password[1] = uppercase[randomBytes[1]%byte(len(uppercase))]
	password[2] = numbers[randomBytes[2]%byte(len(numbers))]
	password[3] = special[randomBytes[3]%byte(len(special))]

	// Shuffle the password
	shuffled := make([]byte, length)
	shuffleBytes := make([]byte, length)
	if _, err := rand.Read(shuffleBytes); err != nil {
		return "", err
	}

	indices := make([]int, length)
	for i := range indices {
		indices[i] = i
	}

	for i := length - 1; i > 0; i-- {
		j := int(shuffleBytes[i]) % (i + 1)
		indices[i], indices[j] = indices[j], indices[i]
	}

	for i, idx := range indices {
		shuffled[i] = password[idx]
	}

	return string(shuffled), nil
}
