package cdc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// mockListener implements Listener for testing without a real database.
type mockListener struct {
	events []*Event
	mu     sync.Mutex
	closed bool
}

func newMockListener(events ...*Event) *mockListener {
	return &mockListener{events: events}
}

func (m *mockListener) Start(ctx context.Context, eventCh chan<- *Event) error {
	for _, e := range m.events {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case eventCh <- e:
		}
	}
	// Wait for context cancellation
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockListener) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func TestFactory(t *testing.T) {
	factory := NewFactory(nil)
	cfg := &connector.Config{
		Name:   "test_cdc",
		Type:   "cdc",
		Driver: "postgres",
		Properties: map[string]interface{}{
			"host":        "localhost",
			"port":        5432,
			"database":    "testdb",
			"user":        "repl_user",
			"password":    "secret",
			"slot_name":   "test_slot",
			"publication": "test_pub",
		},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if conn.Name() != "test_cdc" {
		t.Errorf("expected name test_cdc, got %s", conn.Name())
	}
	if conn.Type() != "cdc" {
		t.Errorf("expected type cdc, got %s", conn.Type())
	}
}

func TestFactoryDefaults(t *testing.T) {
	factory := NewFactory(nil)
	cfg := &connector.Config{
		Name:       "default_cdc",
		Type:       "cdc",
		Properties: map[string]interface{}{},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	cdc := conn.(*Connector)
	if cdc.config.Driver != "postgres" {
		t.Errorf("expected default driver postgres, got %s", cdc.config.Driver)
	}
	if cdc.config.Port != 5432 {
		t.Errorf("expected default port 5432, got %d", cdc.config.Port)
	}
	if cdc.config.SlotName != "mycel_cdc" {
		t.Errorf("expected default slot mycel_cdc, got %s", cdc.config.SlotName)
	}
	if cdc.config.Publication != "mycel_pub" {
		t.Errorf("expected default publication mycel_pub, got %s", cdc.config.Publication)
	}
}

func TestFactoryUnsupportedDriver(t *testing.T) {
	factory := NewFactory(nil)
	cfg := &connector.Config{
		Name:       "bad_cdc",
		Type:       "cdc",
		Driver:     "oracle",
		Properties: map[string]interface{}{},
	}

	_, err := factory.Create(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for unsupported driver")
	}
}

func TestFactorySupports(t *testing.T) {
	factory := NewFactory(nil)
	if !factory.Supports("cdc", "") {
		t.Error("expected Supports('cdc', '') = true")
	}
	if !factory.Supports("cdc", "postgres") {
		t.Error("expected Supports('cdc', 'postgres') = true")
	}
	if factory.Supports("database", "postgres") {
		t.Error("expected Supports('database', 'postgres') = false")
	}
}

func TestRegisterRoute(t *testing.T) {
	conn := New("test", &Config{}, newMockListener(), nil)

	conn.RegisterRoute("INSERT:users", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, nil
	})

	if len(conn.handlers) != 1 {
		t.Errorf("expected 1 handler, got %d", len(conn.handlers))
	}
	if _, ok := conn.handlers["INSERT:users"]; !ok {
		t.Error("expected handler key to be normalized (trigger upper, table lower)")
	}
}

func TestEventDispatch(t *testing.T) {
	events := []*Event{
		{Trigger: "INSERT", Schema: "public", Table: "users", New: map[string]interface{}{"id": int64(1), "name": "alice"}, Timestamp: time.Now()},
		{Trigger: "UPDATE", Schema: "public", Table: "orders", New: map[string]interface{}{"status": "shipped"}, Old: map[string]interface{}{"status": "pending"}, Timestamp: time.Now()},
	}

	conn := New("test", &Config{Driver: "postgres"}, newMockListener(events...), nil)

	var mu sync.Mutex
	var insertCalls, updateCalls int
	var lastInsertInput, lastUpdateInput map[string]interface{}

	conn.RegisterRoute("INSERT:users", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		insertCalls++
		lastInsertInput = input
		mu.Unlock()
		return nil, nil
	})

	conn.RegisterRoute("UPDATE:orders", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		updateCalls++
		lastUpdateInput = input
		mu.Unlock()
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := conn.Start(ctx); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	// Wait for events to be processed
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		done := insertCalls >= 1 && updateCalls >= 1
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for events")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	mu.Lock()
	defer mu.Unlock()

	if insertCalls != 1 {
		t.Errorf("expected 1 insert call, got %d", insertCalls)
	}
	if updateCalls != 1 {
		t.Errorf("expected 1 update call, got %d", updateCalls)
	}

	// Verify input format
	if lastInsertInput["trigger"] != "INSERT" {
		t.Errorf("expected trigger INSERT, got %v", lastInsertInput["trigger"])
	}
	if lastInsertInput["table"] != "users" {
		t.Errorf("expected table users, got %v", lastInsertInput["table"])
	}
	newData := lastInsertInput["new"].(map[string]interface{})
	if newData["name"] != "alice" {
		t.Errorf("expected name alice, got %v", newData["name"])
	}

	if lastUpdateInput["trigger"] != "UPDATE" {
		t.Errorf("expected trigger UPDATE, got %v", lastUpdateInput["trigger"])
	}
	oldData := lastUpdateInput["old"].(map[string]interface{})
	if oldData["status"] != "pending" {
		t.Errorf("expected old status pending, got %v", oldData["status"])
	}
}

func TestWildcardTriggerDispatch(t *testing.T) {
	events := []*Event{
		{Trigger: "INSERT", Schema: "public", Table: "users", New: map[string]interface{}{"id": int64(1)}, Timestamp: time.Now()},
		{Trigger: "UPDATE", Schema: "public", Table: "users", New: map[string]interface{}{"id": int64(1)}, Old: map[string]interface{}{"id": int64(1)}, Timestamp: time.Now()},
		{Trigger: "DELETE", Schema: "public", Table: "users", Old: map[string]interface{}{"id": int64(1)}, Timestamp: time.Now()},
	}

	conn := New("test", &Config{Driver: "postgres"}, newMockListener(events...), nil)

	var mu sync.Mutex
	var calls int

	conn.RegisterRoute("*:users", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn.Start(ctx)

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		done := calls >= 3
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			mu.Lock()
			t.Fatalf("timeout: expected 3 calls, got %d", calls)
			mu.Unlock()
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestTableWildcard(t *testing.T) {
	events := []*Event{
		{Trigger: "INSERT", Schema: "public", Table: "users", New: map[string]interface{}{"id": int64(1)}, Timestamp: time.Now()},
		{Trigger: "INSERT", Schema: "public", Table: "orders", New: map[string]interface{}{"id": int64(2)}, Timestamp: time.Now()},
	}

	conn := New("test", &Config{Driver: "postgres"}, newMockListener(events...), nil)

	var mu sync.Mutex
	var calls int

	conn.RegisterRoute("INSERT:*", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn.Start(ctx)

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		done := calls >= 2
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			mu.Lock()
			t.Fatalf("timeout: expected 2 calls, got %d", calls)
			mu.Unlock()
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestGlobalWildcard(t *testing.T) {
	events := []*Event{
		{Trigger: "INSERT", Schema: "public", Table: "users", New: map[string]interface{}{}, Timestamp: time.Now()},
		{Trigger: "DELETE", Schema: "public", Table: "orders", Old: map[string]interface{}{}, Timestamp: time.Now()},
	}

	conn := New("test", &Config{Driver: "postgres"}, newMockListener(events...), nil)

	var mu sync.Mutex
	var calls int

	conn.RegisterRoute("*:*", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn.Start(ctx)

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		done := calls >= 2
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			mu.Lock()
			t.Fatalf("timeout: expected 2 calls, got %d", calls)
			mu.Unlock()
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestEventToInput(t *testing.T) {
	ts := time.Date(2026, 2, 27, 10, 30, 0, 0, time.UTC)
	event := &Event{
		Trigger:   "UPDATE",
		Schema:    "public",
		Table:     "products",
		New:       map[string]interface{}{"price": 19.99, "name": "Widget"},
		Old:       map[string]interface{}{"price": 14.99, "name": "Widget"},
		Timestamp: ts,
	}

	input := eventToInput(event)

	if input["trigger"] != "UPDATE" {
		t.Errorf("expected trigger UPDATE, got %v", input["trigger"])
	}
	if input["table"] != "products" {
		t.Errorf("expected table products, got %v", input["table"])
	}
	if input["schema"] != "public" {
		t.Errorf("expected schema public, got %v", input["schema"])
	}
	if input["timestamp"] != "2026-02-27T10:30:00Z" {
		t.Errorf("expected timestamp 2026-02-27T10:30:00Z, got %v", input["timestamp"])
	}

	newData := input["new"].(map[string]interface{})
	if newData["price"] != 19.99 {
		t.Errorf("expected new price 19.99, got %v", newData["price"])
	}

	oldData := input["old"].(map[string]interface{})
	if oldData["price"] != 14.99 {
		t.Errorf("expected old price 14.99, got %v", oldData["price"])
	}
}

func TestEventToInputNulls(t *testing.T) {
	event := &Event{
		Trigger:   "INSERT",
		Schema:    "public",
		Table:     "users",
		New:       map[string]interface{}{"id": int64(1)},
		Old:       nil,
		Timestamp: time.Now(),
	}

	input := eventToInput(event)

	if _, ok := input["old"]; ok {
		t.Error("expected no 'old' key for INSERT event")
	}
	if _, ok := input["new"]; !ok {
		t.Error("expected 'new' key for INSERT event")
	}
}

func TestUnmatchedEvent(t *testing.T) {
	events := []*Event{
		{Trigger: "INSERT", Schema: "public", Table: "unhandled_table", New: map[string]interface{}{}, Timestamp: time.Now()},
	}

	conn := New("test", &Config{Driver: "postgres"}, newMockListener(events...), nil)

	// Register a handler for a different table
	conn.RegisterRoute("INSERT:users", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		t.Error("should not be called for unhandled_table")
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	conn.Start(ctx)

	// Give time for the event to be processed (or skipped)
	time.Sleep(100 * time.Millisecond)
	cancel()
}

func TestOperationParsing(t *testing.T) {
	tests := []struct {
		operation string
		trigger   string
		table     string
	}{
		{"INSERT:users", "INSERT", "users"},
		{"UPDATE:orders", "UPDATE", "orders"},
		{"DELETE:sessions", "DELETE", "sessions"},
		{"*:users", "*", "users"},
		{"INSERT:*", "INSERT", "*"},
		{"*:*", "*", "*"},
		{"*", "*", "*"},
		{"insert:Users", "INSERT", "users"},
	}

	for _, tt := range tests {
		trigger, table := ParseOperation(tt.operation)
		if trigger != tt.trigger {
			t.Errorf("ParseOperation(%q) trigger = %q, want %q", tt.operation, trigger, tt.trigger)
		}
		if table != tt.table {
			t.Errorf("ParseOperation(%q) table = %q, want %q", tt.operation, table, tt.table)
		}
	}
}

func TestHealthBeforeStart(t *testing.T) {
	conn := New("test", &Config{}, newMockListener(), nil)
	if err := conn.Health(context.Background()); err == nil {
		t.Error("expected health error before start")
	}
}

func TestHealthAfterStart(t *testing.T) {
	conn := New("test", &Config{Driver: "postgres"}, newMockListener(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn.Start(ctx)
	if err := conn.Health(ctx); err != nil {
		t.Errorf("expected healthy after start, got %v", err)
	}
}

func TestConnectAndType(t *testing.T) {
	conn := New("my_cdc", &Config{}, newMockListener(), nil)

	if conn.Name() != "my_cdc" {
		t.Errorf("expected name my_cdc, got %s", conn.Name())
	}
	if conn.Type() != "cdc" {
		t.Errorf("expected type cdc, got %s", conn.Type())
	}
	if err := conn.Connect(context.Background()); err != nil {
		t.Errorf("expected connect to succeed, got %v", err)
	}
}

func TestClose(t *testing.T) {
	mock := newMockListener()
	conn := New("test", &Config{Driver: "postgres"}, mock, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn.Start(ctx)
	if err := conn.Close(context.Background()); err != nil {
		t.Errorf("expected close to succeed, got %v", err)
	}

	mock.mu.Lock()
	if !mock.closed {
		t.Error("expected listener to be closed")
	}
	mock.mu.Unlock()
}
