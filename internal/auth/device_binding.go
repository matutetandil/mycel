package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Recognising the thing somebody is signing in from.
//
// The whole block was read by nothing: max_devices, trust_duration and
// on_new_device parsed, appeared in the guide, and decided nothing. Worse than
// an unread setting on its own, because the strict and standard presets turn
// device binding on — so a service that asked for no preset at all and one that
// asked for the strictest both had exactly the same defence, which is none.
//
// A device is identified by what the request already carries, not by anything
// installed on it: the browser string, and whatever else the deployment lists
// in `fingerprint`. That is weak on its own — two people on the same browser
// version look alike — which is why the useful settings are the ones that ask
// for a second factor or tell somebody, rather than the one that blocks.

// KnownDevice is a device an account has signed in from.
type KnownDevice struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Fingerprint string    `json:"fingerprint"`
	UserAgent   string    `json:"user_agent,omitempty"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// DeviceStore remembers which devices an account has signed in from.
type DeviceStore interface {
	// Find returns a device by its fingerprint, or ErrDeviceNotFound.
	Find(ctx context.Context, userID, fingerprint string) (*KnownDevice, error)

	// Remember records a device, or updates when it was last seen.
	Remember(ctx context.Context, device *KnownDevice) error

	// List returns every device an account is known to use, oldest first.
	List(ctx context.Context, userID string) ([]*KnownDevice, error)

	// Forget drops a device.
	Forget(ctx context.Context, userID, fingerprint string) error
}

// ErrDeviceNotFound is what a store returns for a device it has not seen.
var ErrDeviceNotFound = &AuthError{Code: "device_not_found", Message: "Device not recognised"}

// MemoryDeviceStore keeps known devices in the process.
type MemoryDeviceStore struct {
	mu      sync.RWMutex
	devices map[string][]*KnownDevice // by user
}

// NewMemoryDeviceStore creates an in-process device store.
func NewMemoryDeviceStore() *MemoryDeviceStore {
	return &MemoryDeviceStore{devices: make(map[string][]*KnownDevice)}
}

func (s *MemoryDeviceStore) Find(ctx context.Context, userID, fingerprint string) (*KnownDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, d := range s.devices[userID] {
		if d.Fingerprint == fingerprint {
			copied := *d
			return &copied, nil
		}
	}
	return nil, ErrDeviceNotFound
}

func (s *MemoryDeviceStore) Remember(ctx context.Context, device *KnownDevice) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, d := range s.devices[device.UserID] {
		if d.Fingerprint == device.Fingerprint {
			d.LastSeen = device.LastSeen
			d.UserAgent = device.UserAgent
			return nil
		}
	}
	copied := *device
	s.devices[device.UserID] = append(s.devices[device.UserID], &copied)
	return nil
}

func (s *MemoryDeviceStore) List(ctx context.Context, userID string) ([]*KnownDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	held := s.devices[userID]
	out := make([]*KnownDevice, 0, len(held))
	for _, d := range held {
		copied := *d
		out = append(out, &copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FirstSeen.Before(out[j].FirstSeen) })
	return out, nil
}

func (s *MemoryDeviceStore) Forget(ctx context.Context, userID, fingerprint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	held := s.devices[userID]
	for i, d := range held {
		if d.Fingerprint == fingerprint {
			s.devices[userID] = append(held[:i], held[i+1:]...)
			return nil
		}
	}
	return nil
}

// WithDeviceStore sets where known devices are remembered.
func WithDeviceStore(store DeviceStore) ManagerOption {
	return func(m *Manager) {
		m.devices = store
	}
}

// deviceBindingEnabled reports whether devices are being watched at all.
func (m *Manager) deviceBindingEnabled() bool {
	return m.config.Security != nil &&
		m.config.Security.DeviceBinding != nil &&
		m.config.Security.DeviceBinding.Enabled
}

// deviceFingerprint is what identifies the thing somebody is signing in from.
//
// The default is the browser string alone, which is the only thing every
// request carries. A deployment behind a proxy that adds more can list the
// fields it wants folded in; anything it names and does not send is left out
// rather than treated as empty, so a header that stops arriving does not turn
// every device into a new one at once.
func (m *Manager) deviceFingerprint(req *LoginRequest, ip, userAgent string) string {
	parts := []string{}

	fields := []string{"user_agent"}
	if binding := m.deviceBindingConfig(); binding != nil && len(binding.Fingerprint) > 0 {
		fields = binding.Fingerprint
	}

	// An empty value contributes nothing rather than contributing emptiness. A
	// proxy that stops forwarding the browser string would otherwise leave
	// every account with one device whose fingerprint is the hash of nothing.
	for _, field := range fields {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "user_agent":
			if userAgent != "" {
				parts = append(parts, "ua="+userAgent)
			}
		case "ip":
			// Coarse on purpose: a phone changes address between one street
			// and the next, and a device that is new every time is no device
			// at all.
			if network := ipNetwork(ip); network != "" {
				parts = append(parts, "ip="+network)
			}
		case "device_id":
			// What a client that keeps its own identifier sends.
			if req != nil && req.DeviceID != "" {
				parts = append(parts, "did="+req.DeviceID)
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

// ipNetwork keeps the network an address belongs to and drops the host, so that
// a device does not stop being itself when it moves.
func ipNetwork(ip string) string {
	if ip == "" {
		return ""
	}
	if strings.Contains(ip, ":") {
		// IPv6: the first four groups, which is the usual /64.
		groups := strings.Split(ip, ":")
		if len(groups) > 4 {
			groups = groups[:4]
		}
		return strings.Join(groups, ":")
	}
	octets := strings.Split(ip, ".")
	if len(octets) == 4 {
		return strings.Join(octets[:3], ".")
	}
	return ip
}

func (m *Manager) deviceBindingConfig() *DeviceBindingConfig {
	if m.config.Security == nil {
		return nil
	}
	return m.config.Security.DeviceBinding
}

// deviceTrustLifetime is how long a device stays recognised without being used.
func (m *Manager) deviceTrustLifetime() time.Duration {
	binding := m.deviceBindingConfig()
	if binding == nil || binding.TrustDuration == "" {
		return 0
	}
	trust, err := ParseDuration(binding.TrustDuration)
	if err != nil {
		return 0
	}
	return trust
}

// checkDevice decides what a sign-in from this device means.
//
// It is called once the password has been accepted and before a session is
// opened, so that a refusal here is a refusal to open one — and so that
// somebody typing the wrong password does not teach the service a new device.
func (m *Manager) checkDevice(ctx context.Context, user *User, req *LoginRequest, ip, userAgent string) error {
	if !m.deviceBindingEnabled() {
		return nil
	}
	binding := m.deviceBindingConfig()

	fingerprint := m.deviceFingerprint(req, ip, userAgent)
	if fingerprint == "" {
		// Nothing to go on. Refusing every sign-in because a header is missing
		// would be a worse failure than not recognising the device.
		m.logger.Debug("device binding is on and this request carries nothing to identify a device",
			"user_id", user.ID)
		return nil
	}

	now := time.Now()
	known, err := m.devices.Find(ctx, user.ID, fingerprint)
	if err == nil && !m.deviceHasGoneCold(known, now) {
		known.LastSeen = now
		if err := m.devices.Remember(ctx, known); err != nil {
			m.logger.Warn("could not record that a device was used", "user_id", user.ID, "error", err)
		}
		return nil
	}

	// From here it is a device this account has not used, or has not used
	// within trust_duration.
	action := "allow"
	if binding.OnNewDevice != "" {
		action = binding.OnNewDevice
	}

	_ = m.runHook(ctx, HookOnSuspiciousActivity, map[string]interface{}{
		"user_id": user.ID, "email": user.Email, "ip": ip, "user_agent": userAgent,
		"reason": "new_device", "device": fingerprint, "action": action,
	})

	switch action {
	case "block":
		m.auditFailure(ctx, AuditLogin, user.Email, ip, userAgent, ErrUnknownDevice)
		return ErrUnknownDevice

	case "challenge":
		// A second factor is the honest answer to "we do not recognise this":
		// it neither locks somebody out of a new laptop nor lets whoever has
		// the password in unchallenged. Without MFA to challenge with there is
		// nothing to fall back to but letting them in, said out loud.
		if !user.MFAEnabled {
			m.logger.Warn("a new device would be challenged but this account has no second factor",
				"user_id", user.ID)
			break
		}
		if req == nil || req.MFACode == "" {
			return ErrMFARequired
		}
	}

	// Remembered whatever the action was, apart from a refusal: the next
	// sign-in from a device somebody was told about should be quiet.
	if err := m.rememberDevice(ctx, user, fingerprint, userAgent, now); err != nil {
		m.logger.Warn("could not remember a device", "user_id", user.ID, "error", err)
	}
	return nil
}

// deviceHasGoneCold reports whether a known device has been unused for longer
// than trust_duration, which makes it new again.
func (m *Manager) deviceHasGoneCold(device *KnownDevice, now time.Time) bool {
	trust := m.deviceTrustLifetime()
	if trust <= 0 {
		return false
	}
	return now.Sub(device.LastSeen) > trust
}

// rememberDevice records a device, dropping the least recently used when the
// account is already at max_devices.
func (m *Manager) rememberDevice(ctx context.Context, user *User, fingerprint, userAgent string, now time.Time) error {
	binding := m.deviceBindingConfig()
	if binding != nil && binding.MaxDevices > 0 {
		known, err := m.devices.List(ctx, user.ID)
		if err != nil {
			return err
		}
		// Least recently used first, so that the one being dropped is the one
		// nobody has signed in from for longest rather than the oldest.
		sort.Slice(known, func(i, j int) bool { return known[i].LastSeen.Before(known[j].LastSeen) })
		for len(known) >= binding.MaxDevices {
			dropped := known[0]
			known = known[1:]
			if err := m.devices.Forget(ctx, user.ID, dropped.Fingerprint); err != nil {
				return err
			}
			m.logger.Info("a device was forgotten to stay within max_devices",
				"user_id", user.ID, "max_devices", binding.MaxDevices)
		}
	}

	id, err := generateID()
	if err != nil {
		return err
	}
	return m.devices.Remember(ctx, &KnownDevice{
		ID:          id,
		UserID:      user.ID,
		Fingerprint: fingerprint,
		UserAgent:   userAgent,
		FirstSeen:   now,
		LastSeen:    now,
	})
}

// validateDeviceBinding refuses a block that cannot do what it says.
func validateDeviceBinding(cfg *Config) error {
	if cfg.Security == nil || cfg.Security.DeviceBinding == nil {
		return nil
	}
	binding := cfg.Security.DeviceBinding

	switch binding.OnNewDevice {
	case "", "allow", "challenge", "block", "notify":
	default:
		return fmt.Errorf("security device_binding: on_new_device = %q is not one of allow, challenge, block or notify",
			binding.OnNewDevice)
	}
	if binding.TrustDuration != "" {
		if _, err := ParseDuration(binding.TrustDuration); err != nil {
			return fmt.Errorf("security device_binding: trust_duration %q is not a duration", binding.TrustDuration)
		}
	}
	for _, field := range binding.Fingerprint {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "user_agent", "ip", "device_id":
		default:
			return fmt.Errorf("security device_binding: fingerprint field %q is not one this can see; "+
				"use user_agent, ip or device_id", field)
		}
	}
	return nil
}
