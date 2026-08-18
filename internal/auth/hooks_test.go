package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// The hooks block was accepted by the parser's block schema and never read, so
// Config.Hooks was nil however much was written in it — and nothing read it
// either. A service asking to be told about a failed sign-in was told nothing.

// recordingFlows remembers what was invoked, and can be told to fail.
type recordingFlows struct {
	mu      sync.Mutex
	calls   []invocation
	failing map[string]bool
}

type invocation struct {
	flow  string
	input map[string]interface{}
}

func newRecordingFlows() *recordingFlows {
	return &recordingFlows{failing: map[string]bool{}}
}

func (r *recordingFlows) InvokeFlow(ctx context.Context, flow string, input map[string]interface{}) (interface{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, invocation{flow: flow, input: input})
	if r.failing[flow] {
		return nil, errors.New("the flow said no")
	}
	return nil, nil
}

func (r *recordingFlows) invoked(flow string) *invocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.calls {
		if r.calls[i].flow == flow {
			return &r.calls[i]
		}
	}
	return nil
}

func (r *recordingFlows) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func managerWithHooks(t *testing.T, hooks *HooksConfig) (*Manager, *recordingFlows) {
	t.Helper()

	flows := newRecordingFlows()
	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Password: &PasswordConfig{MinLength: 8},
		Hooks:    hooks,
	}, WithFlowInvoker(flows))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager, flows
}

func TestSigningInRunsTheFlowTheHookNames(t *testing.T) {
	manager, flows := managerWithHooks(t, &HooksConfig{
		AfterLogin: &HookConfig{Flow: "record_sign_in"},
	})
	registered(t, manager, "someone@example.test", "a-good-password")

	if _, _, err := manager.Login(context.Background(), &LoginRequest{
		Email: "someone@example.test", Password: "a-good-password",
	}, "203.0.113.10", "Mozilla/5.0"); err != nil {
		t.Fatalf("Login: %v", err)
	}

	call := flows.invoked("record_sign_in")
	if call == nil {
		t.Fatal("the after_login hook ran nothing")
	}
	// What the flow can see. Under auth, next to the input any other source
	// would give it.
	event, _ := call.input["auth"].(map[string]interface{})
	if event["event"] != "after_login" {
		t.Errorf("event = %v", event["event"])
	}
	if event["email"] != "someone@example.test" {
		t.Errorf("email = %v", event["email"])
	}
	if event["ip"] != "203.0.113.10" {
		t.Errorf("ip = %v", event["ip"])
	}
}

func TestAFailedSignInIsAnEventSomebodyCanWatch(t *testing.T) {
	manager, flows := managerWithHooks(t, &HooksConfig{
		OnFailedLogin: &HookConfig{Flow: "alert_security"},
	})
	registered(t, manager, "someone@example.test", "a-good-password")

	_, _, err := manager.Login(context.Background(), &LoginRequest{
		Email: "someone@example.test", Password: "the-wrong-password",
	}, "198.51.100.7", "curl/8")
	if err == nil {
		t.Fatal("the wrong password signed in")
	}

	call := flows.invoked("alert_security")
	if call == nil {
		t.Fatal("the on_failed_login hook ran nothing")
	}
	event, _ := call.input["auth"].(map[string]interface{})
	if event["ip"] != "198.51.100.7" {
		t.Errorf("ip = %v", event["ip"])
	}
	if event["reason"] == nil {
		t.Error("the flow was not told why it failed")
	}
}

func TestABeforeHookCanRefuseASignIn(t *testing.T) {
	// The one thing a before_ hook can do that an after_ hook cannot, and the
	// reason on_error = "fail" exists.
	manager, flows := managerWithHooks(t, &HooksConfig{
		BeforeLogin: &HookConfig{Flow: "check_allowlist", OnError: "fail"},
	})
	registered(t, manager, "someone@example.test", "a-good-password")
	flows.failing["check_allowlist"] = true

	_, _, err := manager.Login(context.Background(), &LoginRequest{
		Email: "someone@example.test", Password: "a-good-password",
	}, "203.0.113.10", "Mozilla/5.0")
	if err == nil {
		t.Fatal("a sign-in the hook refused went through")
	}
	if authErr, ok := err.(*AuthError); !ok || authErr.Code != "login_refused" {
		t.Errorf("refusal = %v", err)
	}
}

