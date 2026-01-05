package auth

import (
	"context"
	"testing"
	"time"
)

func TestMFAService(t *testing.T) {
	t.Run("disabled MFA", func(t *testing.T) {
		store := NewMemoryMFAStore()
		svc := NewMFAService(&MFAConfig{Enabled: false}, store)

		status, err := svc.GetStatus(context.Background(), "user1")
		if err != nil {
			t.Fatalf("GetStatus error: %v", err)
		}
		if status.Enabled {
			t.Error("expected MFA to be disabled")
		}
	})

	t.Run("initial status", func(t *testing.T) {
		store := NewMemoryMFAStore()
		svc := NewMFAService(&MFAConfig{Enabled: true}, store)

		status, err := svc.GetStatus(context.Background(), "user1")
		if err != nil {
			t.Fatalf("GetStatus error: %v", err)
		}
		if status.Enabled {
			t.Error("expected MFA to be disabled for new user")
		}
		if status.TOTPConfigured {
			t.Error("expected TOTP not configured")
		}
	})

	t.Run("TOTP setup flow", func(t *testing.T) {
		store := NewMemoryMFAStore()
		svc := NewMFAService(&MFAConfig{
			Enabled: true,
			TOTP:    &TOTPConfig{Issuer: "TestApp"},
			Recovery: &RecoveryConfig{
				CodeCount:  5,
				CodeLength: 8,
			},
		}, store)

		ctx := context.Background()
		userID := "user1"
		email := "test@example.com"

		// Begin TOTP setup
		setup, err := svc.BeginTOTPSetup(ctx, userID, email)
		if err != nil {
			t.Fatalf("BeginTOTPSetup error: %v", err)
		}

		if setup.Secret == "" {
			t.Error("expected secret to be set")
		}
		if setup.QRCode == "" {
			t.Error("expected QR code to be set")
		}
		if setup.ProvisioningURI == "" {
			t.Error("expected provisioning URI to be set")
		}

		// Verify the code and enable TOTP
		code := svc.totp.GenerateCode(setup.Secret)
		recoveryCodes, err := svc.ConfirmTOTPSetup(ctx, userID, code)
		if err != nil {
			t.Fatalf("ConfirmTOTPSetup error: %v", err)
		}

		if len(recoveryCodes) != 5 {
			t.Errorf("expected 5 recovery codes, got %d", len(recoveryCodes))
		}

		// Check status
		status, err := svc.GetStatus(ctx, userID)
		if err != nil {
			t.Fatalf("GetStatus error: %v", err)
		}
		if !status.Enabled {
			t.Error("expected MFA to be enabled")
		}
		if !status.TOTPConfigured {
			t.Error("expected TOTP to be configured")
		}
		if status.RecoveryCodesLeft != 5 {
			t.Errorf("expected 5 recovery codes left, got %d", status.RecoveryCodesLeft)
		}
	})

	t.Run("TOTP validation", func(t *testing.T) {
		store := NewMemoryMFAStore()
		svc := NewMFAService(&MFAConfig{Enabled: true}, store)

		ctx := context.Background()
		userID := "user1"

		// Setup TOTP
		setup, _ := svc.BeginTOTPSetup(ctx, userID, "test@example.com")
		code := svc.totp.GenerateCode(setup.Secret)
		svc.ConfirmTOTPSetup(ctx, userID, code)

		// Validate correct code
		newCode := svc.totp.GenerateCode(setup.Secret)
		err := svc.ValidateTOTP(ctx, userID, newCode)
		if err != nil {
			t.Errorf("ValidateTOTP should succeed with correct code: %v", err)
		}

		// Validate wrong code
		err = svc.ValidateTOTP(ctx, userID, "000000")
		if err == nil {
			t.Error("ValidateTOTP should fail with wrong code")
		}
	})

	t.Run("recovery code validation", func(t *testing.T) {
		store := NewMemoryMFAStore()
		svc := NewMFAService(&MFAConfig{
			Enabled: true,
			Recovery: &RecoveryConfig{
				CodeCount:  3,
				CodeLength: 8,
				GroupSize:  4,
			},
		}, store)

		ctx := context.Background()
		userID := "user1"

		// Setup TOTP to get recovery codes
		setup, _ := svc.BeginTOTPSetup(ctx, userID, "test@example.com")
		code := svc.totp.GenerateCode(setup.Secret)
		recoveryCodes, _ := svc.ConfirmTOTPSetup(ctx, userID, code)

		// Use first recovery code
		err := svc.ValidateRecoveryCode(ctx, userID, recoveryCodes[0])
		if err != nil {
			t.Errorf("ValidateRecoveryCode should succeed: %v", err)
		}

		// Check that code count decreased
		status, _ := svc.GetStatus(ctx, userID)
		if status.RecoveryCodesLeft != 2 {
			t.Errorf("expected 2 recovery codes left, got %d", status.RecoveryCodesLeft)
		}

		// Try to use same code again
		err = svc.ValidateRecoveryCode(ctx, userID, recoveryCodes[0])
		if err == nil {
			t.Error("ValidateRecoveryCode should fail for already used code")
		}

		// Invalid code
		err = svc.ValidateRecoveryCode(ctx, userID, "INVALID-CODE")
		if err == nil {
			t.Error("ValidateRecoveryCode should fail for invalid code")
		}
	})

	t.Run("regenerate recovery codes", func(t *testing.T) {
		store := NewMemoryMFAStore()
		svc := NewMFAService(&MFAConfig{
			Enabled:  true,
			Recovery: &RecoveryConfig{CodeCount: 3, CodeLength: 8},
		}, store)

		ctx := context.Background()
		userID := "user1"

		// Setup TOTP
		setup, _ := svc.BeginTOTPSetup(ctx, userID, "test@example.com")
		code := svc.totp.GenerateCode(setup.Secret)
		oldCodes, _ := svc.ConfirmTOTPSetup(ctx, userID, code)

		// Regenerate codes
		newCodes, err := svc.RegenerateRecoveryCodes(ctx, userID)
		if err != nil {
			t.Fatalf("RegenerateRecoveryCodes error: %v", err)
		}

		if len(newCodes) != 3 {
			t.Errorf("expected 3 new codes, got %d", len(newCodes))
		}

		// Old codes should not work
		err = svc.ValidateRecoveryCode(ctx, userID, oldCodes[0])
		if err == nil {
			t.Error("old recovery code should not work")
		}

		// New codes should work
		err = svc.ValidateRecoveryCode(ctx, userID, newCodes[0])
		if err != nil {
			t.Errorf("new recovery code should work: %v", err)
		}
	})

	t.Run("disable TOTP", func(t *testing.T) {
		store := NewMemoryMFAStore()
		svc := NewMFAService(&MFAConfig{Enabled: true}, store)

		ctx := context.Background()
		userID := "user1"

		// Setup TOTP
		setup, _ := svc.BeginTOTPSetup(ctx, userID, "test@example.com")
		code := svc.totp.GenerateCode(setup.Secret)
		svc.ConfirmTOTPSetup(ctx, userID, code)

		// Disable TOTP
		err := svc.DisableTOTP(ctx, userID)
		if err != nil {
			t.Fatalf("DisableTOTP error: %v", err)
		}

		// Check status
		status, _ := svc.GetStatus(ctx, userID)
		if status.Enabled {
			t.Error("expected MFA to be disabled")
		}
		if status.TOTPConfigured {
			t.Error("expected TOTP not configured")
		}
		if status.RecoveryCodesLeft != 0 {
			t.Error("expected recovery codes to be cleared")
		}
	})

	t.Run("cannot setup TOTP twice", func(t *testing.T) {
		store := NewMemoryMFAStore()
		svc := NewMFAService(&MFAConfig{Enabled: true}, store)

		ctx := context.Background()
		userID := "user1"

		// Setup TOTP first time
		setup, _ := svc.BeginTOTPSetup(ctx, userID, "test@example.com")
		code := svc.totp.GenerateCode(setup.Secret)
		svc.ConfirmTOTPSetup(ctx, userID, code)

		// Try to setup again
		_, err := svc.BeginTOTPSetup(ctx, userID, "test@example.com")
		if err != ErrMFAAlreadyEnabled {
			t.Errorf("expected ErrMFAAlreadyEnabled, got %v", err)
		}
	})
}

