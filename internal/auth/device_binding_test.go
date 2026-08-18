package auth

import (
	"context"
	"testing"
	"time"
)

// The device_binding block was read by nothing, and the strict and standard
// presets turn it on — so a service that asked for no preset and one that asked
// for the strictest had the same defence, which is none.

func deviceService(t *testing.T, binding *DeviceBindingConfig) (*Manager, *recordingFlows) {
	t.Helper()

	flows := newRecordingFlows()
	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Password: &PasswordConfig{MinLength: 8},
		Security: &SecurityConfig{DeviceBinding: binding},
		Hooks:    &HooksConfig{OnSuspiciousActivity: &HookConfig{Flow: "tell_security"}},
	}, WithFlowInvoker(flows))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	registered(t, manager, "someone@example.test", "a-good-password")
	return manager, flows
}

func signIn(t *testing.T, manager *Manager, userAgent, ip string) error {
	t.Helper()
	_, _, err := manager.Login(context.Background(), &LoginRequest{
		Email: "someone@example.test", Password: "a-good-password",
	}, ip, userAgent)
	return err
}

func TestSigningInFromTheSameDeviceTwiceIsQuiet(t *testing.T) {
	manager, _ := deviceService(t, &DeviceBindingConfig{Enabled: true, OnNewDevice: "notify"})

	if err := signIn(t, manager, "Mozilla/5.0", "203.0.113.10"); err != nil {
		t.Fatalf("first sign-in: %v", err)
	}
	if err := signIn(t, manager, "Mozilla/5.0", "203.0.113.10"); err != nil {
		t.Fatalf("second sign-in: %v", err)
	}
}

func TestANewDeviceIsSomethingSomebodyCanBeToldAbout(t *testing.T) {
	manager, flows := deviceService(t, &DeviceBindingConfig{Enabled: true, OnNewDevice: "notify"})

	if err := signIn(t, manager, "Mozilla/5.0", "203.0.113.10"); err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	call := flows.invoked("tell_security")
	if call == nil {
		t.Fatal("nothing was told about a device this account had never used")
	}
	event, _ := call.input["auth"].(map[string]interface{})
	if event["reason"] != "new_device" {
		t.Errorf("reason = %v", event["reason"])
	}

	// The second sign-in from the same device says nothing, or every sign-in
	// is an alert and nobody reads them.
	before := flows.count()
	_ = signIn(t, manager, "Mozilla/5.0", "203.0.113.10")
	if flows.count() != before {
		t.Error("a device already known was reported as new")
	}
}

func TestADifferentBrowserIsADifferentDevice(t *testing.T) {
	manager, flows := deviceService(t, &DeviceBindingConfig{Enabled: true, OnNewDevice: "notify"})

	_ = signIn(t, manager, "Mozilla/5.0", "203.0.113.10")
	first := flows.count()
	_ = signIn(t, manager, "curl/8.4", "203.0.113.10")
	if flows.count() == first {
		t.Error("signing in from another browser was not noticed")
	}
}

func TestMovingAroundOnOneDeviceIsStillOneDevice(t *testing.T) {
	// A phone changes address between one street and the next, and a device
	// that is new every time is no device at all.
	manager, flows := deviceService(t, &DeviceBindingConfig{
		Enabled: true, OnNewDevice: "notify", Fingerprint: []string{"user_agent", "ip"},
	})

	_ = signIn(t, manager, "Mozilla/5.0", "203.0.113.10")
	first := flows.count()
	_ = signIn(t, manager, "Mozilla/5.0", "203.0.113.99")
	if flows.count() != first {
		t.Error("the same device on the same network was treated as new")
	}

	// A different network is another matter.
	_ = signIn(t, manager, "Mozilla/5.0", "198.51.100.7")
	if flows.count() == first {
		t.Error("the same browser from another network was not noticed at all")
	}
}

func TestBlockingRefusesASignInFromAnUnknownDevice(t *testing.T) {
	manager, _ := deviceService(t, &DeviceBindingConfig{Enabled: true, OnNewDevice: "block"})

	err := signIn(t, manager, "Mozilla/5.0", "203.0.113.10")
	if err == nil {
		t.Fatal("a sign-in from an unrecognised device went through with on_new_device = block")
	}
	if authErr, ok := err.(*AuthError); !ok || authErr.Code != "unknown_device" {
		t.Errorf("refusal = %v", err)
	}

	// And it stays refused: a blocked device must not become known by being
	// blocked.
	if err := signIn(t, manager, "Mozilla/5.0", "203.0.113.10"); err == nil {
		t.Error("the second attempt from the same blocked device was allowed")
	}
}

