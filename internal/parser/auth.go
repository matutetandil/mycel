package parser

import (
	"fmt"
	"strconv"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/matutetandil/mycel/v3/internal/auth"
	"github.com/zclconf/go-cty/cty"
)

// parseAuthBlock parses an auth configuration block
func (p *HCLParser) parseAuthBlock(block *hcl.Block) (*auth.Config, error) {
	config := &auth.Config{}

	content, diags := block.Body.Content(authSchema)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing auth block: %s", diags.Error())
	}

	if attr, exists := content.Attributes["base_url"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("base_url", val)
			if err != nil {
				return nil, err
			}
			config.BaseURL = s
		}
	}

	// Parse simple attributes
	if attr, exists := content.Attributes["preset"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("preset", val)
			if err != nil {
				return nil, err
			}
			config.Preset = s
		}
	}

	if attr, exists := content.Attributes["secret"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("secret", val)
			if err != nil {
				return nil, err
			}
			config.Secret = s
		}
	}

	if attr, exists := content.Attributes["storage"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("storage", val)
			if err != nil {
				return nil, err
			}
			config.StorageConnector = s
		}
	}

	// Parse nested blocks
	for _, nested := range content.Blocks {
		switch nested.Type {
		case "storage":
			storage, err := p.parseAuthStorageBlock(nested)
			if err != nil {
				return nil, err
			}
			config.Storage = storage

		case "users":
			users, err := p.parseAuthUsersBlock(nested)
			if err != nil {
				return nil, err
			}
			config.Users = users

		case "jwt":
			jwt, err := p.parseAuthJWTBlock(nested)
			if err != nil {
				return nil, err
			}
			config.JWT = jwt

		case "password":
			password, err := p.parseAuthPasswordBlock(nested)
			if err != nil {
				return nil, err
			}
			config.Password = password

		case "mfa":
			mfa, err := p.parseAuthMFABlock(nested)
			if err != nil {
				return nil, err
			}
			config.MFA = mfa

		case "security":
			security, err := p.parseAuthSecurityBlock(nested)
			if err != nil {
				return nil, err
			}
			config.Security = security

		case "sessions":
			sessions, err := p.parseAuthSessionsBlock(nested)
			if err != nil {
				return nil, err
			}
			config.Sessions = sessions

		case "social":
			social, err := p.parseAuthSocialBlock(nested)
			if err != nil {
				return nil, err
			}
			config.Social = social

		case "sso":
			sso, err := p.parseAuthSSOBlock(nested)
			if err != nil {
				return nil, err
			}
			config.SSO = sso

		case "provider":
			provider, err := p.parseAuthProviderBlock(nested)
			if err != nil {
				return nil, err
			}
			config.Providers = append(config.Providers, provider)

		case "account_linking":
			linking, err := p.parseAuthAccountLinkingBlock(nested)
			if err != nil {
				return nil, err
			}
			config.AccountLinking = linking

		case "endpoints":
			endpoints, err := p.parseAuthEndpointsBlock(nested)
			if err != nil {
				return nil, err
			}
			config.Endpoints = endpoints

		case "hooks":
			hooks, err := p.parseAuthHooksBlock(nested)
			if err != nil {
				return nil, err
			}
			config.Hooks = hooks

		case "audit":
			audit, err := p.parseAuthAuditBlock(nested)
			if err != nil {
				return nil, err
			}
			config.Audit = audit
		}
	}

	return config, nil
}

func (p *HCLParser) parseAuthStorageBlock(block *hcl.Block) (*auth.StorageConfig, error) {
	config := &auth.StorageConfig{}
	diags := gohcl.DecodeBody(block.Body, p.evalCtx, config)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing auth storage block: %s", diags.Error())
	}
	return config, nil
}

func (p *HCLParser) parseAuthUsersBlock(block *hcl.Block) (*auth.UsersConfig, error) {
	config := &auth.UsersConfig{}

	content, diags := block.Body.Content(&hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "connector"},
			{Name: "table"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "fields"},
		},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing auth users block: %s", diags.Error())
	}

	if attr, exists := content.Attributes["connector"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("connector", val)
			if err != nil {
				return nil, err
			}
			config.Connector = s
		}
	}

	if attr, exists := content.Attributes["table"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("table", val)
			if err != nil {
				return nil, err
			}
			config.Table = s
		}
	}

	for _, nested := range content.Blocks {
		if nested.Type == "fields" {
			fields := &auth.FieldsConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, fields)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing fields block: %s", diags.Error())
			}
			config.Fields = fields
		}
	}

	return config, nil
}

