package profile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// A profile list is how one logical connector stands in front of several
// backends — a live API and a cached copy of it, a primary and a replica — and
// the fallback order is the answer to "this one is not available, what now?".
// Everything below is about what happens when a backend refuses, since that is
// the path nobody exercises until production does.

// fakeBackend answers with whatever it is told to answer with, and counts how
// many times it was asked.
type fakeBackend struct {
	name  string
	err   error
	value string
	calls int
}

func (f *fakeBackend) Name() string                  { return f.name }
func (f *fakeBackend) Type() string                  { return "fake" }
func (f *fakeBackend) Connect(context.Context) error { return nil }
func (f *fakeBackend) Close(context.Context) error   { return nil }
func (f *fakeBackend) Health(context.Context) error  { return f.err }

func (f *fakeBackend) Read(context.Context, connector.Query) (*connector.Result, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &connector.Result{Rows: []map[string]interface{}{{"from": f.value}}}, nil
}

func (f *fakeBackend) Write(context.Context, *connector.Data) (*connector.Result, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &connector.Result{Affected: 1}, nil
}

func (f *fakeBackend) Call(context.Context, string, map[string]interface{}) (interface{}, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return map[string]interface{}{"from": f.value}, nil
}

// refusal is an error that says the request itself was the problem, which is
// what an HTTP 4xx or a failed validation produces.
type refusal struct{ msg string }

func (r refusal) Error() string     { return r.msg }
func (r refusal) IsPermanent() bool { return true }

