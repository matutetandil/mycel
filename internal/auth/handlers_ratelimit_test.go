package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The limiter was written per endpoint and per caller, and nothing built it, so
// the block configuring it did nothing. It answers a question neither of the
// other two protections does: the connector's limit caps a whole server, and
// brute force locks one account — this refuses a flood spread across many,
// which is what credential stuffing looks like.

func limitedServer(t *testing.T, cfg *RateLimitConfig) *httptest.Server {
	t.Helper()
	m := ssoManager(t, &Config{
		Preset:   "development",
		Security: &SecurityConfig{RateLimit: cfg},
	})
	mux := http.NewServeMux()
	NewHandler(m).RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, srv *httptest.Server, path string) int {
	t.Helper()
	resp, err := http.Post(srv.URL+path, "application/json", nil)
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestTheConfiguredLimitIsEnforced(t *testing.T) {
	srv := limitedServer(t, &RateLimitConfig{
		Enabled: true,
		Login:   &EndpointRateLimit{Rate: 3, Window: "1m"},
	})

	for i := 1; i <= 3; i++ {
		if code := post(t, srv, "/auth/login"); code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was refused, and three are allowed", i)
		}
	}
	if code := post(t, srv, "/auth/login"); code != http.StatusTooManyRequests {
		t.Errorf("the fourth attempt answered %d, want 429", code)
	}
}

func TestEndpointsAreLimitedSeparately(t *testing.T) {
	// A login limit that also capped registration would be a surprising way to
	// take down sign-ups.
	srv := limitedServer(t, &RateLimitConfig{
		Enabled: true,
		Login:   &EndpointRateLimit{Rate: 1, Window: "1m"},
	})

	post(t, srv, "/auth/login")
	if code := post(t, srv, "/auth/login"); code != http.StatusTooManyRequests {
		t.Fatalf("login = %d, want the limit to have applied", code)
	}
	if code := post(t, srv, "/auth/register"); code == http.StatusTooManyRequests {
		t.Error("registration was refused by the login limit")
	}
}

func TestARefusalSaysWhenToComeBack(t *testing.T) {
	srv := limitedServer(t, &RateLimitConfig{
		Enabled: true,
		Login:   &EndpointRateLimit{Rate: 1, Window: "1m"},
	})

	post(t, srv, "/auth/login")
	resp, err := http.Post(srv.URL+"/auth/login", "application/json", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("a 429 with no Retry-After leaves a well-behaved client guessing")
	}
}

func TestWithoutTheBlockNothingIsLimited(t *testing.T) {
	srv := limitedServer(t, nil)
	for i := 0; i < 20; i++ {
		if code := post(t, srv, "/auth/login"); code == http.StatusTooManyRequests {
			t.Fatalf("request %d was refused with no rate_limit block configured", i)
		}
	}
}

func TestDisabledMeansDisabled(t *testing.T) {
	srv := limitedServer(t, &RateLimitConfig{
		Enabled: false,
		Login:   &EndpointRateLimit{Rate: 1, Window: "1m"},
	})
	for i := 0; i < 5; i++ {
		if code := post(t, srv, "/auth/login"); code == http.StatusTooManyRequests {
			t.Fatalf("request %d was refused although the block says enabled = false", i)
		}
	}
}

func TestAWrittenRateIsNotWidenedByADefaultBurst(t *testing.T) {
	// The burst used to come from a table of defaults whenever it was not
	// written, so `login { rate = 1 }` allowed three attempts at once — three
	// times what the person who wrote it asked for.
	limiter := NewPerKeyRateLimiter(&RateLimitConfig{
		Enabled: true,
		Login:   &EndpointRateLimit{Rate: 1, Window: "1m"},
	})

	if !limiter.Allow("login", "1.2.3.4") {
		t.Fatal("the first attempt was refused")
	}
	if limiter.Allow("login", "1.2.3.4") {
		t.Error("a second attempt was allowed against a rate of one")
	}

	// An endpoint nobody configured still gets the sensible default rather
	// than nothing.
	if !limiter.Allow("register", "1.2.3.4") {
		t.Error("an unconfigured endpoint refused its first request")
	}
}

func TestTheLimitIsPerCaller(t *testing.T) {
	// Otherwise one noisy address takes the endpoint away from everyone.
	limiter := NewPerKeyRateLimiter(&RateLimitConfig{
		Enabled: true,
		Login:   &EndpointRateLimit{Rate: 1, Window: "1m"},
	})

	limiter.Allow("login", "1.2.3.4")
	if limiter.Allow("login", "1.2.3.4") {
		t.Fatal("the first caller was not limited")
	}
	if !limiter.Allow("login", "5.6.7.8") {
		t.Error("a second caller was refused because of the first")
	}
}