func (p *HCLParser) parseAuthJWTBlock(block *hcl.Block) (*auth.JWTConfig, error) {
	config := &auth.JWTConfig{}
	diags := gohcl.DecodeBody(block.Body, p.evalCtx, config)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing auth jwt block: %s", diags.Error())
	}
	return config, nil
}

func (p *HCLParser) parseAuthPasswordBlock(block *hcl.Block) (*auth.PasswordConfig, error) {
	config := &auth.PasswordConfig{}
	diags := gohcl.DecodeBody(block.Body, p.evalCtx, config)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing auth password block: %s", diags.Error())
	}
	return config, nil
}

func (p *HCLParser) parseAuthMFABlock(block *hcl.Block) (*auth.MFAConfig, error) {
	// The presence of an `mfa` block opts MFA in by default; an explicit
	// `enabled = false` can still disable it without removing the block.
	config := &auth.MFAConfig{Enabled: true}

	content, diags := block.Body.Content(&hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "enabled"},
			{Name: "required"},
			{Name: "methods"},
			{Name: "require_for"},
			{Name: "require_multiple"},
			{Name: "min_factors"},
			{Name: "grace_period"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "recovery"},
			{Type: "totp"},
			{Type: "webauthn"},
			{Type: "sms"},
			{Type: "email"},
			{Type: "push"},
		},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing auth mfa block: %s", diags.Error())
	}

	// Parse attributes
	if attr, exists := content.Attributes["enabled"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			config.Enabled = boolOrFalse(val)
		}
	}

	if attr, exists := content.Attributes["required"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("required", val)
			if err != nil {
				return nil, err
			}
			config.Required = s
		}
	}

	if attr, exists := content.Attributes["methods"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() && val.CanIterateElements() {
			iter := val.ElementIterator()
			for iter.Next() {
				_, v := iter.Element()
				config.Methods = append(config.Methods, stringOrEmpty(v))
			}
		}
	}

	if attr, exists := content.Attributes["require_for"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() && val.CanIterateElements() {
			iter := val.ElementIterator()
			for iter.Next() {
				_, v := iter.Element()
				config.RequireFor = append(config.RequireFor, stringOrEmpty(v))
			}
		}
	}

	if attr, exists := content.Attributes["require_multiple"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			config.RequireMultiple = boolOrFalse(val)
		}
	}

	if attr, exists := content.Attributes["min_factors"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			f, _ := coerceInt(val)
			config.MinFactors = f
		}
	}

	if attr, exists := content.Attributes["grace_period"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("grace_period", val)
			if err != nil {
				return nil, err
			}
			config.GracePeriod = s
		}
	}

	// Parse nested blocks
	for _, nested := range content.Blocks {
		switch nested.Type {
		case "recovery":
			recovery := &auth.RecoveryConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, recovery)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing recovery block: %s", diags.Error())
			}
			config.Recovery = recovery

		case "totp":
			totp := &auth.TOTPConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, totp)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing totp block: %s", diags.Error())
			}
			config.TOTP = totp

		case "webauthn":
			webauthn, err := p.parseWebAuthnBlock(nested)
			if err != nil {
				return nil, err
			}
			config.WebAuthn = webauthn

		case "sms":
			sms, err := p.parseAuthSMSBlock(nested)
			if err != nil {
				return nil, err
			}
			config.SMS = sms

		case "email":
			email := &auth.EmailMFAConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, email)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing email block: %s", diags.Error())
			}
			config.Email = email

		case "push":
			push, err := p.parseAuthPushBlock(nested)
			if err != nil {
				return nil, err
			}
			config.Push = push
		}
	}

	return config, nil
}