func profiled(t *testing.T, cfg *Config, backends map[string]*fakeBackend) *ProfiledConnector {
	t.Helper()

	cfg.Profiles = map[string]*ProfileDef{}
	for name := range backends {
		cfg.Profiles[name] = &ProfileDef{
			Name:            name,
			ConnectorConfig: &connector.Config{Name: name, Type: "fake"},
		}
	}

	p, err := New("prices", cfg, func(c *connector.Config) (connector.Connector, error) {
		return backends[c.Name], nil
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return p
}

func TestTheActiveProfileAnswersAndTheOthersAreNotTouched(t *testing.T) {
	live := &fakeBackend{name: "live", value: "live"}
	backup := &fakeBackend{name: "backup", value: "backup"}
	p := profiled(t, &Config{Default: "live", Fallback: []string{"backup"}},
		map[string]*fakeBackend{"live": live, "backup": backup})

	result, err := p.Read(context.Background(), connector.Query{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := result.Rows[0]["from"]; got != "live" {
		t.Errorf("answered from %v, want the active profile", got)
	}
	if backup.calls != 0 {
		t.Errorf("the fallback was asked %d times although the active profile answered", backup.calls)
	}
}

func TestAnUnavailableBackendFallsThroughToTheNext(t *testing.T) {
	live := &fakeBackend{name: "live", err: errors.New("connection refused")}
	backup := &fakeBackend{name: "backup", value: "backup"}
	p := profiled(t, &Config{Default: "live", Fallback: []string{"backup"}},
		map[string]*fakeBackend{"live": live, "backup": backup})

	result, err := p.Read(context.Background(), connector.Query{})
	if err != nil {
		t.Fatalf("the fallback did not answer: %v", err)
	}
	if got := result.Rows[0]["from"]; got != "backup" {
		t.Errorf("answered from %v, want the fallback", got)
	}

	stats := p.Stats()
	if stats["fallbacks"] == nil {
		t.Error("the fallback was not counted, so nothing would show a backend is down")
	}
}

func TestARefusedRequestIsNotSentToEveryOtherBackend(t *testing.T) {
	// A fallback list answers "this backend is not available". It is the wrong
	// answer to a request the backend understood and refused: repeating a
	// rejected write on each profile in turn repeats a side effect that already
	// failed, for a reason none of them will disagree about.
	live := &fakeBackend{name: "live", err: refusal{"422 unprocessable entity"}}
	backup := &fakeBackend{name: "backup", value: "backup"}
	p := profiled(t, &Config{Default: "live", Fallback: []string{"backup"}},
		map[string]*fakeBackend{"live": live, "backup": backup})

	_, err := p.Write(context.Background(), &connector.Data{})
	if err == nil {
		t.Fatal("a refused write was reported as having succeeded")
	}
	if !strings.Contains(err.Error(), "422") {
		t.Errorf("error = %q, want the backend's own refusal rather than a summary", err)
	}
	if backup.calls != 0 {
		t.Errorf("the refused write was sent to the fallback as well (%d times)", backup.calls)
	}
}

func TestARefusalWrappedInContextIsStillARefusal(t *testing.T) {
	// Errors arrive wrapped by the layers they pass through, which is why the
	// runtime asks about the whole chain rather than the outermost error.
	live := &fakeBackend{name: "live", err: fmt.Errorf("writing to orders: %w", refusal{"400 bad request"})}
	backup := &fakeBackend{name: "backup"}
	p := profiled(t, &Config{Default: "live", Fallback: []string{"backup"}},
		map[string]*fakeBackend{"live": live, "backup": backup})

	if _, err := p.Write(context.Background(), &connector.Data{}); err == nil {
		t.Fatal("a refused write was accepted")
	}
	if backup.calls != 0 {
		t.Error("a wrapped refusal was treated as a backend being unavailable")
	}
}

func TestACallerThatHasGoneAwayIsNotServedTwice(t *testing.T) {
	live := &fakeBackend{name: "live", err: context.Canceled}
	backup := &fakeBackend{name: "backup"}
	p := profiled(t, &Config{Default: "live", Fallback: []string{"backup"}},
		map[string]*fakeBackend{"live": live, "backup": backup})

	if _, err := p.Read(context.Background(), connector.Query{}); err == nil {
		t.Fatal("a cancelled read reported success")
	}
	if backup.calls != 0 {
		t.Error("a cancelled request was retried against the fallback")
	}
}

func TestWhenEveryBackendIsDownTheLastReasonIsCarried(t *testing.T) {
	// Otherwise the operator is told only that everything failed, which names
	// neither the backend nor the cause.
	live := &fakeBackend{name: "live", err: errors.New("connection refused")}
	backup := &fakeBackend{name: "backup", err: errors.New("no route to host")}
	p := profiled(t, &Config{Default: "live", Fallback: []string{"backup"}},
		map[string]*fakeBackend{"live": live, "backup": backup})

	_, err := p.Read(context.Background(), connector.Query{})
	if err == nil {
		t.Fatal("a read answered although every backend was down")
	}
	if !strings.Contains(err.Error(), "no route to host") {
		t.Errorf("error = %q, want the last backend's reason", err)
	}
	if !strings.Contains(err.Error(), "prices") {
		t.Errorf("error = %q, want it to name the connector", err)
	}
	if live.calls != 1 || backup.calls != 1 {
		t.Errorf("calls: live=%d backup=%d, want each tried once", live.calls, backup.calls)
	}
}

func TestAProfileThatNeverConnectedIsSkipped(t *testing.T) {
	// A backend that was down at startup must not make every later request
	// fail; that is the whole reason the others are listed.
	live := &fakeBackend{name: "live", value: "live"}
	p, err := New("prices", &Config{
		Default:  "live",
		Fallback: []string{"backup"},
		Profiles: map[string]*ProfileDef{
			"live":   {Name: "live", ConnectorConfig: &connector.Config{Name: "live"}},
			"backup": {Name: "backup", ConnectorConfig: &connector.Config{Name: "backup"}},
		},
	}, func(c *connector.Config) (connector.Connector, error) {
		if c.Name == "backup" {
			return nil, errors.New("cannot build the backup")
		}
		return live, nil
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.Connect(context.Background()); err == nil {
		t.Log("a profile that cannot be built is reported at startup")
	}

	// The active profile alone still serves.
	p2 := profiled(t, &Config{Default: "live", Fallback: []string{"backup"}},
		map[string]*fakeBackend{"live": live})
	if _, err := p2.Read(context.Background(), connector.Query{}); err != nil {
		t.Errorf("a read failed although the active profile was up: %v", err)
	}
}

func TestAnActiveProfileThatIsDownAtStartupIsReported(t *testing.T) {
	// Starting anyway would produce a service that answers nothing and says
	// nothing about why.
	backend := &fakeBackend{name: "live"}
	p, err := New("prices", &Config{
		Default:  "live",
		Profiles: map[string]*ProfileDef{"live": {Name: "live", ConnectorConfig: &connector.Config{Name: "live"}}},
	}, func(*connector.Config) (connector.Connector, error) {
		return &failingConnector{fakeBackend: backend}, nil
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = p.Connect(context.Background())
	if err == nil {
		t.Fatal("a connector whose only profile is down started")
	}
	if !strings.Contains(err.Error(), "live") {
		t.Errorf("error = %q, want it to name the profile", err)
	}
}

type failingConnector struct{ *fakeBackend }

func (f *failingConnector) Connect(context.Context) error {
	return errors.New("connection refused")
}

func TestABackendThatCannotDoTheOperationIsNamed(t *testing.T) {
	// Profiles can be of different kinds, and one that cannot read is a
	// configuration mistake worth naming rather than a generic failure.
	p := profiled(t, &Config{Default: "live"},
		map[string]*fakeBackend{"live": {name: "live"}})

	if _, err := p.Call(context.Background(), "op", nil); err != nil {
		t.Fatalf("Call: %v", err)
	}

	// A connector with none of the operation interfaces.
	bare, err := New("prices", &Config{
		Default:  "live",
		Profiles: map[string]*ProfileDef{"live": {Name: "live", ConnectorConfig: &connector.Config{Name: "live"}}},
	}, func(*connector.Config) (connector.Connector, error) {
		return &bareConnector{}, nil
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := bare.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	for name, call := range map[string]func() error{
		"read":  func() error { _, err := bare.Read(context.Background(), connector.Query{}); return err },
		"write": func() error { _, err := bare.Write(context.Background(), &connector.Data{}); return err },
		"call":  func() error { _, err := bare.Call(context.Background(), "op", nil); return err },
	} {
		err := call()
		if err == nil {
			t.Errorf("%s: a backend that cannot do it reported success", name)
			continue
		}
		if !strings.Contains(err.Error(), "live") {
			t.Errorf("%s: error = %q, want it to name the profile", name, err)
		}
	}
}

type bareConnector struct{}

func (bareConnector) Name() string                  { return "live" }
func (bareConnector) Type() string                  { return "bare" }
func (bareConnector) Connect(context.Context) error { return nil }
func (bareConnector) Close(context.Context) error   { return nil }
func (bareConnector) Health(context.Context) error  { return nil }

func TestStatsCountWhatEachProfileDid(t *testing.T) {
	// This is what tells an operator that the primary has been failing over to
	// the backup all week while every request still succeeded.
	live := &fakeBackend{name: "live", err: errors.New("connection refused")}
	backup := &fakeBackend{name: "backup", value: "backup"}
	p := profiled(t, &Config{Default: "live", Fallback: []string{"backup"}},
		map[string]*fakeBackend{"live": live, "backup": backup})

	for i := 0; i < 3; i++ {
		if _, err := p.Read(context.Background(), connector.Query{}); err != nil {
			t.Fatalf("Read: %v", err)
		}
	}

	stats := p.Stats()
	if stats["active_profile"] != "live" {
		t.Errorf("active = %v", stats["active_profile"])
	}

	requests, _ := stats["requests"].(map[string]int64)
	if requests["live"] != 3 || requests["backup"] != 3 {
		t.Errorf("requests = %v, want three against each", requests)
	}
	errorCount, _ := stats["errors"].(map[string]int64)
	if errorCount["live"] != 3 {
		t.Errorf("errors = %v, want the failing backend's three", errorCount)
	}
	if errorCount["backup"] != 0 {
		t.Errorf("the answering backend was counted as failing: %v", errorCount)
	}
	fallbacks, _ := stats["fallbacks"].(map[string]int64)
	if fallbacks["live->backup"] != 3 {
		t.Errorf("fallbacks = %v, want three from live to backup", fallbacks)
	}

	// The counters handed out must not be the live ones, or a reader could
	// change them.
	requests["live"] = 999
	if again, _ := p.Stats()["requests"].(map[string]int64); again["live"] != 3 {
		t.Error("the statistics handed out are the connector's own maps")
	}
}

func TestHealthFollowsTheActiveProfile(t *testing.T) {
	live := &fakeBackend{name: "live"}
	p := profiled(t, &Config{Default: "live"}, map[string]*fakeBackend{"live": live})

	if err := p.Health(context.Background()); err != nil {
		t.Errorf("a working profile reported unhealthy: %v", err)
	}

	live.err = errors.New("connection refused")
	if err := p.Health(context.Background()); err == nil {
		t.Error("a failing profile reported healthy")
	}
}

func TestAConnectorWithNoWayToChooseIsRefused(t *testing.T) {
	// Neither a default nor an expression means there is no answer to "which
	// backend", and every request would fail at the point of use.
	_, err := New("prices", &Config{}, nil)
	if err == nil {
		t.Fatal("a profiled connector with no default and no select was accepted")
	}
	if !strings.Contains(err.Error(), "select") || !strings.Contains(err.Error(), "default") {
		t.Errorf("error = %q, want it to name both ways of choosing", err)
	}
}

func TestADefaultNamingAProfileThatDoesNotExistIsReported(t *testing.T) {
	p, err := New("prices", &Config{
		Default:  "typo",
		Profiles: map[string]*ProfileDef{"live": {Name: "live", ConnectorConfig: &connector.Config{Name: "live"}}},
	}, func(*connector.Config) (connector.Connector, error) { return &fakeBackend{name: "live"}, nil })
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = p.Connect(context.Background())
	if err == nil {
		t.Fatal("a default naming a profile that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), "typo") {
		t.Errorf("error = %q, want it to name what was written", err)
	}
}
