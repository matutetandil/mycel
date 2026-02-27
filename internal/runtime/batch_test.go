package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/matutetandil/mycel/internal/connector"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/transform"
)

// mockReader implements connector.Reader for batch tests.
type mockBatchReader struct {
	name string
	rows []map[string]interface{}
	err  error
}

func (m *mockBatchReader) Name() string                        { return m.name }
func (m *mockBatchReader) Type() string                        { return "mock" }
func (m *mockBatchReader) Connect(ctx context.Context) error   { return nil }
func (m *mockBatchReader) Close(ctx context.Context) error     { return nil }
func (m *mockBatchReader) Health(ctx context.Context) error    { return nil }

func (m *mockBatchReader) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	if m.err != nil {
		return nil, m.err
	}

	offset := 0
	limit := len(m.rows)
	if query.Pagination != nil {
		offset = query.Pagination.Offset
		limit = query.Pagination.Limit
	}

	if offset >= len(m.rows) {
		return &connector.Result{Rows: nil}, nil
	}

	end := offset + limit
	if end > len(m.rows) {
		end = len(m.rows)
	}

	return &connector.Result{
		Rows: m.rows[offset:end],
	}, nil
}

// mockWriter implements connector.Writer for batch tests.
type mockBatchWriter struct {
	name       string
	written    []map[string]interface{}
	err        error
	failAt     int // fail at this write call index (-1 = never)
	writeCount int // tracks total write calls (including failures)
}

func (m *mockBatchWriter) Name() string                        { return m.name }
func (m *mockBatchWriter) Type() string                        { return "mock" }
func (m *mockBatchWriter) Connect(ctx context.Context) error   { return nil }
func (m *mockBatchWriter) Close(ctx context.Context) error     { return nil }
func (m *mockBatchWriter) Health(ctx context.Context) error    { return nil }

func (m *mockBatchWriter) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	if m.failAt >= 0 && m.writeCount == m.failAt {
		m.writeCount++
		return nil, fmt.Errorf("simulated write error at index %d", m.failAt)
	}
	if m.err != nil {
		return nil, m.err
	}
	m.written = append(m.written, data.Payload)
	m.writeCount++
	return &connector.Result{Affected: 1}, nil
}

func newBatchHandler(reader *mockBatchReader, writer *mockBatchWriter, batch *flow.BatchConfig) *FlowHandler {
	registry := connector.NewRegistry()
	registry.Replace(reader.Name(), reader)
	registry.Replace(writer.Name(), writer)

	return &FlowHandler{
		Config: &flow.Config{
			Name: "test_batch",
			From: &flow.FromConfig{
				Connector: "api",
				Operation: "POST /batch",
			},
			To:    &flow.ToConfig{Connector: writer.Name()},
			Batch: batch,
		},
		Connectors: registry,
	}
}

func TestBatchBasic(t *testing.T) {
	reader := &mockBatchReader{
		name: "source_db",
		rows: []map[string]interface{}{
			{"id": 1, "name": "Alice"},
			{"id": 2, "name": "Bob"},
			{"id": 3, "name": "Carol"},
		},
	}
	writer := &mockBatchWriter{name: "target_db", failAt: -1}

	handler := newBatchHandler(reader, writer, &flow.BatchConfig{
		Source:    "source_db",
		Query:     "SELECT * FROM users",
		ChunkSize: 10,
		OnError:   "stop",
		To: &flow.ToConfig{
			Connector: "target_db",
			Target:    "users",
			Operation: "INSERT",
		},
	})

	result, err := handler.executeBatch(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}

	batchResult, ok := result.(*flow.BatchResult)
	if !ok {
		t.Fatalf("expected *flow.BatchResult, got %T", result)
	}

	if batchResult.Processed != 3 {
		t.Errorf("expected processed=3, got %d", batchResult.Processed)
	}
	if batchResult.Failed != 0 {
		t.Errorf("expected failed=0, got %d", batchResult.Failed)
	}
	if batchResult.Chunks != 1 {
		t.Errorf("expected chunks=1, got %d", batchResult.Chunks)
	}
	if len(writer.written) != 3 {
		t.Errorf("expected 3 written records, got %d", len(writer.written))
	}
}

func TestBatchChunking(t *testing.T) {
	rows := make([]map[string]interface{}, 25)
	for i := 0; i < 25; i++ {
		rows[i] = map[string]interface{}{"id": i, "name": fmt.Sprintf("user_%d", i)}
	}

	reader := &mockBatchReader{name: "source_db", rows: rows}
	writer := &mockBatchWriter{name: "target_db", failAt: -1}

	handler := newBatchHandler(reader, writer, &flow.BatchConfig{
		Source:    "source_db",
		Query:     "SELECT * FROM users",
		ChunkSize: 10,
		OnError:   "stop",
		To: &flow.ToConfig{
			Connector: "target_db",
			Target:    "users",
			Operation: "INSERT",
		},
	})

	result, err := handler.executeBatch(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}

	batchResult := result.(*flow.BatchResult)
	if batchResult.Processed != 25 {
		t.Errorf("expected processed=25, got %d", batchResult.Processed)
	}
	if batchResult.Chunks != 3 {
		t.Errorf("expected chunks=3 (10+10+5), got %d", batchResult.Chunks)
	}
	if len(writer.written) != 25 {
		t.Errorf("expected 25 written records, got %d", len(writer.written))
	}
}