func (p *HCLParser) parseWebAuthnBlock(block *hcl.Block) (*auth.WebAuthnConfig, error) {
	config := &auth.WebAuthnConfig{}

	content, diags := block.Body.Content(&hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "rp_name"},
			{Name: "rp_display_name"},
			{Name: "rp_id"},
			{Name: "origins"},
			{Name: "rp_origins"},
			{Name: "timeout"},
			{Name: "authenticator_attachment"},
			{Name: "user_verification"},
			{Name: "resident_key"},
			{Name: "max_credentials"},
			{Name: "attestation"},
			{Name: "allowed_aaguids"},
		},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing webauthn block: %s", diags.Error())
	}

	if attr, exists := content.Attributes["rp_name"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("rp_name", val)
			if err != nil {
				return nil, err
			}
			config.RPName = s
		}
	}

	if attr, exists := content.Attributes["rp_id"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("rp_id", val)
			if err != nil {
				return nil, err
			}
			config.RPID = s
		}
	}

	if attr, exists := content.Attributes["origins"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() && val.CanIterateElements() {
			iter := val.ElementIterator()
			for iter.Next() {
				_, v := iter.Element()
				config.Origins = append(config.Origins, stringOrEmpty(v))
			}
		}
	}

	if attr, exists := content.Attributes["authenticator_attachment"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("authenticator_attachment", val)
			if err != nil {
				return nil, err
			}
			config.AuthenticatorAttachment = s
		}
	}

	if attr, exists := content.Attributes["user_verification"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("user_verification", val)
			if err != nil {
				return nil, err
			}
			config.UserVerification = s
		}
	}

	if attr, exists := content.Attributes["resident_key"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("resident_key", val)
			if err != nil {
				return nil, err
			}
			config.ResidentKey = s
		}
	}

	if attr, exists := content.Attributes["max_credentials"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			f, _ := coerceInt(val)
			config.MaxCredentials = f
		}
	}

	if attr, exists := content.Attributes["attestation"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("attestation", val)
			if err != nil {
				return nil, err
			}
			config.Attestation = s
		}
	}

	if attr, exists := content.Attributes["allowed_aaguids"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() && val.CanIterateElements() {
			iter := val.ElementIterator()
			for iter.Next() {
				_, v := iter.Element()
				config.AllowedAAGUIDs = append(config.AllowedAAGUIDs, stringOrEmpty(v))
			}
		}
	}

	// Three settings the type has always carried and the parser never
	// accepted, so writing any of them failed the document outright:
	//
	//   rp_display_name — what the browser shows in the prompt
	//   rp_origins      — the alias the service prefers over origins
	//   timeout         — how long a ceremony is given, in milliseconds
	if attr, exists := content.Attributes["rp_display_name"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("rp_display_name", val)
			if err != nil {
				return nil, err
			}
			config.RPDisplayName = s
		}
	}

	if attr, exists := content.Attributes["rp_origins"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() && val.CanIterateElements() {
			iter := val.ElementIterator()
			for iter.Next() {
				_, v := iter.Element()
				config.RPOrigins = append(config.RPOrigins, stringOrEmpty(v))
			}
		}
	}

	if attr, exists := content.Attributes["timeout"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			n, _ := coerceInt(val)
			config.Timeout = n
		}
	}

	return config, nil
}

func (p *HCLParser) parseAuthSMSBlock(block *hcl.Block) (*auth.SMSConfig, error) {
	config := &auth.SMSConfig{}

	content, diags := block.Body.Content(&hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "provider"},
			{Name: "code_length"},
			{Name: "expiry"},
			{Name: "rate_limit"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "twilio"},
		},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing sms block: %s", diags.Error())
	}

	if attr, exists := content.Attributes["provider"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("provider", val)
			if err != nil {
				return nil, err
			}
			config.Provider = s
		}
	}

	if attr, exists := content.Attributes["code_length"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			f, _ := coerceInt(val)
			config.CodeLength = f
		}
	}

	if attr, exists := content.Attributes["expiry"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("expiry", val)
			if err != nil {
				return nil, err
			}
			config.Expiry = s
		}
	}

	if attr, exists := content.Attributes["rate_limit"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("rate_limit", val)
			if err != nil {
				return nil, err
			}
			config.RateLimit = s
		}
	}

	for _, nested := range content.Blocks {
		if nested.Type == "twilio" {
			twilio := &auth.TwilioConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, twilio)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing twilio block: %s", diags.Error())
			}
			config.Twilio = twilio
		}
	}

	return config, nil
}

func (p *HCLParser) parseAuthPushBlock(block *hcl.Block) (*auth.PushConfig, error) {
	config := &auth.PushConfig{}

	content, diags := block.Body.Content(&hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "provider"},
			{Name: "expiry"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "firebase"},
		},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing push block: %s", diags.Error())
	}

	if attr, exists := content.Attributes["provider"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("provider", val)
			if err != nil {
				return nil, err
			}
			config.Provider = s
		}
	}

	if attr, exists := content.Attributes["expiry"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("expiry", val)
			if err != nil {
				return nil, err
			}
			config.Expiry = s
		}
	}

	for _, nested := range content.Blocks {
		if nested.Type == "firebase" {
			firebase := &auth.FirebaseConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, firebase)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing firebase block: %s", diags.Error())
			}
			config.Firebase = firebase
		}
	}

	return config, nil
}

