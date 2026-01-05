package auth

import (
	"fmt"
	"time"
)

// Preset names
const (
	PresetStrict      = "strict"
	PresetStandard    = "standard"
	PresetRelaxed     = "relaxed"
	PresetDevelopment = "development"
)

// GetPreset returns the configuration for a given preset name
func GetPreset(name string) *Config {
	switch name {
	case PresetStrict:
		return strictPreset()
	case PresetStandard:
		return standardPreset()
	case PresetRelaxed:
		return relaxedPreset()
	case PresetDevelopment:
		return developmentPreset()
	default:
		return standardPreset()
	}
}

// strictPreset returns maximum security configuration
func strictPreset() *Config {
	return &Config{
		Preset: PresetStrict,
		JWT: &JWTConfig{
			Algorithm:       "RS256",
			AccessLifetime:  "15m",
			RefreshLifetime: "1d",
			Rotation:        true,
		},
		Password: &PasswordConfig{
			Required:       true,
			MinLength:      12,
			MaxLength:      128,
			RequireUpper:   true,
			RequireLower:   true,
			RequireNumber:  true,
			RequireSpecial: true,
			History:        5,
			MaxAge:         "90d",
			BreachCheck:    true,
			Algorithm:      "argon2id",
			Memory:         65536,
			Iterations:     3,
			Parallelism:    2,
			SaltLength:     16,
			KeyLength:      32,
		},
		MFA: &MFAConfig{
			Required:    "true",
			Methods:     []string{"totp", "webauthn"},
			GracePeriod: "7d",
			Recovery: &RecoveryConfig{
				Enabled:    true,
				CodeCount:  10,
				CodeLength: 8,
			},
			TOTP: &TOTPConfig{
				Digits:    6,
				Period:    30,
				Algorithm: "SHA1",
			},
		},
		Security: &SecurityConfig{
			BruteForce: &BruteForceConfig{
				Enabled:     true,
				MaxAttempts: 3,
				Window:      "15m",
				LockoutTime: "30m",
				TrackBy:     "ip+user",
				ProgressiveDelay: &ProgressiveDelayConfig{
					Enabled:    true,
					Initial:    "1s",
					Multiplier: 2,
					Max:        "30s",
				},
			},
			ImpossibleTravel: &ImpossibleTravelConfig{
				Enabled:     true,
				MaxSpeedKMH: 900,
				OnDetect:    "block",
			},
			DeviceBinding: &DeviceBindingConfig{
				Enabled:       true,
				TrustDuration: "30d",
				MaxDevices:    3,
				OnNewDevice:   "challenge",
			},
			ReplayProtection: &ReplayProtectionConfig{
				Enabled: true,
				Window:  "5m",
			},
		},
		Sessions: &SessionsConfig{
			MaxActive:        3,
			IdleTimeout:      "30m",
			AbsoluteTimeout:  "8h",
			AllowList:        true,
			AllowRevoke:      true,
			Track:            []string{"ip", "user_agent", "location", "device_id"},
			OnMaxReached:     "reject_new",
			ExtendOnActivity: true,
		},
	}
}

