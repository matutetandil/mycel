package auth

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Two sign-ins too far apart to be the same person.
//
// The block declared max_speed_kmh, on_detect and a geoip source, and every one
// of them was read by nothing — so a service configured to block a sign-in from
// the other side of the world thirty seconds after one at home did not notice
// it. The strict preset turns this on, which made the gap worse: the strictest
// setting available and the weakest had the same effect.
//
// What it measures is the straight line over the ground divided by the time
// between two sign-ins. Anything a person could actually travel is longer than
// a straight line, so the speed computed this way is the slowest they could
// have been going — and calling that impossible is a claim that holds.

// lastSeenPlace is where an account last signed in from.
type lastSeenPlace struct {
	location *Location
	at       time.Time
}

// travelHistory remembers the last place each account was seen.
//
// In the process, deliberately: this is a comparison between two consecutive
// sign-ins, not a record, and a service that loses it on a restart misses one
// detection rather than keeping a location history of its users on disk. The
// bound is what stops it growing for the life of the process.
type travelHistory struct {
	mu     sync.Mutex
	places map[string]lastSeenPlace
}

func newTravelHistory() *travelHistory {
	return &travelHistory{places: make(map[string]lastSeenPlace)}
}

func (h *travelHistory) last(userID string) (lastSeenPlace, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	place, held := h.places[userID]
	return place, held
}

func (h *travelHistory) record(userID string, location *Location, at time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.places) > 50000 {
		h.places = make(map[string]lastSeenPlace)
	}
	h.places[userID] = lastSeenPlace{location: location, at: at}
}

// WithGeoIP sets where addresses are looked up.
func WithGeoIP(lookup GeoIPLookup) ManagerOption {
	return func(m *Manager) {
		m.geoip = lookup
	}
}

func (m *Manager) impossibleTravelConfig() *ImpossibleTravelConfig {
	if m.config.Security == nil {
		return nil
	}
	return m.config.Security.ImpossibleTravel
}

func (m *Manager) impossibleTravelEnabled() bool {
	travel := m.impossibleTravelConfig()
	return travel != nil && travel.Enabled
}

// maxTravelSpeed is the speed above which two sign-ins are not the same person.
func (m *Manager) maxTravelSpeed() float64 {
	travel := m.impossibleTravelConfig()
	if travel == nil || travel.MaxSpeedKMH <= 0 {
		// Faster than a commercial flight, so that a real journey between two
		// airports does not trip it.
		return 900
	}
	return float64(travel.MaxSpeedKMH)
}

// checkTravel decides what the distance between this sign-in and the last one
// means.
//
// Like the device check it runs once the password has been accepted: somebody
// guessing at a password should not be teaching the service where the account
// was last seen.
func (m *Manager) checkTravel(ctx context.Context, user *User, req *LoginRequest, ip, userAgent string) error {
	if !m.impossibleTravelEnabled() || m.geoip == nil {
		return nil
	}
	// Nothing to place, and nothing to be learnt from trying: a service behind
	// a proxy that forwards no real address sees these for every sign-in.
	if isPrivateAddress(ip) {
		return nil
	}

	here, err := m.geoip.Locate(ctx, ip)
	if err != nil {
		if err != ErrLocationUnknown {
			// The source is down or misconfigured. Worth saying, and not worth
			// refusing a sign-in over: an outage in a geolocation service must
			// not become an outage here.
			m.logger.Warn("an address could not be located", "error", err)
		}
		return nil
	}

	now := time.Now()
	previous, held := m.travel.last(user.ID)
	// Whatever else happens, this is where they are now.
	defer m.travel.record(user.ID, here, now)

	if !held || previous.location == nil {
		return nil
	}

	hours := now.Sub(previous.at).Hours()
	if hours <= 0 {
		return nil
	}
	distance := kilometresBetween(previous.location, here)
	speed := distance / hours
	if speed <= m.maxTravelSpeed() {
		return nil
	}

	travel := m.impossibleTravelConfig()
	action := "notify"
	if travel.OnDetect != "" {
		action = travel.OnDetect
	}

	m.logger.Warn("a sign-in came from further away than the time between them allows",
		"user_id", user.ID, "from", previous.location.Label, "to", here.Label,
		"km", int(distance), "km_per_hour", int(speed), "action", action)

	_ = m.runHook(ctx, HookOnSuspiciousActivity, map[string]interface{}{
		"user_id": user.ID, "email": user.Email, "ip": ip, "user_agent": userAgent,
		"reason":      "impossible_travel",
		"from":        previous.location.Label,
		"to":          here.Label,
		"km":          int(distance),
		"km_per_hour": int(speed),
		"action":      action,
	})

	switch action {
	case "block":
		m.auditFailure(ctx, AuditLogin, user.Email, ip, userAgent, ErrImpossibleTravel)
		return ErrImpossibleTravel
	case "challenge":
		// The same reasoning as a new device: an account with nothing to
		// challenge with is let through and the gap is said out loud, rather
		// than locking somebody out of their own account over a geolocation
		// database's opinion of an address.
		if !user.MFAEnabled {
			m.logger.Warn("a sign-in would be challenged but this account has no second factor",
				"user_id", user.ID)
			return nil
		}
		if req == nil || req.MFACode == "" {
			return ErrMFARequired
		}
	}
	return nil
}

// validateImpossibleTravel refuses a block that cannot do what it says.
func validateImpossibleTravel(cfg *Config) error {
	if cfg.Security == nil || cfg.Security.ImpossibleTravel == nil {
		return nil
	}
	travel := cfg.Security.ImpossibleTravel

	switch travel.OnDetect {
	case "", "notify", "challenge", "block":
	default:
		return fmt.Errorf("security impossible_travel: on_detect = %q is not one of notify, challenge or block",
			travel.OnDetect)
	}
	if travel.MaxSpeedKMH < 0 {
		return fmt.Errorf("security impossible_travel: max_speed_kmh cannot be negative")
	}
	if !travel.Enabled {
		return nil
	}
	// Enabled with nowhere to look an address up cannot work, and finding that
	// out at startup beats believing it is on.
	if travel.GeoIP == nil || (travel.GeoIP.Database == "" && travel.GeoIP.API == "") {
		return fmt.Errorf("security impossible_travel is enabled but its geoip block names neither a database nor an api, " +
			"so no address can be placed")
	}
	if travel.GeoIP.Database != "" && travel.GeoIP.API != "" {
		return fmt.Errorf("security impossible_travel: the geoip block names both a database and an api; name one")
	}
	return nil
}

// buildGeoIP opens whatever the geoip block names.
func buildGeoIP(cfg *Config) (GeoIPLookup, error) {
	if cfg.Security == nil || cfg.Security.ImpossibleTravel == nil {
		return nil, nil
	}
	travel := cfg.Security.ImpossibleTravel
	if !travel.Enabled || travel.GeoIP == nil {
		return nil, nil
	}

	switch {
	case travel.GeoIP.Database != "":
		lookup, err := NewMMDBLookup(travel.GeoIP.Database)
		if err != nil {
			return nil, err
		}
		return newCachedLookup(lookup, time.Hour), nil
	case travel.GeoIP.API != "":
		lookup, err := NewAPILookup(travel.GeoIP.API)
		if err != nil {
			return nil, err
		}
		return newCachedLookup(lookup, time.Hour), nil
	}
	return nil, nil
}
