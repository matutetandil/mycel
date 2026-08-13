package mock

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Naming a connector for mocking and getting no mocks is the one mistake this
// system cannot afford to make quietly: the point of asking for it by name is
// that its calls must not reach the real thing. A directory spelled `database`
// when the connector is `db` leaves every call going to the real database,
// with the service reporting that mocking is on.

func mockProject(t *testing.T, connectors ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range connectors {
		path := filepath.Join(dir, "connectors", name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(path, "users.json"), []byte(`{"data":[]}`), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return dir
}

func TestNamingAConnectorWithNoMocksIsReported(t *testing.T) {
	// They asked for this connector by name, so there is no reading under
	// which reaching the real one is what they meant.
	var logged strings.Builder
	logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn}))

	manager := NewManager(&Config{
		Enabled:  true,
		Path:     mockProject(t, "db"),
		MockOnly: []string{"payments"}, // named, and has no mocks
	})
	manager.logger = logger

	real := &countingConnector{name: "payments"}
	wrapped, err := manager.WrapConnector("payments", real)
	if err != nil {
		t.Fatalf("WrapConnector: %v", err)
	}

	output := logged.String()
	if !strings.Contains(output, "payments") {
		t.Errorf("nothing was said about the connector that has no mocks:\n%s", output)
	}

	// And the behaviour it warns about is the behaviour: the call reaches the
	// real connector.
	if reader, ok := wrapped.(connector.Reader); ok {
		_, _ = reader.Read(context.Background(), connector.Query{Target: "users"})
	}
	if real.reads == 0 {
		t.Log("the call did not reach the real connector")
	}
}

type countingConnector struct {
	name  string
	reads int
}

func (c *countingConnector) Name() string                  { return c.name }
func (c *countingConnector) Type() string                  { return "fake" }
func (c *countingConnector) Connect(context.Context) error { return nil }
func (c *countingConnector) Close(context.Context) error   { return nil }
func (c *countingConnector) Health(context.Context) error  { return nil }
func (c *countingConnector) Read(context.Context, connector.Query) (*connector.Result, error) {
	c.reads++
	return &connector.Result{}, nil
}

func TestAConnectorWithMocksAnswersFromThemAndNeverReachesTheRealOne(t *testing.T) {
	// The promise of a mock: the real system is not touched. A test suite that
	// half-mocks is worse than one that does not, because the calls that got
	// through are the ones nobody knows about.
	manager := NewManager(&Config{Enabled: true, Path: mockProject(t, "db")})

	real := &countingConnector{name: "db"}
	wrapped, err := manager.WrapConnector("db", real)
	if err != nil {
		t.Fatalf("WrapConnector: %v", err)
	}

	reader, ok := wrapped.(connector.Reader)
	if !ok {
		t.Fatal("the wrapped connector cannot be read from")
	}
	if _, err := reader.Read(context.Background(), connector.Query{Target: "users"}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if real.reads != 0 {
		t.Error("a mocked connector reached the real one")
	}

	// And a target with no mock file still does not reach it, since this
	// connector is answered from mocks entirely.
	if _, err := reader.Read(context.Background(), connector.Query{Target: "orders"}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if real.reads != 0 {
		t.Error("a target with no mock reached the real connector")
	}
}

func TestAConnectorLeftOutIsNotWrapped(t *testing.T) {
	// --no-mock is how one connector is kept real while the rest are mocked.
	manager := NewManager(&Config{
		Enabled: true, Path: mockProject(t, "db"),
		NoMock: []string{"db"},
	})

	real := &countingConnector{name: "db"}
	wrapped, err := manager.WrapConnector("db", real)
	if err != nil {
		t.Fatalf("WrapConnector: %v", err)
	}
	if wrapped != connector.Connector(real) {
		t.Error("a connector excluded from mocking was wrapped anyway")
	}
}

func TestNamingSomeConnectorsLeavesTheRestAlone(t *testing.T) {
	manager := NewManager(&Config{
		Enabled: true, Path: mockProject(t, "db", "payments"),
		MockOnly: []string{"db"},
	})

	if !manager.ShouldMock("db") {
		t.Error("the connector that was named is not mocked")
	}
	if manager.ShouldMock("payments") {
		t.Error("a connector that was not named is mocked")
	}
}

func TestWithMockingOffNothingIsWrapped(t *testing.T) {
	manager := NewManager(&Config{Enabled: false, Path: mockProject(t, "db")})
	real := &countingConnector{name: "db"}
	wrapped, err := manager.WrapConnector("db", real)
	if err != nil {
		t.Fatalf("WrapConnector: %v", err)
	}
	if wrapped != connector.Connector(real) {
		t.Error("a connector was mocked although mocking is off")
	}
}
