package debug

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/trace"
	"github.com/matutetandil/mycel/internal/transform"
	"github.com/matutetandil/mycel/internal/validate"
)

// --- Mock RuntimeInspector ---

type mockInspector struct {
	flows      map[string]*flow.Config
	connectors map[string]*connector.Config
	types      []*validate.TypeSchema
	transforms []*transform.Config
}

func newMockInspector() *mockInspector {
	return &mockInspector{
		flows: map[string]*flow.Config{
			"create_user": {
				Name: "create_user",
				From: &flow.FromConfig{
					Connector:       "api",
					ConnectorParams: map[string]interface{}{"operation": "POST /users"},
				},
				To: &flow.ToConfig{
					Connector:       "postgres",
					ConnectorParams: map[string]interface{}{"operation": "users"},
				},
				Transform: &flow.TransformConfig{
					Mappings: map[string]string{
						"email": "lower(input.email)",
						"name":  "input.name",
					},
				},
			},
			"get_users": {
				Name: "get_users",
				From: &flow.FromConfig{
					Connector:       "api",
					ConnectorParams: map[string]interface{}{"operation": "GET /users"},
				},
				To: &flow.ToConfig{
					Connector:       "postgres",
					ConnectorParams: map[string]interface{}{"operation": "users"},
				},
			},
		},
		connectors: map[string]*connector.Config{
			"api":      {Name: "api", Type: "rest", Driver: ""},
			"postgres": {Name: "postgres", Type: "database", Driver: "postgres"},
		},
		types: []*validate.TypeSchema{
			{
				Name: "user",
				Fields: []validate.FieldSchema{
					{Name: "email", Type: "string", Required: true},
					{Name: "name", Type: "string", Required: true},
					{Name: "age", Type: "number", Required: false},
				},
			},
		},
		transforms: []*transform.Config{
			{
				Name: "normalize_email",
				Mappings: map[string]string{
					"email": "lower(input.email)",
				},
			},
		},
	}
}

func (m *mockInspector) ListFlows() []string {
	names := make([]string, 0, len(m.flows))
	for name := range m.flows {
		names = append(names, name)
	}
	return names
}

func (m *mockInspector) GetFlowConfig(name string) (*flow.Config, bool) {
	cfg, ok := m.flows[name]
	return cfg, ok
}

func (m *mockInspector) ListConnectors() []string {
	names := make([]string, 0, len(m.connectors))
	for name := range m.connectors {
		names = append(names, name)
	}
	return names
}

func (m *mockInspector) GetConnectorConfig(name string) (*connector.Config, bool) {
	cfg, ok := m.connectors[name]
	return cfg, ok
}

func (m *mockInspector) ListTypes() []*validate.TypeSchema {
	return m.types
}

func (m *mockInspector) ListTransforms() []*transform.Config {
	return m.transforms
}

func (m *mockInspector) GetCELTransformer() *transform.CELTransformer {
	t, _ := transform.NewCELTransformer()
	return t
}

// --- Test Helpers ---

func startTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	inspector := newMockInspector()
	srv := NewServer(inspector, nil)
	// Use nil logger — tests don't need logging

	mux := http.NewServeMux()
	srv.RegisterHandlers(mux)
	ts := httptest.NewServer(mux)
	return srv, ts
}

func connectWS(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/debug"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	return conn
}

func sendRequest(t *testing.T, conn *websocket.Conn, id int, method string, params interface{}) {
	t.Helper()
	paramsJSON, _ := json.Marshal(params)
	req := Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf("%d", id)),
		Method:  method,
		Params:  paramsJSON,
	}
	if err := conn.WriteJSON(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
}

func readResponse(t *testing.T, conn *websocket.Conn) *Response {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var resp Response
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	return &resp
}

func readNotification(t *testing.T, conn *websocket.Conn) *Notification {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var n Notification
	if err := conn.ReadJSON(&n); err != nil {
		t.Fatalf("failed to read notification: %v", err)
	}
	return &n
}

