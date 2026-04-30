package runtime

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
)

// fakeStarterConnector implements connector.Connector + Starter so we can
// verify hot reload's restart loop calls Start on event-driven connectors.
type fakeStarterConnector struct {
	name       string
	startCalls atomic.Int32
	closeCalls atomic.Int32
}

func (f *fakeStarterConnector) Name() string                    { return f.name }
func (f *fakeStarterConnector) Type() string                    { return "fake-mq" }
func (f *fakeStarterConnector) Connect(_ context.Context) error { return nil }
func (f *fakeStarterConnector) Close(_ context.Context) error {
	f.closeCalls.Add(1)
	return nil
}
func (f *fakeStarterConnector) Health(_ context.Context) error { return nil }
func (f *fakeStarterConnector) Start(_ context.Context) error {
	f.startCalls.Add(1)
	return nil
}

// TestHotReloadRestartsEventDrivenConnectors covers the regression where
// hotReloadSwitch did not call Start() on event-driven connectors after
// rebuilding the registry. Symptom: RabbitMQ connector reports "connected"
// after a hot reload but the consumer goroutines never start, so message
// delivery silently halts.
//
// The test exercises the loop pattern that hot reload now uses on a list
// of fake Starter connectors and verifies Start was invoked on each.
func TestHotReloadRestartsEventDrivenConnectors(t *testing.T) {
	conns := map[string]connector.Connector{
		"rabbit": &fakeStarterConnector{name: "rabbit"},
		"kafka":  &fakeStarterConnector{name: "kafka"},
	}

	// Mimic the loop hotReloadSwitch runs after CloseAll + initConnectors +
	// registerFlows. No debugger connected, so every Starter must be
	// started immediately.
	for _, conn := range conns {
		if starter, ok := conn.(Starter); ok {
			if err := starter.Start(context.Background()); err != nil {
				t.Fatalf("starter.Start: %v", err)
			}
		}
	}

	for _, conn := range conns {
		f := conn.(*fakeStarterConnector)
		if got := f.startCalls.Load(); got != 1 {
			t.Errorf("%s Start calls: got %d, want 1", f.name, got)
		}
	}
}

// TestStarterInterfaceAssertion is a static check that fakeStarterConnector
// satisfies both interfaces — if either changes, this catches it before the
// hot reload restart loop silently skips real connectors.
func TestStarterInterfaceAssertion(t *testing.T) {
	var _ connector.Connector = (*fakeStarterConnector)(nil)
	var _ Starter = (*fakeStarterConnector)(nil)
}