func (p *HCLParser) parseAuthSecurityBlock(block *hcl.Block) (*auth.SecurityConfig, error) {
	config := &auth.SecurityConfig{}

	content, diags := block.Body.Content(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "brute_force"},
			{Type: "impossible_travel"},
			{Type: "device_binding"},
			{Type: "replay_protection"},
			{Type: "ip_rules"},
			{Type: "rate_limit"},
		},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing security block: %s", diags.Error())
	}

	for _, nested := range content.Blocks {
		switch nested.Type {
		case "brute_force":
			bf, err := p.parseAuthBruteForceBlock(nested)
			if err != nil {
				return nil, err
			}
			config.BruteForce = bf

		case "impossible_travel":
			it := &auth.ImpossibleTravelConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, it)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing impossible_travel block: %s", diags.Error())
			}
			config.ImpossibleTravel = it

		case "device_binding":
			db := &auth.DeviceBindingConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, db)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing device_binding block: %s", diags.Error())
			}
			config.DeviceBinding = db

		case "replay_protection":
			rp := &auth.ReplayProtectionConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, rp)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing replay_protection block: %s", diags.Error())
			}
			config.ReplayProtection = rp

		case "ip_rules":
			ir := &auth.IPRulesConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, ir)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing ip_rules block: %s", diags.Error())
			}
			config.IPRules = ir

		case "rate_limit":
			rl := &auth.RateLimitConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, rl)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing rate_limit block: %s", diags.Error())
			}
			config.RateLimit = rl
		}
	}

	return config, nil
}

func (p *HCLParser) parseAuthBruteForceBlock(block *hcl.Block) (*auth.BruteForceConfig, error) {
	config := &auth.BruteForceConfig{}

	content, diags := block.Body.Content(&hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "enabled"},
			{Name: "max_attempts"},
			{Name: "window"},
			{Name: "lockout_time"},
			{Name: "track_by"},
			{Name: "fail_delay"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "progressive_delay"},
		},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing brute_force block: %s", diags.Error())
	}

	if attr, exists := content.Attributes["enabled"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			config.Enabled = boolOrFalse(val)
		}
	}

	if attr, exists := content.Attributes["max_attempts"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			f, _ := coerceInt(val)
			config.MaxAttempts = f
		}
	}

	if attr, exists := content.Attributes["window"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("window", val)
			if err != nil {
				return nil, err
			}
			config.Window = s
		}
	}

	if attr, exists := content.Attributes["lockout_time"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("lockout_time", val)
			if err != nil {
				return nil, err
			}
			config.LockoutTime = s
		}
	}

	if attr, exists := content.Attributes["fail_delay"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("fail_delay", val)
			if err != nil {
				return nil, err
			}
			config.FailDelay = s
		}
	}

	if attr, exists := content.Attributes["track_by"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("track_by", val)
			if err != nil {
				return nil, err
			}
			config.TrackBy = s
		}
	}

	for _, nested := range content.Blocks {
		if nested.Type == "progressive_delay" {
			pd := &auth.ProgressiveDelayConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, pd)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing progressive_delay block: %s", diags.Error())
			}
			config.ProgressiveDelay = pd
		}
	}

	return config, nil
}

func (p *HCLParser) parseAuthSessionsBlock(block *hcl.Block) (*auth.SessionsConfig, error) {
	// Listing and revoking are on unless somebody writes them off.
	//
	// They are booleans, and a boolean nobody wrote is false — so decoding
	// straight into the struct would close two endpoints that are open today,
	// and turn a sliding session into a fixed one, for every deployment with a
	// sessions block that never mentioned them.
	// Set first and decoded over, which is how mfa.enabled is handled: the
	// block being there is not a decision, writing the word is.
	config := &auth.SessionsConfig{AllowList: true, AllowRevoke: true, ExtendOnActivity: true}
	diags := gohcl.DecodeBody(block.Body, p.evalCtx, config)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing sessions block: %s", diags.Error())
	}
	return config, nil
}

func (p *HCLParser) parseAuthSocialBlock(block *hcl.Block) (*auth.SocialConfig, error) {
	config := &auth.SocialConfig{}

	content, diags := block.Body.Content(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "google"},
			{Type: "github"},
			{Type: "apple"},
		},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing social block: %s", diags.Error())
	}

	for _, nested := range content.Blocks {
		switch nested.Type {
		case "google":
			google := &auth.OAuthProviderConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, google)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing google block: %s", diags.Error())
			}
			config.Google = google

		case "github":
			github := &auth.OAuthProviderConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, github)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing github block: %s", diags.Error())
			}
			config.GitHub = github

		case "apple":
			apple := &auth.AppleConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, apple)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing apple block: %s", diags.Error())
			}
			config.Apple = apple
		}
	}

	return config, nil
}

