package auth

import (
	"context"
	"fmt"

	"github.com/matutetandil/mycel/v3/internal/transform"
)

// What happens around a sign-in, a registration or a password change.
//
// The hooks block was accepted by the parser's block schema and never read
// into the configuration: `Config.Hooks` was nil however much was written in
// it, and nothing would have looked at it anyway. So a service asking to be
// told about a suspicious sign-in was told nothing, silently, which is the
// worst way for a security feature to be absent.
//
// A hook names a flow. That is how the rest of the runtime does this — an
// aspect invokes a flow by name — and it keeps auth out of the business of
// sending email or writing to Slack: the flow already knows how.

// Hook event names, which arrive to the flow as auth.event.
const (
	HookBeforeLogin          = "before_login"
	HookAfterLogin           = "after_login"
	HookAfterRegister        = "after_register"
	HookOnFailedLogin        = "on_failed_login"
	HookOnSuspiciousActivity = "on_suspicious_activity"
	HookOnPasswordReset      = "on_password_reset"
	HookBeforePasswordChange = "before_password_change"
	HookAfterPasswordChange  = "after_password_change"
)

// FlowInvoker runs a flow by name. The runtime's flow registry is one.
type FlowInvoker interface {
	InvokeFlow(ctx context.Context, flowName string, input map[string]interface{}) (interface{}, error)
}

// WithFlowInvoker gives the manager a way to run the flows its hooks name.
func WithFlowInvoker(invoker FlowInvoker) ManagerOption {
	return func(m *Manager) {
		m.flows = invoker
	}
}

// hookFor returns the hook configured for an event, if any.
func (m *Manager) hookFor(event string) *HookConfig {
	if m.config.Hooks == nil {
		return nil
	}
	switch event {
	case HookBeforeLogin:
		return m.config.Hooks.BeforeLogin
	case HookAfterLogin:
		return m.config.Hooks.AfterLogin
	case HookAfterRegister:
		return m.config.Hooks.AfterRegister
	case HookOnFailedLogin:
		return m.config.Hooks.OnFailedLogin
	case HookOnSuspiciousActivity:
		return m.config.Hooks.OnSuspiciousActivity
	case HookOnPasswordReset:
		return m.config.Hooks.OnPasswordReset
	case HookBeforePasswordChange:
		return m.config.Hooks.BeforePasswordChange
	case HookAfterPasswordChange:
		return m.config.Hooks.AfterPasswordChange
	}
	return nil
}

// runHook invokes the flow an event is bound to.
//
// It returns an error only when the hook is configured to refuse the thing it
// is attached to. Everything else — no hook, a condition that did not hold, a
// flow that failed on an `ignore` hook — is reported and the caller carries on:
// an account must not become unusable because a notification could not be sent.
func (m *Manager) runHook(ctx context.Context, event string, data map[string]interface{}) error {
	hook := m.hookFor(event)
	if hook == nil || hook.Flow == "" {
		return nil
	}

	if m.flows == nil {
		// Configured and unrunnable. Worth saying once per occurrence rather
		// than never: the usual cause is a manager built outside the runtime.
		m.logger.Warn("an auth hook names a flow but nothing can run flows here",
			"event", event, "flow", hook.Flow)
		return nil
	}

	payload := map[string]interface{}{"event": event}
	for k, v := range data {
		payload[k] = v
	}

	if hook.Condition != "" {
		run, err := m.hookConditionHolds(ctx, hook, payload)
		if err != nil {
			m.logger.Warn("an auth hook condition could not be evaluated",
				"event", event, "condition", hook.Condition, "error", err)
			return nil
		}
		if !run {
			return nil
		}
	}

	// The flow sees the event under `auth`, next to the input it would get
	// from any other source.
	if _, err := m.flows.InvokeFlow(ctx, hook.Flow, map[string]interface{}{"auth": payload}); err != nil {
		m.logger.Warn("an auth hook failed", "event", event, "flow", hook.Flow, "error", err)
		if hook.OnError == "fail" {
			return fmt.Errorf("%s hook %q failed: %w", event, hook.Flow, err)
		}
	}
	return nil
}

// hookConditionHolds evaluates the hook's condition against the event.
func (m *Manager) hookConditionHolds(ctx context.Context, hook *HookConfig, payload map[string]interface{}) (bool, error) {
	if m.hookConditions == nil {
		transformer, err := transform.NewCELTransformer()
		if err != nil {
			return false, err
		}
		m.hookConditions = transformer
	}
	return m.hookConditions.EvaluateCondition(ctx, map[string]interface{}{"auth": payload}, hook.Condition)
}

// validateHooks reports a hooks block that could not do what it says.
//
// Checked when the manager is built rather than when somebody signs in: a
// misspelled flow name should stop a deployment, not surprise the first person
// through the door.
func validateHooks(cfg *Config) error {
	if cfg.Hooks == nil {
		return nil
	}
	for _, event := range []string{
		HookBeforeLogin, HookAfterLogin, HookAfterRegister, HookOnFailedLogin,
		HookOnSuspiciousActivity, HookOnPasswordReset, HookBeforePasswordChange, HookAfterPasswordChange,
	} {
		hook := hookIn(cfg.Hooks, event)
		if hook == nil {
			continue
		}
		if hook.Flow == "" {
			return fmt.Errorf("auth hook %s names no flow", event)
		}
		switch hook.OnError {
		case "", "ignore", "fail":
		default:
			return fmt.Errorf("auth hook %s: on_error = %q is not one of ignore or fail", event, hook.OnError)
		}
		// Refusing after the event has happened cannot undo it, so a hook that
		// says so is a misunderstanding worth naming.
		if hook.OnError == "fail" && !isBeforeHook(event) {
			return fmt.Errorf("auth hook %s: on_error = \"fail\" only means something on a before_ hook, "+
				"since %s runs once the change has already been made", event, event)
		}
	}
	return nil
}

func hookIn(hooks *HooksConfig, event string) *HookConfig {
	switch event {
	case HookBeforeLogin:
		return hooks.BeforeLogin
	case HookAfterLogin:
		return hooks.AfterLogin
	case HookAfterRegister:
		return hooks.AfterRegister
	case HookOnFailedLogin:
		return hooks.OnFailedLogin
	case HookOnSuspiciousActivity:
		return hooks.OnSuspiciousActivity
	case HookOnPasswordReset:
		return hooks.OnPasswordReset
	case HookBeforePasswordChange:
		return hooks.BeforePasswordChange
	case HookAfterPasswordChange:
		return hooks.AfterPasswordChange
	}
	return nil
}

func isBeforeHook(event string) bool {
	return event == HookBeforeLogin || event == HookBeforePasswordChange
}