// --- Tests ---

func TestAttachDetach(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	// Attach
	sendRequest(t, conn, 1, "debug.attach", &AttachParams{ClientName: "test-ide"})
	resp := readResponse(t, conn)

	if resp.Error != nil {
		t.Fatalf("attach failed: %s", resp.Error.Message)
	}

	var result AttachResult
	b, _ := json.Marshal(resp.Result)
	json.Unmarshal(b, &result)

	if result.SessionID == "" {
		t.Error("expected non-empty session ID")
	}
	if len(result.Flows) == 0 {
		t.Error("expected at least one flow")
	}

	// Detach
	sendRequest(t, conn, 2, "debug.detach", nil)
	resp = readResponse(t, conn)
	if resp.Error != nil {
		t.Fatalf("detach failed: %s", resp.Error.Message)
	}
}

func TestMethodWithoutAttach(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.threads", nil)
	resp := readResponse(t, conn)

	if resp.Error == nil {
		t.Fatal("expected error for method without attach")
	}
	if resp.Error.Code != CodeSessionNotFound {
		t.Errorf("expected code %d, got %d", CodeSessionNotFound, resp.Error.Code)
	}
}

func TestInspectFlows(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	readResponse(t, conn)

	sendRequest(t, conn, 2, "inspect.flows", nil)
	resp := readResponse(t, conn)

	if resp.Error != nil {
		t.Fatalf("inspect.flows failed: %s", resp.Error.Message)
	}

	var flows []*FlowInfo
	b, _ := json.Marshal(resp.Result)
	json.Unmarshal(b, &flows)

	if len(flows) != 2 {
		t.Errorf("expected 2 flows, got %d", len(flows))
	}
}

func TestInspectFlowDetail(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	readResponse(t, conn)

	sendRequest(t, conn, 2, "inspect.flow", &InspectFlowParams{Name: "create_user"})
	resp := readResponse(t, conn)

	if resp.Error != nil {
		t.Fatalf("inspect.flow failed: %s", resp.Error.Message)
	}

	var info FlowInfo
	b, _ := json.Marshal(resp.Result)
	json.Unmarshal(b, &info)

	if info.Name != "create_user" {
		t.Errorf("expected flow name create_user, got %s", info.Name)
	}
	if info.From == nil || info.From.Connector != "api" {
		t.Error("expected from connector api")
	}
	if info.To == nil || info.To.Connector != "postgres" {
		t.Error("expected to connector postgres")
	}
	if len(info.Transform) != 2 {
		t.Errorf("expected 2 transform rules, got %d", len(info.Transform))
	}
}

func TestInspectFlowNotFound(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	readResponse(t, conn)

	sendRequest(t, conn, 2, "inspect.flow", &InspectFlowParams{Name: "nonexistent"})
	resp := readResponse(t, conn)

	if resp.Error == nil {
		t.Fatal("expected error for nonexistent flow")
	}
	if resp.Error.Code != CodeFlowNotFound {
		t.Errorf("expected code %d, got %d", CodeFlowNotFound, resp.Error.Code)
	}
}

func TestInspectConnectors(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	readResponse(t, conn)

	sendRequest(t, conn, 2, "inspect.connectors", nil)
	resp := readResponse(t, conn)

	if resp.Error != nil {
		t.Fatalf("inspect.connectors failed: %s", resp.Error.Message)
	}

	var connectors []*ConnectorInfo
	b, _ := json.Marshal(resp.Result)
	json.Unmarshal(b, &connectors)

	if len(connectors) != 2 {
		t.Errorf("expected 2 connectors, got %d", len(connectors))
	}
}