// standardPreset returns balanced security configuration
func standardPreset() *Config {
	return &Config{
		Preset: PresetStandard,
		JWT: &JWTConfig{
			Algorithm:       "HS256",
			AccessLifetime:  "1h",
			RefreshLifetime: "7d",
			Rotation:        true,
		},
		Password: &PasswordConfig{
			Required:       true,
			MinLength:      8,
			MaxLength:      128,
			RequireUpper:   true,
			RequireLower:   true,
			RequireNumber:  true,
			RequireSpecial: false,
			History:        3,
			BreachCheck:    false,
			Algorithm:      "argon2id",
			Memory:         65536,
			Iterations:     3,
			Parallelism:    2,
			SaltLength:     16,
			KeyLength:      32,
		},
		MFA: &MFAConfig{
			Required:    "optional",
			Methods:     []string{"totp", "webauthn"},
			GracePeriod: "14d",
			Recovery: &RecoveryConfig{
				Enabled:    true,
				CodeCount:  10,
				CodeLength: 8,
			},
			TOTP: &TOTPConfig{
				Digits:    6,
				Period:    30,
				Algorithm: "SHA1",
			},
		},
		Security: &SecurityConfig{
			BruteForce: &BruteForceConfig{
				Enabled:     true,
				MaxAttempts: 5,
				Window:      "15m",
				LockoutTime: "15m",
				TrackBy:     "ip+user",
			},
			DeviceBinding: &DeviceBindingConfig{
				Enabled:       true,
				TrustDuration: "30d",
				MaxDevices:    5,
				OnNewDevice:   "notify",
			},
			ReplayProtection: &ReplayProtectionConfig{
				Enabled: true,
				Window:  "5m",
			},
		},
		Sessions: &SessionsConfig{
			MaxActive:        5,
			IdleTimeout:      "1h",
			AbsoluteTimeout:  "24h",
			AllowList:        true,
			AllowRevoke:      true,
			Track:            []string{"ip", "user_agent"},
			OnMaxReached:     "revoke_oldest",
			ExtendOnActivity: true,
		},
	}
}

// relaxedPreset returns minimal security configuration
func relaxedPreset() *Config {
	return &Config{
		Preset: PresetRelaxed,
		JWT: &JWTConfig{
			Algorithm:       "HS256",
			AccessLifetime:  "24h",
			RefreshLifetime: "30d",
			Rotation:        false,
		},
		Password: &PasswordConfig{
			Required:       true,
			MinLength:      6,
			MaxLength:      128,
			RequireUpper:   false,
			RequireLower:   false,
			RequireNumber:  false,
			RequireSpecial: false,
			History:        0,
			BreachCheck:    false,
			Algorithm:      "argon2id",
			Memory:         65536,
			Iterations:     3,
			Parallelism:    2,
			SaltLength:     16,
			KeyLength:      32,
		},
		MFA: &MFAConfig{
			Required: "false",
			Methods:  []string{"totp"},
		},
		Security: &SecurityConfig{
			BruteForce: &BruteForceConfig{
				Enabled:     true,
				MaxAttempts: 10,
				Window:      "15m",
				LockoutTime: "5m",
				TrackBy:     "ip",
			},
		},
		Sessions: &SessionsConfig{
			MaxActive:        10,
			IdleTimeout:      "24h",
			AbsoluteTimeout:  "7d",
			AllowList:        true,
			AllowRevoke:      true,
			OnMaxReached:     "revoke_oldest",
			ExtendOnActivity: true,
		},
	}
}

// developmentPreset returns configuration for local development
func developmentPreset() *Config {
	return &Config{
		Preset: PresetDevelopment,
		JWT: &JWTConfig{
			Algorithm:       "HS256",
			AccessLifetime:  "7d",
			RefreshLifetime: "30d",
			Rotation:        false,
		},
		Password: &PasswordConfig{
			Required:       true,
			MinLength:      1,
			MaxLength:      128,
			RequireUpper:   false,
			RequireLower:   false,
			RequireNumber:  false,
			RequireSpecial: false,
			History:        0,
			BreachCheck:    false,
			Algorithm:      "argon2id",
			Memory:         65536,
			Iterations:     1,
			Parallelism:    1,
			SaltLength:     16,
			KeyLength:      32,
		},
		MFA: &MFAConfig{
			Required: "false",
			Methods:  []string{},
		},
		Security: &SecurityConfig{
			BruteForce: &BruteForceConfig{
				Enabled: false,
			},
		},
		Sessions: &SessionsConfig{
			MaxActive:        100,
			IdleTimeout:      "30d",
			AbsoluteTimeout:  "90d",
			AllowList:        true,
			AllowRevoke:      true,
			OnMaxReached:     "revoke_oldest",
			ExtendOnActivity: true,
		},
	}
}

