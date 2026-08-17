package auth

import "testing"

// A preset is the security posture someone gets by naming one word. Nobody
// reads the four of them side by side, so what these tests are really for is
// the day one value is edited: a change that quietly relaxes production is the
// failure mode, and it should have to be written down here as well.

func TestGetPresetByName(t *testing.T) {
	for name, want := range map[string]string{
		PresetStrict:      PresetStrict,
		PresetStandard:    PresetStandard,
		PresetRelaxed:     PresetRelaxed,
		PresetDevelopment: PresetDevelopment,
	} {
		if got := GetPreset(name).Preset; got != want {
			t.Errorf("GetPreset(%q) = %q", name, got)
		}
	}
}

func TestAnUnknownPresetFallsBackToStandard(t *testing.T) {
	// Not to development, and not to nothing: a typo in the preset name must
	// not be the reason a service runs with the loosest settings.
	for _, name := range []string{"", "strick", "prod", "none"} {
		if got := GetPreset(name).Preset; got != PresetStandard {
			t.Errorf("GetPreset(%q) = %q, want the standard preset", name, got)
		}
	}
}

func TestTheOrderingThatMakesPresetsMeanSomething(t *testing.T) {
	strict, standard, relaxed, dev := GetPreset(PresetStrict), GetPreset(PresetStandard),
		GetPreset(PresetRelaxed), GetPreset(PresetDevelopment)

	// Password length only ever loosens as the preset does.
	if !(strict.Password.MinLength >= standard.Password.MinLength &&
		standard.Password.MinLength >= relaxed.Password.MinLength &&
		relaxed.Password.MinLength >= dev.Password.MinLength) {
		t.Errorf("minimum password length is not ordered: %d, %d, %d, %d",
			strict.Password.MinLength, standard.Password.MinLength,
			relaxed.Password.MinLength, dev.Password.MinLength)
	}

	// Every preset hashes with argon2id. A preset is a posture, not a licence
	// to store passwords differently.
	for name, cfg := range map[string]*Config{
		PresetStrict: strict, PresetStandard: standard,
		PresetRelaxed: relaxed, PresetDevelopment: dev,
	} {
		if cfg.Password.Algorithm != "argon2id" {
			t.Errorf("%s hashes with %q", name, cfg.Password.Algorithm)
		}
		if cfg.Password.SaltLength < 16 || cfg.Password.KeyLength < 32 {
			t.Errorf("%s uses salt %d and key %d", name, cfg.Password.SaltLength, cfg.Password.KeyLength)
		}
	}

	// The strictest preset is the one that demands a second factor, and the
	// development one is the only one that turns MFA off — which is why
	// enrolling TOTP against it needs the flag set by hand.
	if strict.MFA.Required != "true" {
		t.Errorf("the strict preset does not require MFA: %q", strict.MFA.Required)
	}
	if dev.MFA.Required != "false" {
		t.Errorf("the development preset requires MFA: %q", dev.MFA.Required)
	}
}

func TestMergeFillsOnlyWhatWasLeftOut(t *testing.T) {
	// The point of writing a preset name plus a few attributes: the attributes
	// win, and everything else comes from the preset rather than from zero.
	cfg := &Config{
		Preset:   PresetStrict,
		JWT:      &JWTConfig{Algorithm: "HS512"},
		Password: &PasswordConfig{MinLength: 20},
	}

	merged := MergeWithPreset(cfg)
	strict := GetPreset(PresetStrict)

	if merged.JWT.Algorithm != "HS512" {
		t.Errorf("the written algorithm was replaced: %q", merged.JWT.Algorithm)
	}
	if merged.JWT.AccessLifetime != strict.JWT.AccessLifetime {
		t.Errorf("access lifetime = %q, want the preset's %q",
			merged.JWT.AccessLifetime, strict.JWT.AccessLifetime)
	}
	if merged.Password.MinLength != 20 {
		t.Errorf("the written minimum length was replaced: %d", merged.Password.MinLength)
	}
	// The hashing parameters are exactly what a person leaves out, and getting
	// zeros for them would be an unusable Argon2 configuration.
	if merged.Password.Memory != strict.Password.Memory ||
		merged.Password.Iterations != strict.Password.Iterations ||
		merged.Password.Parallelism != strict.Password.Parallelism ||
		merged.Password.SaltLength != strict.Password.SaltLength ||
		merged.Password.KeyLength != strict.Password.KeyLength {
		t.Errorf("hashing parameters were not filled in: %+v", merged.Password)
	}
}

func TestMergeSuppliesWholeBlocksThatWereNotWritten(t *testing.T) {
	cfg := &Config{Preset: PresetStandard}

	merged := MergeWithPreset(cfg)

	for name, got := range map[string]interface{}{
		"jwt": merged.JWT, "password": merged.Password,
		"mfa": merged.MFA, "security": merged.Security, "sessions": merged.Sessions,
	} {
		if got == nil {
			t.Errorf("the %s block was left nil, so its settings would all read as zero", name)
		}
	}
}

func TestMergeOfTheNestedSecurityBlocks(t *testing.T) {
	// Writing one of them must not cost the others: someone tuning brute-force
	// lockout should not lose replay protection by doing so.
	standard := GetPreset(PresetStandard)
	if standard.Security == nil || standard.Security.BruteForce == nil {
		t.Skip("the standard preset carries no brute-force settings to merge against")
	}

	cfg := &Config{
		Preset:   PresetStandard,
		Security: &SecurityConfig{BruteForce: &BruteForceConfig{MaxAttempts: 99}},
	}
	merged := MergeWithPreset(cfg)

	if merged.Security.BruteForce.MaxAttempts != 99 {
		t.Errorf("the written attempt limit was replaced: %d", merged.Security.BruteForce.MaxAttempts)
	}
	if standard.Security.ReplayProtection != nil && merged.Security.ReplayProtection == nil {
		t.Error("replay protection was lost by writing an unrelated security setting")
	}
	if standard.Security.DeviceBinding != nil && merged.Security.DeviceBinding == nil {
		t.Error("device binding was lost by writing an unrelated security setting")
	}
}

func TestMergeOfMFAKeepsWhatWasWritten(t *testing.T) {
	cfg := &Config{
		Preset: PresetStrict,
		MFA:    &MFAConfig{Enabled: true, Methods: []string{"totp"}},
	}
	merged := MergeWithPreset(cfg)

	if len(merged.MFA.Methods) != 1 || merged.MFA.Methods[0] != "totp" {
		t.Errorf("the written methods were replaced: %v", merged.MFA.Methods)
	}
	// Everything not written still comes from the preset.
	if merged.MFA.Required != GetPreset(PresetStrict).MFA.Required {
		t.Errorf("required = %q", merged.MFA.Required)
	}
	if merged.MFA.Recovery == nil || merged.MFA.TOTP == nil {
		t.Error("the recovery and TOTP settings were not filled in")
	}
}