func TestBatchWithTransform(t *testing.T) {
	reader := &mockBatchReader{
		name: "source_db",
		rows: []map[string]interface{}{
			{"id": 1, "email": "ALICE@EXAMPLE.COM"},
			{"id": 2, "email": "BOB@EXAMPLE.COM"},
		},
	}
	writer := &mockBatchWriter{name: "target_db", failAt: -1}

	handler := newBatchHandler(reader, writer, &flow.BatchConfig{
		Source:    "source_db",
		Query:     "SELECT * FROM users",
		ChunkSize: 100,
		OnError:   "stop",
		Transform: &flow.TransformConfig{
			Mappings: map[string]string{
				"id":        "input.id",
				"email":     "input.email.lowerAscii()",
				"migrated":  "true",
			},
		},
		To: &flow.ToConfig{
			Connector: "target_db",
			Target:    "users",
			Operation: "INSERT",
		},
	})

	// Need transformer initialized
	transformer, _ := transform.NewCELTransformer()
	handler.Transformer = transformer

	result, err := handler.executeBatch(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}

	batchResult := result.(*flow.BatchResult)
	if batchResult.Processed != 2 {
		t.Errorf("expected processed=2, got %d", batchResult.Processed)
	}

	if len(writer.written) != 2 {
		t.Fatalf("expected 2 written records, got %d", len(writer.written))
	}

	if writer.written[0]["email"] != "alice@example.com" {
		t.Errorf("expected lowercased email, got %v", writer.written[0]["email"])
	}
	if writer.written[0]["migrated"] != true {
		t.Errorf("expected migrated=true, got %v", writer.written[0]["migrated"])
	}
}

func TestBatchOnErrorContinue(t *testing.T) {
	reader := &mockBatchReader{
		name: "source_db",
		rows: []map[string]interface{}{
			{"id": 1, "name": "Alice"},
			{"id": 2, "name": "Bob"},
			{"id": 3, "name": "Carol"},
		},
	}
	writer := &mockBatchWriter{name: "target_db", failAt: 1} // Fail on second write

	handler := newBatchHandler(reader, writer, &flow.BatchConfig{
		Source:    "source_db",
		Query:     "SELECT * FROM users",
		ChunkSize: 100,
		OnError:   "continue",
		To: &flow.ToConfig{
			Connector: "target_db",
			Target:    "users",
			Operation: "INSERT",
		},
	})

	result, err := handler.executeBatch(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("batch with on_error=continue should not fail: %v", err)
	}

	batchResult := result.(*flow.BatchResult)
	if batchResult.Processed != 2 {
		t.Errorf("expected processed=2, got %d", batchResult.Processed)
	}
	if batchResult.Failed != 1 {
		t.Errorf("expected failed=1, got %d", batchResult.Failed)
	}
	if len(batchResult.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(batchResult.Errors))
	}
}

func TestBatchOnErrorStop(t *testing.T) {
	reader := &mockBatchReader{
		name: "source_db",
		rows: []map[string]interface{}{
			{"id": 1, "name": "Alice"},
			{"id": 2, "name": "Bob"},
		},
	}
	writer := &mockBatchWriter{name: "target_db", failAt: 0} // Fail on first write

	handler := newBatchHandler(reader, writer, &flow.BatchConfig{
		Source:    "source_db",
		Query:     "SELECT * FROM users",
		ChunkSize: 100,
		OnError:   "stop",
		To: &flow.ToConfig{
			Connector: "target_db",
			Target:    "users",
			Operation: "INSERT",
		},
	})

	_, err := handler.executeBatch(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("batch with on_error=stop should fail on write error")
	}
}

func TestBatchEmptySource(t *testing.T) {
	reader := &mockBatchReader{name: "source_db", rows: nil}
	writer := &mockBatchWriter{name: "target_db", failAt: -1}

	handler := newBatchHandler(reader, writer, &flow.BatchConfig{
		Source:    "source_db",
		Query:     "SELECT * FROM empty_table",
		ChunkSize: 100,
		OnError:   "stop",
		To: &flow.ToConfig{
			Connector: "target_db",
			Target:    "users",
			Operation: "INSERT",
		},
	})

	result, err := handler.executeBatch(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("batch with empty source should succeed: %v", err)
	}

	batchResult := result.(*flow.BatchResult)
	if batchResult.Processed != 0 {
		t.Errorf("expected processed=0, got %d", batchResult.Processed)
	}
	if batchResult.Chunks != 0 {
		t.Errorf("expected chunks=0, got %d", batchResult.Chunks)
	}
}