func TestInspectTypes(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	readResponse(t, conn)

	sendRequest(t, conn, 2, "inspect.types", nil)
	resp := readResponse(t, conn)

	if resp.Error != nil {
		t.Fatalf("inspect.types failed: %s", resp.Error.Message)
	}

	var types []*TypeInfo
	b, _ := json.Marshal(resp.Result)
	json.Unmarshal(b, &types)

	if len(types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(types))
	}
	if types[0].Name != "user" {
		t.Errorf("expected type user, got %s", types[0].Name)
	}
	if len(types[0].Fields) != 3 {
		t.Errorf("expected 3 fields, got %d", len(types[0].Fields))
	}
}

func TestInspectTransforms(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	readResponse(t, conn)

	sendRequest(t, conn, 2, "inspect.transforms", nil)
	resp := readResponse(t, conn)

	if resp.Error != nil {
		t.Fatalf("inspect.transforms failed: %s", resp.Error.Message)
	}

	var transforms []*TransformInfo
	b, _ := json.Marshal(resp.Result)
	json.Unmarshal(b, &transforms)

	if len(transforms) != 1 {
		t.Fatalf("expected 1 transform, got %d", len(transforms))
	}
	if transforms[0].Name != "normalize_email" {
		t.Errorf("expected transform normalize_email, got %s", transforms[0].Name)
	}
}

func TestSetBreakpoints(t *testing.T) {
	srv, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	resp := readResponse(t, conn)

	var attach AttachResult
	b, _ := json.Marshal(resp.Result)
	json.Unmarshal(b, &attach)

	// Set breakpoints
	sendRequest(t, conn, 2, "debug.setBreakpoints", &SetBreakpointsParams{
		Flow: "create_user",
		Breakpoints: []BreakpointSpec{
			{Stage: trace.StageTransform, RuleIndex: -1},
			{Stage: trace.StageValidateIn, RuleIndex: -1},
		},
	})
	resp = readResponse(t, conn)
	if resp.Error != nil {
		t.Fatalf("setBreakpoints failed: %s", resp.Error.Message)
	}

	// Verify breakpoints are stored
	sess, ok := srv.GetSession(attach.SessionID)
	if !ok {
		t.Fatal("session not found")
	}
	bps := sess.GetBreakpoints("create_user")
	if len(bps) != 2 {
		t.Errorf("expected 2 breakpoints, got %d", len(bps))
	}
}

func TestThreadsEmpty(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	readResponse(t, conn)

	sendRequest(t, conn, 2, "debug.threads", nil)
	resp := readResponse(t, conn)

	if resp.Error != nil {
		t.Fatalf("threads failed: %s", resp.Error.Message)
	}

	var result ThreadsResult
	b, _ := json.Marshal(resp.Result)
	json.Unmarshal(b, &result)

	if len(result.Threads) != 0 {
		t.Errorf("expected 0 threads, got %d", len(result.Threads))
	}
}

func TestUnknownMethod(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	readResponse(t, conn)

	sendRequest(t, conn, 2, "nonexistent.method", nil)
	resp := readResponse(t, conn)

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected code %d, got %d", CodeMethodNotFound, resp.Error.Code)
	}
}

