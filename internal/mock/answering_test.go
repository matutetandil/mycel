package mock

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Answering from recorded files instead of the real service.
//
// The loader was covered and the connector that stands in front of a real one
// was not — which is the half that decides whether a flow talks to a recording
// or to production. Getting that wrong in the wrong direction is a test suite
// writing to a live system.

// recordings writes a mock directory the way a project holds one.
func recordings(t *testing.T, files map[string]interface{}) *Loader {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(path, encoded, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return NewLoader(dir)
}

// refusing is a connector that fails anything asked of it, so a test can tell
// a recorded answer from a real one.
type refusing struct{ reached bool }

func (r *refusing) Name() string                  { return "store" }
func (r *refusing) Type() string                  { return "database" }
func (r *refusing) Connect(context.Context) error { return nil }
func (r *refusing) Close(context.Context) error   { return nil }
func (r *refusing) Health(context.Context) error  { return nil }

func (r *refusing) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	r.reached = true
	return &connector.Result{Rows: []map[string]interface{}{{"from": "the real service"}}}, nil
}

func (r *refusing) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	r.reached = true
	return &connector.Result{Affected: 99}, nil
}

func TestARecordedAnswerIsUsedInsteadOfTheRealService(t *testing.T) {
	loader := recordings(t, map[string]interface{}{
		"connectors/store/orders.json": map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{"id": "order-1", "status": "paid"},
			},
		},
	})

	real := &refusing{}
	c, err := NewConnector("store", real, loader, &ConnectorMockConfig{}, false)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	result, err := c.Read(context.Background(), connector.Query{Target: "orders"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["id"] != "order-1" {
		t.Errorf("rows = %v, want the recording", result.Rows)
	}
	if real.reached {
		t.Error("the real service was called although a recording exists")
	}
}

func TestWithNoRecordingTheRealServiceAnswers(t *testing.T) {
	// Mocking one target and leaving the rest real is how somebody isolates
	// the part they are working on.
	loader := recordings(t, map[string]interface{}{
		"connectors/store/orders.json": map[string]interface{}{"data": []interface{}{}},
	})

	real := &refusing{}
	c, err := NewConnector("store", real, loader, &ConnectorMockConfig{}, false)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	result, err := c.Read(context.Background(), connector.Query{Target: "customers"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !real.reached {
		t.Error("a target with no recording did not reach the real service")
	}
	if len(result.Rows) != 1 || result.Rows[0]["from"] != "the real service" {
		t.Errorf("rows = %v", result.Rows)
	}
}

func TestWithNothingRealBehindItAMissingRecordingIsEmpty(t *testing.T) {
	// mock_only: there is no real service to fall through to. A target nobody
	// recorded answers with nothing rather than an error — a project mocks
	// what it needs and the rest is simply empty. Whether the connector has
	// any recordings at all is reported when it is configured, which is where
	// a typo can still be caught.
	loader := recordings(t, map[string]interface{}{})

	c, err := NewConnector("store", nil, loader, &ConnectorMockConfig{}, true)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	result, err := c.Read(context.Background(), connector.Query{Target: "orders"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Errorf("rows = %v, want none", result.Rows)
	}
}

func TestARecordingCanAnswerDifferentlyPerRequest(t *testing.T) {
	// The reason recordings are more than fixtures: a flow's branches are
	// exercised by making the same call answer differently.
	loader := recordings(t, map[string]interface{}{
		"connectors/store/orders.json": map[string]interface{}{
			"responses": []interface{}{
				map[string]interface{}{
					"when": `input.status == "paid"`,
					"data": []interface{}{map[string]interface{}{"id": "order-1", "status": "paid"}},
				},
				map[string]interface{}{
					"when":  `input.status == "cancelled"`,
					"error": "this order was cancelled",
				},
				map[string]interface{}{
					"default": true,
					"data":    []interface{}{},
				},
			},
		},
	})

	c, err := NewConnector("store", &refusing{}, loader, &ConnectorMockConfig{}, true)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}
	ctx := context.Background()

	result, err := c.Read(ctx, connector.Query{
		Target: "orders", Filters: map[string]interface{}{"status": "paid"},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Errorf("rows = %v, want the answer for a paid order", result.Rows)
	}

	// A recorded failure, which is how a flow's error handling is exercised
	// without arranging for the real service to fail.
	_, err = c.Read(ctx, connector.Query{
		Target: "orders", Filters: map[string]interface{}{"status": "cancelled"},
	})
	if err == nil {
		t.Error("a recorded failure was answered as a success")
	} else if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("error = %q, want what the recording said", err)
	}

	// And anything else falls to the default rather than to the real service.
	result, err = c.Read(ctx, connector.Query{
		Target: "orders", Filters: map[string]interface{}{"status": "pending"},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Errorf("rows = %v, want the default answer", result.Rows)
	}
}

func TestARecordingCanAnswerSlowly(t *testing.T) {
	// What makes a mock worth more than a fixture: a flow's timeout is only
	// exercised by something that takes time.
	loader := recordings(t, map[string]interface{}{
		"connectors/store/orders.json": map[string]interface{}{
			"responses": []interface{}{
				map[string]interface{}{"default": true, "delay": "60ms", "data": []interface{}{}},
			},
		},
	})

	c, err := NewConnector("store", &refusing{}, loader, &ConnectorMockConfig{}, true)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	started := time.Now()
	if _, err := c.Read(context.Background(), connector.Query{Target: "orders"}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if time.Since(started) < 50*time.Millisecond {
		t.Error("the recorded delay was not waited for, so no timeout can be exercised")
	}
}

func TestAWriteIsRecordedToo(t *testing.T) {
	// Otherwise a suite that mocks its reads still writes to a live system.
	loader := recordings(t, map[string]interface{}{
		"connectors/store/orders.json": map[string]interface{}{"affected": 1},
	})

	real := &refusing{}
	c, err := NewConnector("store", real, loader, &ConnectorMockConfig{}, false)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	result, err := c.Write(context.Background(), &connector.Data{
		Target: "orders", Payload: map[string]interface{}{"id": "order-1"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if real.reached {
		t.Error("the write reached the real service although a recording exists")
	}
	if result.Affected != 1 {
		t.Errorf("affected = %d, want what the recording said", result.Affected)
	}
}

func TestTheConnectorStillLooksLikeTheOneItStandsInFor(t *testing.T) {
	// A flow names a connector and the runtime looks it up by name and type,
	// so standing in front of one must not change either.
	loader := recordings(t, map[string]interface{}{})

	c, err := NewConnector("store", &refusing{}, loader, &ConnectorMockConfig{}, false)
	if err != nil {
		t.Fatalf("NewConnector: %v", err)
	}

	if c.Name() != "store" || c.Type() != "database" {
		t.Errorf("name = %q, type = %q", c.Name(), c.Type())
	}
	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Errorf("Connect: %v", err)
	}
	if err := c.Health(ctx); err != nil {
		t.Errorf("Health: %v", err)
	}
	if err := c.Close(ctx); err != nil {
		t.Errorf("Close: %v", err)
	}
	// The real one is reachable, which is what lets the runtime unwrap it for
	// anything the stand-in does not implement.
	if c.Unwrap() == nil {
		t.Error("the connector it stands in for cannot be reached")
	}
}

func TestAnOperationOfItsOwnIsRecordedByItsName(t *testing.T) {
	// A REST call recorded under the method and path, which is what a mocked
	// third-party API looks like.
	loader := recordings(t, map[string]interface{}{
		"connectors/api/GET_users.json": map[string]interface{}{
			"data": []interface{}{map[string]interface{}{"id": "u-1"}},
		},
	})

	mock, err := loader.LoadOperationMock("api", "get", "/users")
	if err != nil {
		t.Fatalf("LoadOperationMock: %v", err)
	}
	if mock == nil {
		t.Fatal("the recording was not found under the name the loader builds")
	}

	// And a whole flow's answer, recorded under the flow's own name.
	flowLoader := recordings(t, map[string]interface{}{
		"flows/list_orders.json": map[string]interface{}{"data": []interface{}{}},
	})
	flowMock, err := flowLoader.LoadFlowMock("list_orders")
	if err != nil {
		t.Fatalf("LoadFlowMock: %v", err)
	}
	if flowMock == nil {
		t.Fatal("a flow's recording was not found")
	}

	// A name nobody recorded is nothing rather than an error, so a project
	// mocks what it wants and leaves the rest alone.
	missing, err := flowLoader.LoadFlowMock("a_flow_nobody_recorded")
	if err != nil || missing != nil {
		t.Errorf("mock = %v, err = %v, want nothing at all", missing, err)
	}
}

func TestRecordingsAreReadFromDiskAgainAfterTheCacheIsCleared(t *testing.T) {
	// Hot reload: somebody edits a recording while the service runs.
	dir := t.TempDir()
	path := filepath.Join(dir, "connectors", "store", "orders.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"affected": 1}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loader := NewLoader(dir)
	first, err := loader.LoadConnectorMock("store", "orders")
	if err != nil || first.Affected != 1 {
		t.Fatalf("mock = %+v, err = %v", first, err)
	}

	if err := os.WriteFile(path, []byte(`{"affected": 7}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	loader.ClearCache()

	second, err := loader.LoadConnectorMock("store", "orders")
	if err != nil {
		t.Fatalf("LoadConnectorMock: %v", err)
	}
	if second.Affected != 7 {
		t.Errorf("affected = %d, want the edited recording", second.Affected)
	}
}
