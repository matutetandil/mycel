package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The impossible_travel block declared max_speed_kmh, on_detect and a geoip
// source, and every one of them was read by nothing — so a service configured
// to block a sign-in from the other side of the world thirty seconds after one
// at home did not notice it. The strict preset turns this on, which made the
// gap worse: the strictest setting and the weakest had the same effect.

// placeMap is a lookup with the answers written down.
type placeMap struct {
	places map[string]*Location
	err    error
}

func (p placeMap) Locate(ctx context.Context, ip string) (*Location, error) {
	if p.err != nil {
		return nil, p.err
	}
	if place, held := p.places[ip]; held {
		return place, nil
	}
	return nil, ErrLocationUnknown
}

func (p placeMap) Close() error { return nil }

var (
	london    = &Location{Latitude: 51.5074, Longitude: -0.1278, Label: "London, GB"}
	sydney    = &Location{Latitude: -33.8688, Longitude: 151.2093, Label: "Sydney, AU"}
	edinburgh = &Location{Latitude: 55.9533, Longitude: -3.1883, Label: "Edinburgh, GB"}
)

func travelService(t *testing.T, travel *ImpossibleTravelConfig, lookup GeoIPLookup) (*Manager, *recordingFlows) {
	t.Helper()

	flows := newRecordingFlows()
	manager, err := NewManager(&Config{
		Preset:   "development",
		JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
		Password: &PasswordConfig{MinLength: 8},
		Security: &SecurityConfig{ImpossibleTravel: travel},
		Hooks:    &HooksConfig{OnSuspiciousActivity: &HookConfig{Flow: "tell_security"}},
	}, WithFlowInvoker(flows), WithGeoIP(lookup))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	registered(t, manager, "someone@example.test", "a-good-password")
	return manager, flows
}

func TestTwoSignInsTooFarApartAreNoticed(t *testing.T) {
	lookup := placeMap{places: map[string]*Location{
		"203.0.113.10": london,
		"198.51.100.7": sydney,
	}}
	manager, flows := travelService(t, &ImpossibleTravelConfig{
		Enabled: true, MaxSpeedKMH: 900, OnDetect: "notify",
		GeoIP: &GeoIPConfig{API: "https://geo.example/{ip}"},
	}, lookup)

	if err := signIn(t, manager, "Mozilla/5.0", "203.0.113.10"); err != nil {
		t.Fatalf("first sign-in: %v", err)
	}
	if err := signIn(t, manager, "Mozilla/5.0", "198.51.100.7"); err != nil {
		t.Fatalf("second sign-in: %v", err)
	}

	call := flows.invoked("tell_security")
	if call == nil {
		t.Fatal("London then Sydney a moment later was not noticed")
	}
	event, _ := call.input["auth"].(map[string]interface{})
	if event["reason"] != "impossible_travel" {
		t.Errorf("reason = %v", event["reason"])
	}
	// What somebody reading the alert needs: where from, where to, how far.
	if event["from"] != "London, GB" || event["to"] != "Sydney, AU" {
		t.Errorf("from %v to %v", event["from"], event["to"])
	}
	if km, _ := event["km"].(int); km < 16000 || km > 18000 {
		t.Errorf("distance = %v km, want about 17000", event["km"])
	}
}

func TestAJourneySomebodyCouldHaveMadeIsNotNoticed(t *testing.T) {
	// London to Edinburgh is a train ride. Calling that impossible would make
	// the whole thing noise nobody reads.
	lookup := placeMap{places: map[string]*Location{
		"203.0.113.10": london,
		"198.51.100.7": edinburgh,
	}}
	manager, flows := travelService(t, &ImpossibleTravelConfig{
		Enabled: true, MaxSpeedKMH: 900, OnDetect: "notify",
		GeoIP: &GeoIPConfig{API: "https://geo.example/{ip}"},
	}, lookup)

	_ = signIn(t, manager, "Mozilla/5.0", "203.0.113.10")

	// Move the last sighting back an hour: 530km in an hour is a fast train,
	// not a teleport.
	manager.travel.mu.Lock()
	for id, place := range manager.travel.places {
		place.at = time.Now().Add(-time.Hour)
		manager.travel.places[id] = place
	}
	manager.travel.mu.Unlock()

	_ = signIn(t, manager, "Mozilla/5.0", "198.51.100.7")
	if flows.count() != 0 {
		t.Errorf("a journey somebody could have made was reported: %+v", flows.calls)
	}
}