func TestChallengingAsksForASecondFactor(t *testing.T) {
	manager, _ := deviceService(t, &DeviceBindingConfig{Enabled: true, OnNewDevice: "challenge"})
	ctx := context.Background()

	user, err := manager.userStore.FindByEmail(ctx, "someone@example.test")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if err := manager.userStore.UpdateMFAEnabled(ctx, user.ID, true); err != nil {
		t.Fatalf("UpdateMFAEnabled: %v", err)
	}

	err = signIn(t, manager, "Mozilla/5.0", "203.0.113.10")
	if err == nil {
		t.Fatal("a new device signed in without being challenged")
	}
	if authErr, ok := err.(*AuthError); !ok || authErr.Code != "mfa_required" {
		t.Errorf("refusal = %v, want a request for a second factor", err)
	}
}

func TestChallengingAnAccountWithNoSecondFactorDoesNotLockItOut(t *testing.T) {
	// There is nothing to challenge with. Letting them in and saying so is
	// better than an account nobody can sign into from a new laptop.
	manager, _ := deviceService(t, &DeviceBindingConfig{Enabled: true, OnNewDevice: "challenge"})

	if err := signIn(t, manager, "Mozilla/5.0", "203.0.113.10"); err != nil {
		t.Errorf("an account with no second factor was locked out of a new device: %v", err)
	}
}

func TestAnAccountKeepsOnlyAsManyDevicesAsItMay(t *testing.T) {
	manager, _ := deviceService(t, &DeviceBindingConfig{
		Enabled: true, OnNewDevice: "allow", MaxDevices: 2,
	})
	ctx := context.Background()

	for _, ua := range []string{"browser-one", "browser-two", "browser-three"} {
		if err := signIn(t, manager, ua, "203.0.113.10"); err != nil {
			t.Fatalf("%s: %v", ua, err)
		}
		// So that last-seen times differ and the least recently used is
		// unambiguous.
		time.Sleep(2 * time.Millisecond)
	}

	user, _ := manager.userStore.FindByEmail(ctx, "someone@example.test")
	known, err := manager.devices.List(ctx, user.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(known) != 2 {
		t.Fatalf("%d devices remembered for a limit of 2", len(known))
	}
	// The one dropped is the one nobody has used for longest.
	for _, d := range known {
		if d.UserAgent == "browser-one" {
			t.Error("the least recently used device was kept and a newer one dropped")
		}
	}
}

func TestADeviceNobodyHasUsedForLongEnoughIsNewAgain(t *testing.T) {
	manager, flows := deviceService(t, &DeviceBindingConfig{
		Enabled: true, OnNewDevice: "notify", TrustDuration: "24h",
	})
	ctx := context.Background()

	_ = signIn(t, manager, "Mozilla/5.0", "203.0.113.10")
	reported := flows.count()

	// Wind the device back past its trust.
	user, _ := manager.userStore.FindByEmail(ctx, "someone@example.test")
	store := manager.devices.(*MemoryDeviceStore)
	store.mu.Lock()
	for _, d := range store.devices[user.ID] {
		d.LastSeen = time.Now().Add(-48 * time.Hour)
	}
	store.mu.Unlock()

	_ = signIn(t, manager, "Mozilla/5.0", "203.0.113.10")
	if flows.count() == reported {
		t.Error("a device unused for twice its trust_duration was still recognised")
	}
}

func TestNothingConfiguredWatchesNothing(t *testing.T) {
	// Every deployment that has not asked for this. Signing in must not start
	// depending on a browser string.
	manager, flows := deviceService(t, nil)

	for _, ua := range []string{"one", "two", "three"} {
		if err := signIn(t, manager, ua, "203.0.113.10"); err != nil {
			t.Fatalf("%s: %v", ua, err)
		}
	}
	if flows.count() != 0 {
		t.Errorf("a service with no device_binding block reported %d events", flows.count())
	}
}

func TestABindingThatCannotWorkIsRefusedAtStartup(t *testing.T) {
	for name, binding := range map[string]*DeviceBindingConfig{
		"an action nobody implemented": {Enabled: true, OnNewDevice: "quarantine"},
		"a trust that is not a time":   {Enabled: true, TrustDuration: "soon"},
		"a field nothing can see":      {Enabled: true, Fingerprint: []string{"screen_resolution"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewManager(&Config{
				Preset:   "development",
				JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
				Security: &SecurityConfig{DeviceBinding: binding},
			})
			if err == nil {
				t.Fatal("the configuration was accepted")
			}
		})
	}
}

func TestARequestCarryingNothingToGoOnDoesNotRefuseEverybody(t *testing.T) {
	// A proxy that stops forwarding the browser string must not become an
	// outage.
	manager, _ := deviceService(t, &DeviceBindingConfig{Enabled: true, OnNewDevice: "block"})

	if err := signIn(t, manager, "", "203.0.113.10"); err != nil {
		t.Errorf("a request with no browser string was refused: %v", err)
	}
}