func (p *HCLParser) parseAuthSSOBlock(block *hcl.Block) (*auth.SSOConfig, error) {
	config := &auth.SSOConfig{}

	content, diags := block.Body.Content(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "oidc", LabelNames: []string{"name"}},
			{Type: "saml", LabelNames: []string{"name"}},
			{Type: "linking"},
		},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing sso block: %s", diags.Error())
	}

	for _, nested := range content.Blocks {
		switch nested.Type {
		case "oidc":
			oidc := &auth.OIDCConfig{}
			if len(nested.Labels) > 0 {
				oidc.Name = nested.Labels[0]
			}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, oidc)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing oidc block: %s", diags.Error())
			}
			config.OIDC = append(config.OIDC, oidc)

		case "linking":
			linking := &auth.AccountLinkingConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, linking)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing linking block: %s", diags.Error())
			}
			config.Linking = linking

		case "saml":
			saml := &auth.SAMLConfig{}
			if len(nested.Labels) > 0 {
				saml.Name = nested.Labels[0]
			}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, saml)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing saml block: %s", diags.Error())
			}
			config.SAML = append(config.SAML, saml)
		}
	}

	return config, nil
}

func (p *HCLParser) parseAuthProviderBlock(block *hcl.Block) (*auth.ProviderConfig, error) {
	config := &auth.ProviderConfig{}

	if len(block.Labels) > 0 {
		config.Name = block.Labels[0]
	}

	content, diags := block.Body.Content(&hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "type"},
			{Name: "validate"},
			{Name: "request"},
			{Name: "sync_to"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "response"},
		},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing provider block: %s", diags.Error())
	}

	if attr, exists := content.Attributes["type"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("type", val)
			if err != nil {
				return nil, err
			}
			config.Type = s
		}
	}

	if attr, exists := content.Attributes["validate"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("validate", val)
			if err != nil {
				return nil, err
			}
			config.Validate = s
		}
	}

	if attr, exists := content.Attributes["request"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() && val.Type().IsObjectType() {
			config.Request = make(map[string]string)
			for it := val.ElementIterator(); it.Next(); {
				k, v := it.Element()
				config.Request[stringOrEmpty(k)] = stringOrEmpty(v)
			}
		}
	}

	if attr, exists := content.Attributes["sync_to"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("sync_to", val)
			if err != nil {
				return nil, err
			}
			config.SyncTo = s
		}
	}

	for _, nested := range content.Blocks {
		if nested.Type == "response" {
			response := &auth.ProviderResponseConfig{}
			diags := gohcl.DecodeBody(nested.Body, p.evalCtx, response)
			if diags.HasErrors() {
				return nil, fmt.Errorf("parsing response block: %s", diags.Error())
			}
			config.Response = response
		}
	}

	return config, nil
}

func (p *HCLParser) parseAuthAccountLinkingBlock(block *hcl.Block) (*auth.AccountLinkingConfig, error) {
	config := &auth.AccountLinkingConfig{}
	diags := gohcl.DecodeBody(block.Body, p.evalCtx, config)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing account_linking block: %s", diags.Error())
	}
	return config, nil
}

