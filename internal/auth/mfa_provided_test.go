package auth

import (
	"strings"
	"testing"
)

// A second factor the runtime cannot provide has to be refused, not accepted.
//
// `mfa { methods = ["sms"] }` parsed, counted towards min_factors, and was
// never dispatched on — enrolment offers TOTP and WebAuthn whatever the list
// says. `mfa { sms { } }`, `mfa { email { } }` and `mfa { push { } }` parsed
// into config fields that nothing in the package reads. So a service could be
// configured for SMS two-factor, start cleanly, log "auth system initialized",
// and have no second factor at all — which is the worst shape this class of
// bug takes, because the configuration is the only thing anybody checks.
func TestAnMFAMethodThatIsNotProvidedIsRefused(t *testing.T) {
	for name, config := range map[string]*Config{
		"a method nothing dispatches on": {
			Secret: "a-secret-long-enough-for-hmac-signing",
			MFA:    &MFAConfig{Enabled: true, Methods: []string{"totp", "sms"}},
		},
		"an sms block": {
			Secret: "a-secret-long-enough-for-hmac-signing",
			MFA:    &MFAConfig{Enabled: true, SMS: &SMSConfig{}},
		},
		"an email block": {
			Secret: "a-secret-long-enough-for-hmac-signing",
			MFA:    &MFAConfig{Enabled: true, Email: &EmailMFAConfig{}},
		},
		"a push block": {
			Secret: "a-secret-long-enough-for-hmac-signing",
			MFA:    &MFAConfig{Enabled: true, Push: &PushConfig{}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewManager(config)
			if err == nil {
				t.Fatal("the service started with a second factor it cannot provide")
			}
			// The message has to say what can be had instead.
			for _, want := range []string{"totp", "webauthn"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the error does not name %q: %v", want, err)
				}
			}
		})
	}
}

// What is provided still works, and so does saying nothing.
func TestTheMethodsThatAreProvidedStart(t *testing.T) {
	for name, mfa := range map[string]*MFAConfig{
		"both of them":     {Enabled: true, Methods: []string{"totp", "webauthn"}},
		"one of them":      {Enabled: true, Methods: []string{"totp"}},
		"no list at all":   {Enabled: true},
		"no mfa block":     nil,
		"recovery is fine": {Enabled: true, Methods: []string{"totp"}, Recovery: &RecoveryConfig{}},
	} {
		t.Run(name, func(t *testing.T) {
			config := &Config{Secret: "a-secret-long-enough-for-hmac-signing", MFA: mfa}
			if _, err := NewManager(config); err != nil {
				t.Errorf("refused a configuration it can provide: %v", err)
			}
		})
	}
}