func TestBlockingRefusesTheSecondSignIn(t *testing.T) {
	lookup := placeMap{places: map[string]*Location{
		"203.0.113.10": london,
		"198.51.100.7": sydney,
	}}
	manager, _ := travelService(t, &ImpossibleTravelConfig{
		Enabled: true, MaxSpeedKMH: 900, OnDetect: "block",
		GeoIP: &GeoIPConfig{API: "https://geo.example/{ip}"},
	}, lookup)

	if err := signIn(t, manager, "Mozilla/5.0", "203.0.113.10"); err != nil {
		t.Fatalf("first sign-in: %v", err)
	}
	err := signIn(t, manager, "Mozilla/5.0", "198.51.100.7")
	if err == nil {
		t.Fatal("the second sign-in went through with on_detect = block")
	}
	if authErr, ok := err.(*AuthError); !ok || authErr.Code != "impossible_travel" {
		t.Errorf("refusal = %v", err)
	}
}

func TestTheFirstSignInIsNeverImpossible(t *testing.T) {
	// There is nothing to compare it against.
	lookup := placeMap{places: map[string]*Location{"198.51.100.7": sydney}}
	manager, flows := travelService(t, &ImpossibleTravelConfig{
		Enabled: true, MaxSpeedKMH: 900, OnDetect: "block",
		GeoIP: &GeoIPConfig{API: "https://geo.example/{ip}"},
	}, lookup)

	if err := signIn(t, manager, "Mozilla/5.0", "198.51.100.7"); err != nil {
		t.Errorf("the first sign-in an account ever made was refused: %v", err)
	}
	if flows.count() != 0 {
		t.Error("the first sign-in was reported as suspicious")
	}
}

func TestAnAddressNobodyCanPlaceIsNotHeldAgainstAnybody(t *testing.T) {
	// Most addresses in most tests, and every address behind a proxy that
	// forwards none.
	lookup := placeMap{places: map[string]*Location{"203.0.113.10": london}}
	manager, flows := travelService(t, &ImpossibleTravelConfig{
		Enabled: true, MaxSpeedKMH: 900, OnDetect: "block",
		GeoIP: &GeoIPConfig{API: "https://geo.example/{ip}"},
	}, lookup)

	_ = signIn(t, manager, "Mozilla/5.0", "203.0.113.10")
	if err := signIn(t, manager, "Mozilla/5.0", "192.0.2.55"); err != nil {
		t.Errorf("a sign-in from an address nothing could place was refused: %v", err)
	}
	if flows.count() != 0 {
		t.Error("an address nothing could place was reported")
	}
}

func TestALocalAddressIsNotLookedUpAtAll(t *testing.T) {
	// A service behind a proxy that forwards no real address sees these for
	// every sign-in, and asking a paid API about 10.0.0.1 costs money to learn
	// nothing.
	asked := 0
	counting := &countingLookup{inner: placeMap{places: map[string]*Location{}}, asked: &asked}
	manager, _ := travelService(t, &ImpossibleTravelConfig{
		Enabled: true, MaxSpeedKMH: 900, OnDetect: "block",
		GeoIP: &GeoIPConfig{API: "https://geo.example/{ip}"},
	}, counting)

	_ = signIn(t, manager, "Mozilla/5.0", "10.0.0.4")
	_ = signIn(t, manager, "Mozilla/5.0", "127.0.0.1")
	if asked != 0 {
		t.Errorf("a local address was looked up %d times", asked)
	}
}

type countingLookup struct {
	inner GeoIPLookup
	asked *int
}

func (c *countingLookup) Locate(ctx context.Context, ip string) (*Location, error) {
	*c.asked++
	return c.inner.Locate(ctx, ip)
}

func (c *countingLookup) Close() error { return nil }

func TestAGeoIPServiceHavingABadDayIsNotAnOutageHere(t *testing.T) {
	failing := placeMap{err: errors.New("the service is down")}
	manager, _ := travelService(t, &ImpossibleTravelConfig{
		Enabled: true, MaxSpeedKMH: 900, OnDetect: "block",
		GeoIP: &GeoIPConfig{API: "https://geo.example/{ip}"},
	}, failing)

	if err := signIn(t, manager, "Mozilla/5.0", "203.0.113.10"); err != nil {
		t.Errorf("a geolocation service being down stopped a sign-in: %v", err)
	}
}