func TestAFailingHookDoesNotBreakTheThingItWatches(t *testing.T) {
	// The default. A notification that cannot be sent must not stop somebody
	// signing in — the alternative is a service whose sign-in depends on Slack.
	manager, flows := managerWithHooks(t, &HooksConfig{
		AfterLogin: &HookConfig{Flow: "record_sign_in"},
	})
	registered(t, manager, "someone@example.test", "a-good-password")
	flows.failing["record_sign_in"] = true

	if _, _, err := manager.Login(context.Background(), &LoginRequest{
		Email: "someone@example.test", Password: "a-good-password",
	}, "203.0.113.10", "Mozilla/5.0"); err != nil {
		t.Errorf("a failing after_login hook stopped a sign-in: %v", err)
	}
}

func TestAConditionNarrowsWhenTheFlowRuns(t *testing.T) {
	manager, flows := managerWithHooks(t, &HooksConfig{
		OnFailedLogin: &HookConfig{
			Flow:      "alert_security",
			Condition: `auth.ip == "198.51.100.7"`,
		},
	})
	registered(t, manager, "someone@example.test", "a-good-password")

	// An address the condition does not match.
	_, _, _ = manager.Login(context.Background(), &LoginRequest{
		Email: "someone@example.test", Password: "wrong",
	}, "203.0.113.10", "Mozilla/5.0")
	if flows.count() != 0 {
		t.Fatalf("the flow ran for an event the condition excluded: %+v", flows.calls)
	}

	// And one it does.
	_, _, _ = manager.Login(context.Background(), &LoginRequest{
		Email: "someone@example.test", Password: "wrong",
	}, "198.51.100.7", "curl/8")
	if flows.invoked("alert_security") == nil {
		t.Error("the flow did not run for an event the condition matched")
	}
}

func TestChangingAPasswordIsAnEventToo(t *testing.T) {
	manager, flows := managerWithHooks(t, &HooksConfig{
		AfterPasswordChange: &HookConfig{Flow: "tell_them"},
	})
	user := registered(t, manager, "someone@example.test", "a-good-password")

	if err := manager.ChangePassword(context.Background(), user.ID, "a-good-password", "a-different-password"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if flows.invoked("tell_them") == nil {
		t.Error("the after_password_change hook ran nothing")
	}
}

func TestAHookWithNothingToRunFlowsWithIsNotACrash(t *testing.T) {
	// A manager built outside the runtime — every test in this package that
	// does not care about hooks, and anything embedding auth directly.
	manager, err := NewManager(&Config{
		Preset: "development",
		JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Hooks:  &HooksConfig{AfterLogin: &HookConfig{Flow: "record_sign_in"}},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := manager.runHook(context.Background(), HookAfterLogin, nil); err != nil {
		t.Errorf("runHook with no invoker: %v", err)
	}
}

func TestAHooksBlockThatCannotWorkIsRefusedAtStartup(t *testing.T) {
	// Refusing after the fact cannot undo it, so a hook that says so has been
	// misunderstood — and finding out at startup beats finding out from the
	// first person whose password change half-happened.
	for name, hooks := range map[string]*HooksConfig{
		"refusing after the change is made": {
			AfterPasswordChange: &HookConfig{Flow: "tell_them", OnError: "fail"},
		},
		"a word that is neither": {
			BeforeLogin: &HookConfig{Flow: "check", OnError: "maybe"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewManager(&Config{
				Preset: "development",
				JWT:    &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
				Hooks:  hooks,
			})
			if err == nil {
				t.Fatal("the configuration was accepted")
			}
			if !strings.Contains(err.Error(), "hook") {
				t.Errorf("the error does not say which part is wrong: %v", err)
			}
		})
	}
}