func TestTOTPService(t *testing.T) {
	t.Run("generate and validate code", func(t *testing.T) {
		svc := NewTOTPService(&TOTPConfig{
			Issuer:    "TestApp",
			Algorithm: "SHA1",
			Digits:    6,
			Period:    30,
			Skew:      1,
		})

		secret, err := svc.GenerateSecret()
		if err != nil {
			t.Fatalf("GenerateSecret error: %v", err)
		}

		code := svc.GenerateCode(secret)
		if len(code) != 6 {
			t.Errorf("expected 6 digit code, got %d", len(code))
		}

		if !svc.Validate(secret, code) {
			t.Error("code should be valid")
		}
	})

	t.Run("validate with skew", func(t *testing.T) {
		svc := NewTOTPService(&TOTPConfig{
			Digits: 6,
			Period: 30,
			Skew:   1,
		})

		secret, _ := svc.GenerateSecret()

		// Generate code for current time
		now := time.Now()
		code := svc.generateCode(secret, now)

		// Should be valid at current time
		if !svc.ValidateWithTime(secret, code, now) {
			t.Error("code should be valid at current time")
		}

		// Should be valid 30 seconds ago (within skew)
		if !svc.ValidateWithTime(secret, code, now.Add(-30*time.Second)) {
			t.Error("code should be valid within skew period")
		}

		// Should NOT be valid 90 seconds ago (outside skew)
		if svc.ValidateWithTime(secret, code, now.Add(-90*time.Second)) {
			t.Error("code should not be valid outside skew period")
		}
	})

	t.Run("generate URI", func(t *testing.T) {
		svc := NewTOTPService(&TOTPConfig{
			Issuer:    "MyApp",
			Algorithm: "SHA1",
			Digits:    6,
			Period:    30,
		})

		secret := "JBSWY3DPEHPK3PXP"
		uri := svc.GenerateURI(secret, "user@example.com")

		if uri == "" {
			t.Error("URI should not be empty")
		}
		if !contains(uri, "otpauth://totp/") {
			t.Error("URI should start with otpauth://totp/")
		}
		if !contains(uri, "secret="+secret) {
			t.Error("URI should contain secret")
		}
		if !contains(uri, "issuer=MyApp") {
			t.Error("URI should contain issuer")
		}
	})

	t.Run("generate QR code", func(t *testing.T) {
		svc := NewTOTPService(&TOTPConfig{Issuer: "Test"})

		uri := "otpauth://totp/Test:user@example.com?secret=JBSWY3DPEHPK3PXP"
		qr, err := svc.GenerateQRCode(uri)
		if err != nil {
			t.Fatalf("GenerateQRCode error: %v", err)
		}

		if qr == "" {
			t.Error("QR code should not be empty")
		}
		if !contains(qr, "data:image/png;base64,") {
			t.Error("QR code should be base64 PNG")
		}
	})
}

