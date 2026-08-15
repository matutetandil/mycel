package connector

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
)

// The registry every connector in a service passes through.
//
// It is what turns a configuration block into a running connector, hands one
// to a flow that names it, connects them all at start-up and checks their
// health afterwards. Almost none of it was exercised — the closing was, after
// a shutdown that outlived its grace period, and nothing else.

// stubConnector is a connector that does as it is told, so a test can say what
// happens rather than arrange for it.
type stubConnector struct {
	name        string
	connectErr  error
	healthErr   error
	connected   bool
	readable    bool
	writable    bool
	lastWritten *Data
}

func (s *stubConnector) Name() string { return s.name }
func (s *stubConnector) Type() string { return "stub" }
func (s *stubConnector) Connect(context.Context) error {
	if s.connectErr != nil {
		return s.connectErr
	}
	s.connected = true
	return nil
}
func (s *stubConnector) Health(context.Context) error { return s.healthErr }
func (s *stubConnector) Close(context.Context) error  { return nil }

// readableConnector and writableConnector are the two halves a flow asks for:
// a source it can read and a destination it can write.
type readableConnector struct{ *stubConnector }

func (r readableConnector) Read(ctx context.Context, query Query) (*Result, error) {
	return &Result{Rows: []map[string]interface{}{{"id": "order-1"}}}, nil
}

type writableConnector struct{ *stubConnector }

func (w writableConnector) Write(ctx context.Context, data *Data) (*Result, error) {
	w.lastWritten = data
	return &Result{Affected: 1}, nil
}

// stubFactory builds whatever it was given, for one connector type.
type stubFactory struct {
	connType string
	build    func(*Config) Connector
	err      error
}

func (f stubFactory) Supports(connType, driver string) bool { return connType == f.connType }
func (f stubFactory) Create(ctx context.Context, cfg *Config) (Connector, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.build(cfg), nil
}

func registryWith(factories ...Factory) *Registry {
	registry := NewRegistry()
	for _, factory := range factories {
		registry.RegisterFactory(factory)
	}
	return registry
}