// MergeWithPreset merges user config with preset defaults
func MergeWithPreset(cfg *Config) *Config {
	preset := GetPreset(cfg.Preset)

	// Merge JWT
	if cfg.JWT == nil {
		cfg.JWT = preset.JWT
	} else {
		mergeJWT(cfg.JWT, preset.JWT)
	}

	// Merge Password
	if cfg.Password == nil {
		cfg.Password = preset.Password
	} else {
		mergePassword(cfg.Password, preset.Password)
	}

	// Merge MFA
	if cfg.MFA == nil {
		cfg.MFA = preset.MFA
	} else {
		mergeMFA(cfg.MFA, preset.MFA)
	}

	// Merge Security
	if cfg.Security == nil {
		cfg.Security = preset.Security
	} else {
		mergeSecurity(cfg.Security, preset.Security)
	}

	// Merge Sessions
	if cfg.Sessions == nil {
		cfg.Sessions = preset.Sessions
	} else {
		mergeSessions(cfg.Sessions, preset.Sessions)
	}

	return cfg
}

func mergeJWT(cfg, preset *JWTConfig) {
	if cfg.Algorithm == "" {
		cfg.Algorithm = preset.Algorithm
	}
	if cfg.AccessLifetime == "" {
		cfg.AccessLifetime = preset.AccessLifetime
	}
	if cfg.RefreshLifetime == "" {
		cfg.RefreshLifetime = preset.RefreshLifetime
	}
}

func mergePassword(cfg, preset *PasswordConfig) {
	if cfg.MinLength == 0 {
		cfg.MinLength = preset.MinLength
	}
	if cfg.MaxLength == 0 {
		cfg.MaxLength = preset.MaxLength
	}
	if cfg.Algorithm == "" {
		cfg.Algorithm = preset.Algorithm
	}
	if cfg.Memory == 0 {
		cfg.Memory = preset.Memory
	}
	if cfg.Iterations == 0 {
		cfg.Iterations = preset.Iterations
	}
	if cfg.Parallelism == 0 {
		cfg.Parallelism = preset.Parallelism
	}
	if cfg.SaltLength == 0 {
		cfg.SaltLength = preset.SaltLength
	}
	if cfg.KeyLength == 0 {
		cfg.KeyLength = preset.KeyLength
	}
}

func mergeMFA(cfg, preset *MFAConfig) {
	if cfg.Required == "" {
		cfg.Required = preset.Required
	}
	if len(cfg.Methods) == 0 {
		cfg.Methods = preset.Methods
	}
	if cfg.Recovery == nil {
		cfg.Recovery = preset.Recovery
	}
	if cfg.TOTP == nil {
		cfg.TOTP = preset.TOTP
	}
}

func mergeSecurity(cfg, preset *SecurityConfig) {
	if cfg.BruteForce == nil {
		cfg.BruteForce = preset.BruteForce
	}
	if cfg.DeviceBinding == nil {
		cfg.DeviceBinding = preset.DeviceBinding
	}
	if cfg.ReplayProtection == nil {
		cfg.ReplayProtection = preset.ReplayProtection
	}
	if cfg.ImpossibleTravel == nil {
		cfg.ImpossibleTravel = preset.ImpossibleTravel
	}
}

func mergeSessions(cfg, preset *SessionsConfig) {
	if cfg.MaxActive == 0 {
		cfg.MaxActive = preset.MaxActive
	}
	if cfg.IdleTimeout == "" {
		cfg.IdleTimeout = preset.IdleTimeout
	}
	if cfg.AbsoluteTimeout == "" {
		cfg.AbsoluteTimeout = preset.AbsoluteTimeout
	}
	if cfg.OnMaxReached == "" {
		cfg.OnMaxReached = preset.OnMaxReached
	}
}

// ParseDuration parses a duration string like "15m", "1h", "7d"
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	// Handle day suffix
	if len(s) > 1 && s[len(s)-1] == 'd' {
		daysStr := s[:len(s)-1]
		var days int
		if _, err := fmt.Sscanf(daysStr, "%d", &days); err != nil {
			return 0, fmt.Errorf("invalid day format: %s", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}

	return time.ParseDuration(s)
}