func TestMemoryMFAStore(t *testing.T) {
	t.Run("CRUD operations", func(t *testing.T) {
		store := NewMemoryMFAStore()
		ctx := context.Background()

		// Get non-existent data
		_, err := store.GetMFAData(ctx, "user1")
		if err == nil {
			t.Error("expected error for non-existent user")
		}

		// Save data
		data := &MFAUserData{
			UserID:      "user1",
			TOTPSecret:  "secret123",
			TOTPEnabled: true,
			CreatedAt:   time.Now(),
		}
		err = store.SaveMFAData(ctx, data)
		if err != nil {
			t.Fatalf("SaveMFAData error: %v", err)
		}

		// Get data
		retrieved, err := store.GetMFAData(ctx, "user1")
		if err != nil {
			t.Fatalf("GetMFAData error: %v", err)
		}
		if retrieved.TOTPSecret != "secret123" {
			t.Error("secret mismatch")
		}

		// Delete data
		err = store.DeleteMFAData(ctx, "user1")
		if err != nil {
			t.Fatalf("DeleteMFAData error: %v", err)
		}

		// Verify deleted
		_, err = store.GetMFAData(ctx, "user1")
		if err == nil {
			t.Error("expected error after deletion")
		}
	})
}

func TestRecoveryCodes(t *testing.T) {
	t.Run("generate random code", func(t *testing.T) {
		code1, err := generateRandomCode(8)
		if err != nil {
			t.Fatalf("generateRandomCode error: %v", err)
		}
		if len(code1) != 8 {
			t.Errorf("expected 8 characters, got %d", len(code1))
		}

		code2, _ := generateRandomCode(8)
		if code1 == code2 {
			t.Error("codes should be different")
		}
	})

	t.Run("format with groups", func(t *testing.T) {
		formatted := formatCodeWithGroups("ABCD1234", 4)
		if formatted != "ABCD-1234" {
			t.Errorf("expected ABCD-1234, got %s", formatted)
		}

		formatted = formatCodeWithGroups("ABC", 4)
		if formatted != "ABC" {
			t.Errorf("expected ABC (no dash for single group), got %s", formatted)
		}
	})

	t.Run("normalize code", func(t *testing.T) {
		normalized := normalizeRecoveryCode("ABCD-1234")
		if normalized != "ABCD1234" {
			t.Errorf("expected ABCD1234, got %s", normalized)
		}

		normalized = normalizeRecoveryCode("abcd 1234")
		if normalized != "ABCD1234" {
			t.Errorf("expected ABCD1234, got %s", normalized)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