func TestBatchWithParams(t *testing.T) {
	reader := &mockBatchReader{
		name: "source_db",
		rows: []map[string]interface{}{
			{"id": 1, "name": "Alice"},
		},
	}
	writer := &mockBatchWriter{name: "target_db", failAt: -1}

	handler := newBatchHandler(reader, writer, &flow.BatchConfig{
		Source:    "source_db",
		Query:     "SELECT * FROM users WHERE created_at > :since",
		Params:    map[string]interface{}{"since": "2024-01-01"},
		ChunkSize: 100,
		OnError:   "stop",
		To: &flow.ToConfig{
			Connector: "target_db",
			Target:    "users",
			Operation: "INSERT",
		},
	})

	result, err := handler.executeBatch(context.Background(), map[string]interface{}{
		"since": "2024-01-01",
	})
	if err != nil {
		t.Fatalf("batch with params failed: %v", err)
	}

	batchResult := result.(*flow.BatchResult)
	if batchResult.Processed != 1 {
		t.Errorf("expected processed=1, got %d", batchResult.Processed)
	}
}

func TestBatchResultStats(t *testing.T) {
	rows := make([]map[string]interface{}, 50)
	for i := 0; i < 50; i++ {
		rows[i] = map[string]interface{}{"id": i}
	}

	reader := &mockBatchReader{name: "source_db", rows: rows}
	writer := &mockBatchWriter{name: "target_db", failAt: -1}

	handler := newBatchHandler(reader, writer, &flow.BatchConfig{
		Source:    "source_db",
		Query:     "SELECT * FROM items",
		ChunkSize: 15,
		OnError:   "stop",
		To: &flow.ToConfig{
			Connector: "target_db",
			Target:    "items",
			Operation: "INSERT",
		},
	})

	result, err := handler.executeBatch(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}

	batchResult := result.(*flow.BatchResult)
	if batchResult.Processed != 50 {
		t.Errorf("expected processed=50, got %d", batchResult.Processed)
	}
	// 15 + 15 + 15 + 5 = 4 chunks
	if batchResult.Chunks != 4 {
		t.Errorf("expected chunks=4, got %d", batchResult.Chunks)
	}
	if batchResult.Failed != 0 {
		t.Errorf("expected failed=0, got %d", batchResult.Failed)
	}
}

func TestBatchDefaultChunkSize(t *testing.T) {
	reader := &mockBatchReader{
		name: "source_db",
		rows: []map[string]interface{}{{"id": 1}},
	}
	writer := &mockBatchWriter{name: "target_db", failAt: -1}

	handler := newBatchHandler(reader, writer, &flow.BatchConfig{
		Source:    "source_db",
		ChunkSize: 0, // Should default to 100
		OnError:   "stop",
		To: &flow.ToConfig{
			Connector: "target_db",
			Target:    "users",
			Operation: "INSERT",
		},
	})

	_, err := handler.executeBatch(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("batch with default chunk_size failed: %v", err)
	}
}

func TestBatchSourceNotFound(t *testing.T) {
	writer := &mockBatchWriter{name: "target_db", failAt: -1}
	registry := connector.NewRegistry()
	registry.Replace("target_db", writer)

	handler := &FlowHandler{
		Config: &flow.Config{
			Name: "test_batch",
			From: &flow.FromConfig{Connector: "api", Operation: "POST /batch"},
			To:   &flow.ToConfig{Connector: "target_db"},
			Batch: &flow.BatchConfig{
				Source:    "nonexistent",
				ChunkSize: 100,
				OnError:   "stop",
				To: &flow.ToConfig{
					Connector: "target_db",
					Target:    "users",
					Operation: "INSERT",
				},
			},
		},
		Connectors: registry,
	}

	_, err := handler.executeBatch(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing source connector")
	}
}

func TestBatchReadError(t *testing.T) {
	reader := &mockBatchReader{
		name: "source_db",
		err:  fmt.Errorf("connection lost"),
	}
	writer := &mockBatchWriter{name: "target_db", failAt: -1}

	handler := newBatchHandler(reader, writer, &flow.BatchConfig{
		Source:    "source_db",
		ChunkSize: 100,
		OnError:   "stop",
		To: &flow.ToConfig{
			Connector: "target_db",
			Target:    "users",
			Operation: "INSERT",
		},
	})

	_, err := handler.executeBatch(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for read failure with on_error=stop")
	}
}

func TestBatchReadErrorContinue(t *testing.T) {
	reader := &mockBatchReader{
		name: "source_db",
		err:  fmt.Errorf("connection lost"),
	}
	writer := &mockBatchWriter{name: "target_db", failAt: -1}

	handler := newBatchHandler(reader, writer, &flow.BatchConfig{
		Source:    "source_db",
		ChunkSize: 100,
		OnError:   "continue",
		To: &flow.ToConfig{
			Connector: "target_db",
			Target:    "users",
			Operation: "INSERT",
		},
	})

	// With on_error=continue, read error on first chunk means we read past all rows
	// Since there's only one error and no subsequent data, the loop ends after one error
	result, err := handler.executeBatch(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("batch with on_error=continue should not fail: %v", err)
	}

	batchResult := result.(*flow.BatchResult)
	if len(batchResult.Errors) == 0 {
		t.Error("expected errors to be recorded")
	}
}