func (p *HCLParser) parseAuthEndpointsBlock(block *hcl.Block) (*auth.EndpointsConfig, error) {
	// Start from the defaults, so that naming one endpoint does not silence the
	// rest. Writing the block at all used to leave every endpoint nil, and a
	// nil endpoint is a disabled one: `endpoints { prefix = "/auth" }` — the
	// form the documentation shows for moving the prefix, and the only thing
	// the auth example wrote — turned off login, register, refresh, me and the
	// other thirteen. Overriding one is still an override, and turning one off
	// is still `enabled = false` inside its own block.
	config := auth.DefaultEndpointsConfig()

	content, diags := block.Body.Content(&hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "prefix"},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "login"},
			{Type: "logout"},
			{Type: "register"},
			{Type: "refresh"},
			{Type: "me"},
			{Type: "password_forgot"},
			{Type: "password_reset"},
			{Type: "password_change"},
			{Type: "sessions_list"},
			{Type: "sessions_revoke"},
			{Type: "mfa_setup"},
			{Type: "mfa_verify"},
			{Type: "mfa_disable"},
			{Type: "mfa_recovery"},
			{Type: "social_callback"},
			{Type: "sso_callback"},
		},
	})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing endpoints block: %s", diags.Error())
	}

	if attr, exists := content.Attributes["prefix"]; exists {
		val, diags := attr.Expr.Value(p.evalCtx)
		if !diags.HasErrors() {
			s, err := stringValue("prefix", val)
			if err != nil {
				return nil, err
			}
			config.Prefix = s
		}
	}

	for _, nested := range content.Blocks {
		endpoint := &auth.EndpointConfig{Enabled: true}
		diags := gohcl.DecodeBody(nested.Body, p.evalCtx, endpoint)
		if diags.HasErrors() {
			return nil, fmt.Errorf("parsing %s endpoint block: %s", nested.Type, diags.Error())
		}

		switch nested.Type {
		case "login":
			config.Login = endpoint
		case "logout":
			config.Logout = endpoint
		case "register":
			config.Register = endpoint
		case "refresh":
			config.Refresh = endpoint
		case "me":
			config.Me = endpoint
		case "password_forgot":
			config.PasswordForgot = endpoint
		case "password_reset":
			config.PasswordReset = endpoint
		case "password_change":
			config.PasswordChange = endpoint
		case "sessions_list":
			config.SessionsList = endpoint
		case "sessions_revoke":
			config.SessionsRevoke = endpoint
		case "mfa_setup":
			config.MFASetup = endpoint
		case "mfa_verify":
			config.MFAVerify = endpoint
		case "mfa_disable":
			config.MFADisable = endpoint
		case "mfa_recovery":
			config.MFARecovery = endpoint
		case "social_callback":
			config.SocialCallback = endpoint
		case "sso_callback":
			config.SSOCallback = endpoint
		}
	}

	return config, nil
}

func (p *HCLParser) parseAuthAuditBlock(block *hcl.Block) (*auth.AuditConfig, error) {
	config := &auth.AuditConfig{}
	diags := gohcl.DecodeBody(block.Body, p.evalCtx, config)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing audit block: %s", diags.Error())
	}
	return config, nil
}

// parseAuthHooksBlock reads the flows an account's events are bound to.
//
// The block was listed in the schema below and never read, so `Config.Hooks`
// was nil however much was written in it: a service asking to be told about a
// suspicious sign-in was told nothing, and nothing said why.
func (p *HCLParser) parseAuthHooksBlock(block *hcl.Block) (*auth.HooksConfig, error) {
	config := &auth.HooksConfig{}
	diags := gohcl.DecodeBody(block.Body, p.evalCtx, config)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing hooks block: %s", diags.Error())
	}
	return config, nil
}

// authSchema defines the schema for auth block
var authSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "preset"},
		{Name: "base_url"},
		{Name: "secret"},
		{Name: "storage"},
	},
	Blocks: []hcl.BlockHeaderSchema{
		{Type: "storage"},
		{Type: "users"},
		{Type: "jwt"},
		{Type: "password"},
		{Type: "mfa"},
		{Type: "security"},
		{Type: "sessions"},
		{Type: "social"},
		{Type: "sso"},
		{Type: "provider", LabelNames: []string{"name"}},
		{Type: "account_linking"},
		{Type: "endpoints"},
		{Type: "hooks"},
		{Type: "audit"},
	},
}

// stringValue reads an attribute that is stored as text, accepting the values a
// person naturally writes for one.
//
// Every one of these used to be a bare cty AsString, which panics on anything
// else: `mfa { required = false }` — a boolean where the field happens to hold
// "true" or "false" — took the whole binary down with "panic: not a string",
// during validation rather than at run time.
func stringValue(name string, val cty.Value) (string, error) {
	if val.IsNull() {
		return "", nil
	}
	switch val.Type() {
	case cty.String:
		return val.AsString(), nil
	case cty.Bool:
		if boolOrFalse(val) {
			return "true", nil
		}
		return "false", nil
	case cty.Number:
		bf := val.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return strconv.FormatInt(i, 10), nil
		}
		f, _ := bf.Float64()
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	}
	return "", fmt.Errorf("attribute %q must be a string, and %s cannot be read as one",
		name, val.Type().FriendlyName())
}

// stringOrEmpty is stringValue for a list element, where the position rather
// than a name identifies the value.
func stringOrEmpty(val cty.Value) string {
	s, _ := stringValue("", val)
	return s
}
