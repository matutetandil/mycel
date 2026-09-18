package runtime

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/transform"
)

// runtimeForConfig builds a runtime over one configuration file, with its
// connectors registered — the state registerFlows is called in.
func runtimeForConfig(t *testing.T, config string) *Runtime {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "service.mycel"), []byte(strings.TrimSpace(config)), 0o644); err != nil {
		t.Fatalf("writing the configuration: %v", err)
	}

	r, err := New(Options{
		ConfigDir: dir,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err != nil {
		t.Fatalf("building the runtime: %v", err)
	}
	if err := r.initConnectors(context.Background()); err != nil {
		t.Fatalf("registering connectors: %v", err)
	}
	return r
}

// A scheduled flow has no source: the clock triggers it. Reading through the
// from block that is not there crashed the service at startup — in three
// separate places, each found only after fixing the one before — so the example
// demonstrating scheduled jobs could not be started at all, and `mycel validate`
// said the configuration was fine.

func TestAFlowWithNoSourceDoesNotCrashTheAccessors(t *testing.T) {
	// The accessors answer for a flow that has no from block rather than
	// dereferencing it. Called on a nil receiver, which is what the runtime
	// holds for a scheduled flow.
	var from *flow.FromConfig

	if got := from.GetOperation(); got != "" {
		t.Errorf("GetOperation on a flow with no source = %q", got)
	}
	if got := from.GetFormat(); got != "" {
		t.Errorf("GetFormat on a flow with no source = %q", got)
	}
	if got := from.GetConnector(); got != "" {
		t.Errorf("GetConnector on a flow with no source = %q", got)
	}
	if got := from.FilterCondition(); got != "" {
		t.Errorf("FilterCondition on a flow with no source = %q", got)
	}
}

func TestAScheduledFlowRegisters(t *testing.T) {
	config := `
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "` + t.TempDir() + `/jobs.db"
}

flow "nightly_cleanup" {
  when = "0 3 * * *"

  to {
    connector = "db"
    target    = "logs"
    operation = "DELETE"
  }
}`

	r := runtimeForConfig(t, config)
	if err := r.registerFlows(); err != nil {
		t.Fatalf("a scheduled flow could not be registered: %v", err)
	}

	handler, found := r.flows.Get("nightly_cleanup")
	if !found {
		t.Fatal("the scheduled flow was not registered")
	}
	if handler.Source != nil {
		t.Error("a scheduled flow was given a source connector")
	}
}

func TestAFlowWithNeitherSourceNorScheduleIsRefused(t *testing.T) {
	// Nothing would ever run it, and saying so beats starting a service that
	// silently does nothing.
	config := `
connector "db" {
  type     = "database"
  driver   = "sqlite"
  database = "` + t.TempDir() + `/jobs.db"
}

flow "unreachable" {
  to {
    connector = "db"
    target    = "logs"
  }
}`

	r := runtimeForConfig(t, config)
	err := r.registerFlows()
	if err == nil {
		t.Fatal("a flow with no source and no schedule was registered; nothing can trigger it")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("the error does not name the flow: %v", err)
	}
}

// Registering is not running. The three tests above cover a scheduled flow
// being built and accepted; what follows is the tick itself, which nothing had
// ever exercised.
//
// The scheduler calls the handler with a nil input map, and a flow triggered by
// the clock has a nil FromConfig. Both are the ordinary shape of a cron flow,
// and both used to end the process on the first tick: the tracing span added
// in v2.10.0 read h.Config.From.Connector as a field, skipping the nil-safe
// getter. The published examples/scheduled reproduced it — at @every 5m, so
// the crash landed five minutes after anyone stopped watching.
//
// Surviving the tick is only half of it. With no source operation to read the
// intent from, the flow was classified as a read: the example that inserts a
// heartbeat row ran `SELECT * FROM heartbeats` every tick, wrote nothing, and
// reported success.

func scheduledHandler(t *testing.T, to *flow.ToConfig, conn connector.Connector) *FlowHandler {
	t.Helper()
	registry := connector.NewRegistry()
	registry.Replace("sink", conn)

	tr, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	return &FlowHandler{
		// No From at all: what the parser produces for a flow whose only
		// trigger is `when`.
		Config: &flow.Config{
			Name: "health_ping",
			When: "@every 3s",
			To:   to,
			Transform: &flow.TransformConfig{
				Mappings: map[string]string{"status": "'alive'"},
				Order:    []string{"status"},
			},
		},
		Connectors:  registry,
		Dest:        conn,
		Transformer: tr,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestAScheduledFlowSurvivesItsFirstTick(t *testing.T) {
	sink := &recordingWriter{name: "sink"}
	to := &flow.ToConfig{
		Connector:       "sink",
		ConnectorParams: map[string]interface{}{"target": "heartbeats"},
	}

	h := scheduledHandler(t, to, sink)

	// Exactly how the scheduler calls it: no payload at all.
	if _, err := h.HandleRequest(context.Background(), nil); err != nil {
		t.Fatalf("a scheduled tick failed: %v", err)
	}
}

func TestAScheduledFlowWritesRatherThanReads(t *testing.T) {
	sink := &recordingWriter{name: "sink"}
	to := &flow.ToConfig{
		Connector:       "sink",
		ConnectorParams: map[string]interface{}{"target": "heartbeats"},
	}

	h := scheduledHandler(t, to, sink)
	if _, err := h.HandleRequest(context.Background(), nil); err != nil {
		t.Fatalf("a scheduled tick failed: %v", err)
	}

	written := sink.writes()
	if len(written) != 1 {
		t.Fatalf("a scheduled flow with a destination wrote %d times, want 1 — it was treated as a read", len(written))
	}
	if written[0].Operation != "INSERT" {
		t.Errorf("operation = %q, want INSERT", written[0].Operation)
	}
	if written[0].Target != "heartbeats" {
		t.Errorf("target = %q, want heartbeats", written[0].Target)
	}
	if got := written[0].Payload["status"]; got != "alive" {
		t.Errorf("payload status = %v, want alive — the transform output never reached the destination", got)
	}
}

// A destination that names its own operation keeps it: the cleanup job in the
// published example declares DELETE, and deriving the write intent for a
// scheduled flow must not override what the author asked for.
func TestAScheduledFlowKeepsAnExplicitDestinationOperation(t *testing.T) {
	sink := &recordingWriter{name: "sink"}
	to := &flow.ToConfig{
		Connector: "sink",
		ConnectorParams: map[string]interface{}{
			"target":    "logs",
			"operation": "DELETE",
		},
	}

	h := scheduledHandler(t, to, sink)
	if _, err := h.HandleRequest(context.Background(), nil); err != nil {
		t.Fatalf("a scheduled tick failed: %v", err)
	}

	written := sink.writes()
	if len(written) != 1 {
		t.Fatalf("wrote %d times, want 1", len(written))
	}
	if written[0].Operation != "DELETE" {
		t.Errorf("operation = %q, want DELETE", written[0].Operation)
	}
}

// The guard that makes the fix stick.
//
// FromConfig answers every getter for a nil receiver, and the comment above
// them records the three crashes that led to them. The fourth arrived anyway,
// because a patch read the struct field directly and skipped the getter. Field
// access is what reintroduces this, so this reads the handler's own source and
// refuses it.
func TestTheFlowHandlerReadsItsSourceThroughNilSafeGetters(t *testing.T) {
	const file = "flow_registry.go"

	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}

	// h.Config.From.<Field> — an exported identifier that is not a call.
	// Getters (GetConnector(), GetOperation(), GetFilterConfig(), …) are
	// followed by "(", so they do not match.
	direct := regexp.MustCompile(`h\.Config\.From\.([A-Z]\w*)([^(\w]|$)`)

	var offenders []string
	for i, line := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		for _, m := range direct.FindAllStringSubmatch(line, -1) {
			offenders = append(offenders, "line "+strconv.Itoa(i+1)+": .From."+m[1]+"  →  "+trimmed)
		}
	}

	if len(offenders) > 0 {
		t.Errorf("%s reads h.Config.From fields directly, which panics for a scheduled flow "+
			"(it has no from block). Use the nil-safe getters on FromConfig:\n  %s",
			file, strings.Join(offenders, "\n  "))
	}
}