func TestEventStreaming(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	resp := readResponse(t, conn)

	var attach AttachResult
	b, _ := json.Marshal(resp.Result)
	json.Unmarshal(b, &attach)

	// Simulate events from a flow
	sess, _ := startTestServer(t) // fresh server for inspector
	_ = sess

	// Use the actual server's stream
	srv, _ := startTestServer(t)
	defer func() {}()

	stream := srv.Stream()

	// Subscribe manually for testing
	ch := stream.Subscribe("test-sub")

	// Broadcast a flow start event
	stream.Broadcast(newNotification("event.flowStart", &FlowStartEvent{
		ThreadID: "t1",
		FlowName: "create_user",
		Input:    map[string]interface{}{"email": "test@example.com"},
	}))

	select {
	case n := <-ch:
		if n.Method != "event.flowStart" {
			t.Errorf("expected event.flowStart, got %s", n.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	stream.Unsubscribe("test-sub")
}

func TestStudioCollector(t *testing.T) {
	stream := NewEventStream()
	ch := stream.Subscribe("test")
	defer stream.Unsubscribe("test")

	collector := NewStudioCollector(stream, "t1", "create_user")

	// Record a stage event with output (simulates stage exit)
	collector.Record(trace.Event{
		Stage:    trace.StageTransform,
		Output:   map[string]interface{}{"email": "test@test.com"},
		Duration: 100 * time.Microsecond,
	})

	select {
	case n := <-ch:
		if n.Method != "event.stageExit" {
			t.Errorf("expected event.stageExit, got %s", n.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for stage exit event")
	}

	// Verify event was stored
	events := collector.Events()
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
}

func TestStudioCollectorFlowEvents(t *testing.T) {
	stream := NewEventStream()
	ch := stream.Subscribe("test")
	defer stream.Unsubscribe("test")

	collector := NewStudioCollector(stream, "t1", "create_user")

	// Flow start
	collector.BroadcastFlowStart(map[string]interface{}{"email": "test@test.com"})
	n := <-ch
	if n.Method != "event.flowStart" {
		t.Errorf("expected event.flowStart, got %s", n.Method)
	}

	// Flow end
	collector.BroadcastFlowEnd(map[string]interface{}{"id": 1}, 5*time.Millisecond, nil)
	n = <-ch
	if n.Method != "event.flowEnd" {
		t.Errorf("expected event.flowEnd, got %s", n.Method)
	}
}

func TestDebugThreadPauseResume(t *testing.T) {
	thread := NewDebugThread("t1", "create_user")

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		action := thread.Pause()
		if action != actionContinue {
			t.Errorf("expected actionContinue, got %d", action)
		}
	}()

	// Wait for thread to be paused
	thread.WaitForPause()

	if !thread.IsPaused() {
		t.Error("expected thread to be paused")
	}

	thread.Resume(actionContinue)
	wg.Wait()

	if thread.IsPaused() {
		t.Error("expected thread to not be paused after resume")
	}
}

func TestDebugThreadVariables(t *testing.T) {
	thread := NewDebugThread("t1", "create_user")

	activation := map[string]interface{}{
		"input": map[string]interface{}{
			"email": "TEST@EXAMPLE.COM",
			"name":  "Alice",
		},
		"output": map[string]interface{}{
			"email": "test@example.com",
		},
	}
	thread.SetState(trace.StageTransform, "", activation)

	vars := thread.GetVariables()
	input, ok := vars.Input.(map[string]interface{})
	if !ok {
		t.Fatal("expected input to be a map")
	}
	if input["email"] != "TEST@EXAMPLE.COM" {
		t.Errorf("unexpected input email: %v", input["email"])
	}

	output, ok := vars.Output.(map[string]interface{})
	if !ok {
		t.Fatal("expected output to be a map")
	}
	if output["email"] != "test@example.com" {
		t.Errorf("unexpected output email: %v", output["email"])
	}
}

func TestDebugThreadEvaluateCEL(t *testing.T) {
	thread := NewDebugThread("t1", "create_user")

	activation := map[string]interface{}{
		"input": map[string]interface{}{
			"email": "TEST@EXAMPLE.COM",
			"name":  "Alice",
		},
		"output": map[string]interface{}{},
		"ctx":    map[string]interface{}{},
	}
	thread.SetState(trace.StageTransform, "", activation)

	transformer, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	result, err := thread.EvaluateCEL(transformer, `input.name`)
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	if result != "Alice" {
		t.Errorf("expected Alice, got %v", result)
	}
}

func TestStudioBreakpointController(t *testing.T) {
	session := NewSession("s1", "test")
	session.SetBreakpoints("create_user", []BreakpointSpec{
		{Stage: trace.StageTransform, RuleIndex: -1},
	})

	stream := NewEventStream()
	ch := stream.Subscribe("test")
	defer stream.Unsubscribe("test")

	thread := NewDebugThread("t1", "create_user")
	session.RegisterThread(thread)
	defer session.UnregisterThread("t1")

	collector := NewStudioCollector(stream, "t1", "create_user")
	controller := NewStudioBreakpointController(session, thread, stream, collector)

	// Should break on transform stage
	if !controller.ShouldBreak(trace.StageTransform) {
		t.Error("expected ShouldBreak to return true for transform stage")
	}

	// Should not break on other stages
	if controller.ShouldBreak(trace.StageRead) {
		t.Error("expected ShouldBreak to return false for read stage")
	}

	// Test Pause (in goroutine since it blocks)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ok := controller.Pause(trace.StageTransform, "", map[string]interface{}{"email": "test"})
		if !ok {
			t.Error("expected Pause to return true (not aborted)")
		}
	}()

	// Wait for stopped event
	n := <-ch
	if n.Method != "event.stopped" {
		t.Errorf("expected event.stopped, got %s", n.Method)
	}

	// Wait for thread to actually pause
	thread.WaitForPause()

	// Resume
	thread.Resume(actionContinue)
	wg.Wait()

	// Should get continued event
	n = <-ch
	if n.Method != "event.continued" {
		t.Errorf("expected event.continued, got %s", n.Method)
	}
}

func TestTransformHookIntegration(t *testing.T) {
	transformer, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	rules := []transform.Rule{
		{Target: "email", Expression: `"test@example.com"`},
		{Target: "name", Expression: `"Alice"`},
	}

	// Track hook calls
	var hookCalls []string
	var mu sync.Mutex
	hook := &testHook{
		beforeFn: func(ctx context.Context, index int, rule transform.Rule, activation map[string]interface{}) bool {
			mu.Lock()
			hookCalls = append(hookCalls, fmt.Sprintf("before:%d:%s", index, rule.Target))
			mu.Unlock()
			return true
		},
		afterFn: func(ctx context.Context, index int, rule transform.Rule, result interface{}, err error) {
			mu.Lock()
			hookCalls = append(hookCalls, fmt.Sprintf("after:%d:%s:%v", index, rule.Target, result))
			mu.Unlock()
		},
	}

	ctx := transform.WithTransformHook(context.Background(), hook)
	input := map[string]interface{}{"raw": "data"}

	output, err := transformer.Transform(ctx, input, rules)
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}

	if output["email"] != "test@example.com" {
		t.Errorf("unexpected email: %v", output["email"])
	}
	if output["name"] != "Alice" {
		t.Errorf("unexpected name: %v", output["name"])
	}

	// Verify hook calls
	expected := []string{
		"before:0:email",
		"after:0:email:test@example.com",
		"before:1:name",
		"after:1:name:Alice",
	}
	if len(hookCalls) != len(expected) {
		t.Fatalf("expected %d hook calls, got %d: %v", len(expected), len(hookCalls), hookCalls)
	}
	for i, call := range hookCalls {
		if call != expected[i] {
			t.Errorf("hook call %d: expected %s, got %s", i, expected[i], call)
		}
	}
}

func TestTransformHookAbort(t *testing.T) {
	transformer, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	rules := []transform.Rule{
		{Target: "email", Expression: `"test@example.com"`},
		{Target: "name", Expression: `"Alice"`},
	}

	// Abort on second rule
	hook := &testHook{
		beforeFn: func(ctx context.Context, index int, rule transform.Rule, activation map[string]interface{}) bool {
			return index == 0 // abort on index 1
		},
		afterFn: func(ctx context.Context, index int, rule transform.Rule, result interface{}, err error) {},
	}

	ctx := transform.WithTransformHook(context.Background(), hook)
	_, err = transformer.Transform(ctx, map[string]interface{}{}, rules)
	if err == nil {
		t.Fatal("expected error from aborted transform")
	}
	if !strings.Contains(err.Error(), "aborted") {
		t.Errorf("expected abort error, got: %v", err)
	}
}

func TestTransformHookNilCost(t *testing.T) {
	// Verify that transforms work normally without a hook (nil context value)
	transformer, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	rules := []transform.Rule{
		{Target: "result", Expression: `"hello"`},
	}

	output, err := transformer.Transform(context.Background(), map[string]interface{}{}, rules)
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}
	if output["result"] != "hello" {
		t.Errorf("unexpected result: %v", output["result"])
	}
}

func TestTransformResponseHook(t *testing.T) {
	transformer, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	rules := []transform.Rule{
		{Target: "total", Expression: `output.count`},
	}

	var hookCalls int
	hook := &testHook{
		beforeFn: func(ctx context.Context, index int, rule transform.Rule, activation map[string]interface{}) bool {
			hookCalls++
			return true
		},
		afterFn: func(ctx context.Context, index int, rule transform.Rule, result interface{}, err error) {
			hookCalls++
		},
	}

	ctx := transform.WithTransformHook(context.Background(), hook)
	input := map[string]interface{}{}
	output := map[string]interface{}{"count": 42}

	result, err := transformer.TransformResponse(ctx, input, output, rules)
	if err != nil {
		t.Fatalf("transform response failed: %v", err)
	}

	if hookCalls != 2 {
		t.Errorf("expected 2 hook calls, got %d", hookCalls)
	}

	// CEL returns int64 for integer values
	if result["total"] != int64(42) {
		t.Errorf("expected 42, got %v (type %T)", result["total"], result["total"])
	}
}

func TestTransformWithContextHook(t *testing.T) {
	transformer, err := transform.NewCELTransformer()
	if err != nil {
		t.Fatalf("failed to create transformer: %v", err)
	}

	rules := []transform.Rule{
		{Target: "combined", Expression: `input.name + " " + enriched.suffix`},
	}

	var hookCalls int
	hook := &testHook{
		beforeFn: func(ctx context.Context, index int, rule transform.Rule, activation map[string]interface{}) bool {
			hookCalls++
			return true
		},
		afterFn: func(ctx context.Context, index int, rule transform.Rule, result interface{}, err error) {
			hookCalls++
		},
	}

	ctx := transform.WithTransformHook(context.Background(), hook)
	input := map[string]interface{}{"name": "Alice"}
	enriched := map[string]interface{}{"suffix": "Smith"}

	result, err := transformer.TransformWithContext(ctx, input, enriched, nil, rules)
	if err != nil {
		t.Fatalf("transform with context failed: %v", err)
	}

	if hookCalls != 2 {
		t.Errorf("expected 2 hook calls, got %d", hookCalls)
	}
	if result["combined"] != "Alice Smith" {
		t.Errorf("expected 'Alice Smith', got %v", result["combined"])
	}
}

func TestSessionBreakpointManagement(t *testing.T) {
	session := NewSession("s1", "test")

	// Initially no breakpoints
	if session.HasBreakpoints("create_user") {
		t.Error("expected no breakpoints initially")
	}

	// Set breakpoints
	session.SetBreakpoints("create_user", []BreakpointSpec{
		{Stage: trace.StageTransform, RuleIndex: -1},
		{Stage: trace.StageTransform, RuleIndex: 0, Condition: `input.email != ""`},
	})

	if !session.HasBreakpoints("create_user") {
		t.Error("expected breakpoints after set")
	}

	bps := session.GetBreakpoints("create_user")
	if len(bps) != 2 {
		t.Errorf("expected 2 breakpoints, got %d", len(bps))
	}

	// Clear breakpoints
	session.SetBreakpoints("create_user", nil)
	if session.HasBreakpoints("create_user") {
		t.Error("expected no breakpoints after clear")
	}
}

func TestSessionThreadManagement(t *testing.T) {
	session := NewSession("s1", "test")

	thread := NewDebugThread("t1", "create_user")
	session.RegisterThread(thread)

	threads := session.ListThreads()
	if len(threads) != 1 {
		t.Errorf("expected 1 thread, got %d", len(threads))
	}

	got, ok := session.GetThread("t1")
	if !ok {
		t.Fatal("expected to find thread t1")
	}
	if got.FlowName != "create_user" {
		t.Errorf("expected flow create_user, got %s", got.FlowName)
	}

	session.UnregisterThread("t1")
	threads = session.ListThreads()
	if len(threads) != 0 {
		t.Errorf("expected 0 threads after unregister, got %d", len(threads))
	}
}

func TestEventStreamBroadcast(t *testing.T) {
	stream := NewEventStream()

	// Subscribe two clients
	ch1 := stream.Subscribe("c1")
	ch2 := stream.Subscribe("c2")

	// Broadcast
	n := newNotification("event.test", map[string]string{"key": "value"})
	stream.Broadcast(n)

	// Both should receive
	select {
	case got := <-ch1:
		if got.Method != "event.test" {
			t.Errorf("c1: expected event.test, got %s", got.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("c1: timeout")
	}

	select {
	case got := <-ch2:
		if got.Method != "event.test" {
			t.Errorf("c2: expected event.test, got %s", got.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("c2: timeout")
	}

	// Unsubscribe c1
	stream.Unsubscribe("c1")

	// Only c2 should receive
	stream.Broadcast(newNotification("event.test2", nil))
	select {
	case got := <-ch2:
		if got.Method != "event.test2" {
			t.Errorf("c2: expected event.test2, got %s", got.Method)
		}
	case <-time.After(time.Second):
		t.Fatal("c2: timeout after unsubscribe")
	}

	stream.Unsubscribe("c2")
}

func TestHasClients(t *testing.T) {
	srv, ts := startTestServer(t)
	defer ts.Close()

	if srv.HasClients() {
		t.Error("expected no clients initially")
	}

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	readResponse(t, conn)

	if !srv.HasClients() {
		t.Error("expected clients after attach")
	}
}

func TestContinueThreadNotFound(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	readResponse(t, conn)

	sendRequest(t, conn, 2, "debug.continue", &ContinueParams{ThreadID: "nonexistent"})
	resp := readResponse(t, conn)

	if resp.Error == nil {
		t.Fatal("expected error for nonexistent thread")
	}
	if resp.Error.Code != CodeThreadNotFound {
		t.Errorf("expected code %d, got %d", CodeThreadNotFound, resp.Error.Code)
	}
}

func TestEvaluateNotPaused(t *testing.T) {
	srv, ts := startTestServer(t)
	defer ts.Close()

	conn := connectWS(t, ts)
	defer conn.Close()

	sendRequest(t, conn, 1, "debug.attach", &AttachParams{})
	resp := readResponse(t, conn)

	var attach AttachResult
	b, _ := json.Marshal(resp.Result)
	json.Unmarshal(b, &attach)

	// Register a thread that is NOT paused
	sess, ok := srv.GetSession(attach.SessionID)
	if !ok {
		t.Fatalf("session %s not found", attach.SessionID)
	}
	thread := NewDebugThread("t1", "create_user")
	sess.RegisterThread(thread)
	defer sess.UnregisterThread("t1")

	sendRequest(t, conn, 2, "debug.evaluate", &EvaluateParams{
		ThreadID:   "t1",
		Expression: `"hello"`,
	})
	resp = readResponse(t, conn)

	if resp.Error == nil {
		t.Fatal("expected error for evaluate on non-paused thread")
	}
}

// --- Test Helpers ---

type testHook struct {
	beforeFn func(ctx context.Context, index int, rule transform.Rule, activation map[string]interface{}) bool
	afterFn  func(ctx context.Context, index int, rule transform.Rule, result interface{}, err error)
}

func (h *testHook) BeforeRule(ctx context.Context, index int, rule transform.Rule, activation map[string]interface{}) bool {
	return h.beforeFn(ctx, index, rule, activation)
}

func (h *testHook) AfterRule(ctx context.Context, index int, rule transform.Rule, result interface{}, err error) {
	h.afterFn(ctx, index, rule, result, err)
}