func TestAConfigurationBecomesARunningConnector(t *testing.T) {
	registry := registryWith(stubFactory{
		connType: "stub",
		build:    func(cfg *Config) Connector { return &stubConnector{name: cfg.Name} },
	})
	ctx := context.Background()

	if err := registry.Register(ctx, &Config{Name: "orders_db", Type: "stub"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	found, err := registry.Get("orders_db")
	if err != nil || found.Name() != "orders_db" {
		t.Fatalf("Get: %v, %v", found, err)
	}

	// Two connectors under one name is a flow writing to whichever was
	// registered last, so it is refused rather than resolved.
	err = registry.Register(ctx, &Config{Name: "orders_db", Type: "stub"})
	if err == nil {
		t.Error("a second connector took the first one's name")
	}

	// And a name nobody registered says so — this is what a flow naming a
	// connector that does not exist gets.
	if _, err := registry.Get("nothing"); err == nil {
		t.Error("a connector nobody registered was handed over")
	} else if !strings.Contains(err.Error(), "nothing") {
		t.Errorf("the error does not name the connector: %v", err)
	}
}

func TestAConnectorTypeNothingCanBuild(t *testing.T) {
	// The failure a configuration naming a type nobody implements gets — a
	// plugin that was not installed, or a typo in the type.
	registry := registryWith(stubFactory{connType: "stub"})

	_, err := registry.Create(context.Background(), &Config{Name: "sf", Type: "salesforce", Driver: "v58"})
	if err == nil {
		t.Fatal("a connector of a type nobody implements was built")
	}
	for _, want := range []string{"salesforce", "v58"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}

	// Supports answers the same question without building anything.
	if registry.Supports("salesforce", "") {
		t.Error("the registry claims it can build a type it cannot")
	}
	if !registry.Supports("stub", "") {
		t.Error("the registry denies a type it can build")
	}
}

func TestAFactoryThatRefusesToBuild(t *testing.T) {
	// Bad credentials, an address that will not parse: the connector is never
	// registered, and the error names which one it was.
	registry := registryWith(stubFactory{connType: "stub", err: errors.New("port must be a number")})

	err := registry.Register(context.Background(), &Config{Name: "orders_db", Type: "stub"})
	if err == nil {
		t.Fatal("a connector that could not be built was registered")
	}
	if !strings.Contains(err.Error(), "orders_db") || !strings.Contains(err.Error(), "port must be a number") {
		t.Errorf("the error does not say what failed or why: %v", err)
	}
	if _, err := registry.Get("orders_db"); err == nil {
		t.Error("a connector that failed to build is still registered")
	}
}

func TestAskingForAConnectorThatCanDoWhatTheFlowNeeds(t *testing.T) {
	// A flow reading from a destination-only connector, or writing to a
	// source-only one, is a configuration mistake — and it has to be reported
	// as that rather than as a nil dereference somewhere downstream.
	registry := registryWith(
		stubFactory{connType: "reader", build: func(cfg *Config) Connector {
			return readableConnector{&stubConnector{name: cfg.Name}}
		}},
		stubFactory{connType: "writer", build: func(cfg *Config) Connector {
			return writableConnector{&stubConnector{name: cfg.Name}}
		}},
		stubFactory{connType: "neither", build: func(cfg *Config) Connector {
			return &stubConnector{name: cfg.Name}
		}},
	)
	ctx := context.Background()

	for _, cfg := range []*Config{
		{Name: "source", Type: "reader"},
		{Name: "destination", Type: "writer"},
		{Name: "inert", Type: "neither"},
	} {
		if err := registry.Register(ctx, cfg); err != nil {
			t.Fatalf("Register %s: %v", cfg.Name, err)
		}
	}

	if _, err := registry.GetReader("source"); err != nil {
		t.Errorf("a readable connector was refused as a reader: %v", err)
	}
	if _, err := registry.GetWriter("destination"); err != nil {
		t.Errorf("a writable connector was refused as a writer: %v", err)
	}

	if _, err := registry.GetReader("inert"); err == nil {
		t.Error("a connector that cannot be read from was handed over as a source")
	} else if !strings.Contains(err.Error(), "reading") {
		t.Errorf("the error does not say what it cannot do: %v", err)
	}
	if _, err := registry.GetWriter("inert"); err == nil {
		t.Error("a connector that cannot be written to was handed over as a destination")
	}

	// A name nobody registered fails the same way through either door.
	if _, err := registry.GetReader("nothing"); err == nil {
		t.Error("a reader nobody registered was handed over")
	}
	if _, err := registry.GetWriter("nothing"); err == nil {
		t.Error("a writer nobody registered was handed over")
	}
}

func TestStartingEverySystemTheServiceTalksTo(t *testing.T) {
	// Connecting is where a wrong password or an unreachable host is found,
	// and it has to stop the service starting: a connector that never
	// connected fails on the first message instead, in the middle of a flow.
	registry := registryWith(stubFactory{connType: "stub", build: func(cfg *Config) Connector {
		return &stubConnector{name: cfg.Name}
	}})
	ctx := context.Background()

	for _, name := range []string{"orders_db", "queue", "api"} {
		if err := registry.Register(ctx, &Config{Name: name, Type: "stub"}); err != nil {
			t.Fatalf("Register %s: %v", name, err)
		}
	}

	if err := registry.ConnectAll(ctx); err != nil {
		t.Fatalf("ConnectAll: %v", err)
	}
	for _, name := range registry.List() {
		conn, _ := registry.Get(name)
		if !conn.(*stubConnector).connected {
			t.Errorf("%s was never connected", name)
		}
	}

	// One that cannot connect stops the lot, and says which.
	registry.Replace("queue", &stubConnector{name: "queue", connectErr: errors.New("connection refused")})
	err := registry.ConnectAll(ctx)
	if err == nil {
		t.Fatal("the service started with a connector that could not connect")
	}
	if !strings.Contains(err.Error(), "queue") {
		t.Errorf("the error does not name the connector: %v", err)
	}
}

func TestAskingEverySystemWhetherItIsStillThere(t *testing.T) {
	// What /health answers with. Every connector is asked, including the ones
	// that are fine — a health check that stops at the first failure hides
	// the second.
	registry := registryWith(stubFactory{connType: "stub", build: func(cfg *Config) Connector {
		return &stubConnector{name: cfg.Name}
	}})
	ctx := context.Background()

	for _, name := range []string{"orders_db", "queue", "api"} {
		_ = registry.Register(ctx, &Config{Name: name, Type: "stub"})
	}
	registry.Replace("queue", &stubConnector{name: "queue", healthErr: errors.New("no connection")})
	registry.Replace("api", &stubConnector{name: "api", healthErr: errors.New("502 from upstream")})

	results := registry.HealthCheckAll(ctx)

	if len(results) != 3 {
		t.Fatalf("checked %d connectors, want all three", len(results))
	}
	if results["orders_db"] != nil {
		t.Errorf("a healthy connector was reported unhealthy: %v", results["orders_db"])
	}
	if results["queue"] == nil || results["api"] == nil {
		t.Error("a failing connector was reported healthy")
	}
}

func TestWhatTheServiceSaysItIsConnectedTo(t *testing.T) {
	registry := registryWith(stubFactory{connType: "stub", build: func(cfg *Config) Connector {
		return &stubConnector{name: cfg.Name}
	}})
	ctx := context.Background()

	if len(registry.List()) != 0 {
		t.Errorf("a new registry already holds %v", registry.List())
	}

	for _, name := range []string{"orders_db", "queue"} {
		_ = registry.Register(ctx, &Config{Name: name, Type: "stub"})
	}

	names := registry.List()
	sort.Strings(names)
	if len(names) != 2 || names[0] != "orders_db" || names[1] != "queue" {
		t.Errorf("names = %v", names)
	}

	// Names is the same list under another name, and both are read by the
	// banner and the admin server.
	also := registry.Names()
	sort.Strings(also)
	if len(also) != len(names) {
		t.Errorf("List and Names disagree: %v vs %v", names, also)
	}

	// Replacing is how the mock system wraps a connector: same name, and
	// nothing else moves.
	registry.Replace("queue", &stubConnector{name: "queue-mock"})
	replaced, err := registry.Get("queue")
	if err != nil || replaced.Name() != "queue-mock" {
		t.Errorf("replacement = %v, %v", replaced, err)
	}
	if len(registry.List()) != 2 {
		t.Errorf("replacing changed how many connectors there are: %v", registry.List())
	}
}