func TestABlockThatCannotWorkIsRefusedAtStartup(t *testing.T) {
	for name, travel := range map[string]*ImpossibleTravelConfig{
		"enabled with nowhere to look anything up": {Enabled: true, MaxSpeedKMH: 900},
		"an action nobody implemented": {
			Enabled: true, OnDetect: "quarantine", GeoIP: &GeoIPConfig{API: "https://geo.example/{ip}"},
		},
		"both a file and a service": {
			Enabled: true, GeoIP: &GeoIPConfig{Database: "/tmp/city.mmdb", API: "https://geo.example/{ip}"},
		},
		"a url with no address in it": {
			Enabled: true, GeoIP: &GeoIPConfig{API: "https://geo.example/lookup"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewManager(&Config{
				Preset:   "development",
				JWT:      &JWTConfig{Secret: "a-secret-long-enough-for-signing"},
				Security: &SecurityConfig{ImpossibleTravel: travel},
			})
			if err == nil {
				t.Fatal("the configuration was accepted")
			}
		})
	}
}

func TestDistanceIsMeasuredOverTheGround(t *testing.T) {
	// Known distances, so that a wrong formula shows up as a wrong number
	// rather than as detections nobody can explain.
	for name, tc := range map[string]struct {
		a, b *Location
		want float64
	}{
		"London to Sydney":    {london, sydney, 16990},
		"London to Edinburgh": {london, edinburgh, 534},
		"nowhere at all":      {london, london, 0},
	} {
		t.Run(name, func(t *testing.T) {
			got := kilometresBetween(tc.a, tc.b)
			if got < tc.want*0.98 || got > tc.want*1.02+1 {
				t.Errorf("%.0f km, want about %.0f", got, tc.want)
			}
		})
	}
}

func TestAnAPILookupReadsWhateverTheProviderCallsIt(t *testing.T) {
	// Every provider names these differently, and a lookup that understands
	// only one of them works with only one provider.
	for name, body := range map[string]map[string]interface{}{
		"latitude and longitude": {"latitude": 51.5074, "longitude": -0.1278, "city": "London"},
		"lat and lon":            {"lat": 51.5074, "lon": -0.1278, "city": "London"},
		"lat and lng":            {"lat": 51.5074, "lng": -0.1278, "city": "London"},
		"nested under location":  {"location": map[string]interface{}{"lat": 51.5074, "lng": -0.1278}},
		"as strings":             {"latitude": "51.5074", "longitude": "-0.1278"},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()

			lookup, err := NewAPILookup(server.URL + "/{ip}")
			if err != nil {
				t.Fatalf("NewAPILookup: %v", err)
			}
			place, err := lookup.Locate(context.Background(), "203.0.113.10")
			if err != nil {
				t.Fatalf("Locate: %v", err)
			}
			if place.Latitude < 51.5 || place.Latitude > 51.51 {
				t.Errorf("latitude = %v", place.Latitude)
			}
		})
	}
}

func TestALookupIsNotRepeatedForTheSameAddress(t *testing.T) {
	// Against a paid service every repeat costs something, and the same person
	// signing in twice is the common case.
	asked := 0
	counting := &countingLookup{
		inner: placeMap{places: map[string]*Location{"203.0.113.10": london}},
		asked: &asked,
	}
	cached := newCachedLookup(counting, time.Hour)

	for i := 0; i < 5; i++ {
		if _, err := cached.Locate(context.Background(), "203.0.113.10"); err != nil {
			t.Fatalf("Locate: %v", err)
		}
	}
	if asked != 1 {
		t.Errorf("the service was asked %d times about one address", asked)
	}
}

func TestAServiceThatIsDownIsNotCached(t *testing.T) {
	// Otherwise a blip becomes an hour of no detection at all.
	asked := 0
	counting := &countingLookup{inner: placeMap{err: errors.New("down")}, asked: &asked}
	cached := newCachedLookup(counting, time.Hour)

	_, _ = cached.Locate(context.Background(), "203.0.113.10")
	_, _ = cached.Locate(context.Background(), "203.0.113.10")
	if asked != 2 {
		t.Errorf("a failing lookup was asked %d times, want it retried", asked)
	}
}
