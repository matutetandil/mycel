package elasticsearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// newTestServer creates an httptest.Server that mocks Elasticsearch responses.
func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

// newTestConnector creates a connector pointing to a test server.
func newTestConnector(t *testing.T, server *httptest.Server) *Connector {
	t.Helper()
	config := &Config{
		Nodes:   []string{server.URL},
		Index:   "test_index",
		Timeout: 5 * time.Second,
	}
	return New("test_es", config, nil)
}

func TestFactory(t *testing.T) {
	factory := NewFactory(nil)

	cfg := &connector.Config{
		Name: "test_es",
		Type: "elasticsearch",
		Properties: map[string]interface{}{
			"nodes":    []interface{}{"http://localhost:9200", "http://localhost:9201"},
			"username": "elastic",
			"password": "changeme",
			"index":    "products",
			"timeout":  "10s",
		},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("factory.Create failed: %v", err)
	}

	if conn.Name() != "test_es" {
		t.Errorf("expected name=test_es, got %s", conn.Name())
	}
	if conn.Type() != "elasticsearch" {
		t.Errorf("expected type=elasticsearch, got %s", conn.Type())
	}

	esConn := conn.(*Connector)
	if len(esConn.nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(esConn.nodes))
	}
	if esConn.username != "elastic" {
		t.Errorf("expected username=elastic, got %s", esConn.username)
	}
	if esConn.index != "products" {
		t.Errorf("expected index=products, got %s", esConn.index)
	}
	if esConn.timeout != 10*time.Second {
		t.Errorf("expected timeout=10s, got %v", esConn.timeout)
	}
}

func TestFactoryDefaults(t *testing.T) {
	factory := NewFactory(nil)

	cfg := &connector.Config{
		Name:       "default_es",
		Type:       "elasticsearch",
		Properties: map[string]interface{}{},
	}

	conn, err := factory.Create(context.Background(), cfg)
	if err != nil {
		t.Fatalf("factory.Create failed: %v", err)
	}

	esConn := conn.(*Connector)
	if len(esConn.nodes) != 1 || esConn.nodes[0] != "http://localhost:9200" {
		t.Errorf("expected default node=http://localhost:9200, got %v", esConn.nodes)
	}
	if esConn.timeout != 30*time.Second {
		t.Errorf("expected default timeout=30s, got %v", esConn.timeout)
	}
}

func TestFactorySupports(t *testing.T) {
	factory := NewFactory(nil)

	if !factory.Supports("elasticsearch", "") {
		t.Error("factory should support 'elasticsearch' type")
	}
	if factory.Supports("database", "") {
		t.Error("factory should not support 'database' type")
	}
	if factory.Supports("rest", "") {
		t.Error("factory should not support 'rest' type")
	}
}

func TestSearch(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "/_search") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 2},
				"hits": []interface{}{
					map[string]interface{}{
						"_id":    "1",
						"_score": 1.5,
						"_source": map[string]interface{}{
							"name":  "Product A",
							"price": 29.99,
						},
					},
					map[string]interface{}{
						"_id":    "2",
						"_score": 1.2,
						"_source": map[string]interface{}{
							"name":  "Product B",
							"price": 49.99,
						},
					},
				},
			},
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	result, err := conn.Read(context.Background(), connector.Query{
		Target:    "products",
		Operation: "search",
		RawQuery: map[string]interface{}{
			"query": map[string]interface{}{
				"match": map[string]interface{}{
					"name": "product",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Rows))
	}
	if result.Rows[0]["name"] != "Product A" {
		t.Errorf("expected name=Product A, got %v", result.Rows[0]["name"])
	}
	if result.Rows[0]["_id"] != "1" {
		t.Errorf("expected _id=1, got %v", result.Rows[0]["_id"])
	}
	if result.Affected != 2 {
		t.Errorf("expected total=2, got %d", result.Affected)
	}
}

func TestSearchWithFilters(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)

		// Verify bool/must query was built from filters
		query, _ := req["query"].(map[string]interface{})
		boolQ, _ := query["bool"].(map[string]interface{})
		must, _ := boolQ["must"].([]interface{})
		if len(must) != 1 {
			t.Errorf("expected 1 must clause, got %d", len(must))
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 1},
				"hits": []interface{}{
					map[string]interface{}{
						"_id":     "1",
						"_source": map[string]interface{}{"status": "active"},
					},
				},
			},
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	result, err := conn.Read(context.Background(), connector.Query{
		Target:    "products",
		Operation: "search",
		Filters:   map[string]interface{}{"status": "active"},
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(result.Rows))
	}
}

func TestSearchWithPaginationAndSort(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)

		// Verify pagination
		if req["size"] != float64(10) {
			t.Errorf("expected size=10, got %v", req["size"])
		}
		if req["from"] != float64(20) {
			t.Errorf("expected from=20, got %v", req["from"])
		}

		// Verify sort
		sort, ok := req["sort"].([]interface{})
		if !ok || len(sort) != 1 {
			t.Errorf("expected 1 sort clause, got %v", req["sort"])
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 50},
				"hits":  []interface{}{},
			},
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	_, err := conn.Read(context.Background(), connector.Query{
		Target:    "products",
		Operation: "search",
		Pagination: &connector.Pagination{
			Limit:  10,
			Offset: 20,
		},
		OrderBy: []connector.OrderClause{
			{Field: "price", Desc: true},
		},
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
}

func TestGetDocument(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.Contains(r.URL.Path, "/_doc/doc123") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"_id":    "doc123",
			"found":  true,
			"_source": map[string]interface{}{
				"name":  "Test",
				"email": "test@example.com",
			},
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	result, err := conn.Read(context.Background(), connector.Query{
		Target:    "users",
		Operation: "get",
		Filters:   map[string]interface{}{"id": "doc123"},
	})
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if result.Rows[0]["_id"] != "doc123" {
		t.Errorf("expected _id=doc123, got %v", result.Rows[0]["_id"])
	}
	if result.Rows[0]["name"] != "Test" {
		t.Errorf("expected name=Test, got %v", result.Rows[0]["name"])
	}
}

func TestGetDocumentNotFound(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"found": false,
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	result, err := conn.Read(context.Background(), connector.Query{
		Target:    "users",
		Operation: "get",
		Filters:   map[string]interface{}{"id": "nonexistent"},
	})
	if err != nil {
		t.Fatalf("get should not error on 404: %v", err)
	}
	if result.Rows != nil {
		t.Errorf("expected nil rows for not found, got %v", result.Rows)
	}
}

func TestGetDocumentMissingID(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	defer server.Close()

	conn := newTestConnector(t, server)
	_, err := conn.Read(context.Background(), connector.Query{
		Target:    "users",
		Operation: "get",
	})
	if err == nil {
		t.Error("expected error for missing id in get operation")
	}
}

func TestCount(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/_count") {
			t.Errorf("expected _count path, got %s", r.URL.Path)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"count": 42,
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	result, err := conn.Read(context.Background(), connector.Query{
		Target:    "products",
		Operation: "count",
		Filters:   map[string]interface{}{"status": "active"},
	})
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if result.Affected != 42 {
		t.Errorf("expected count=42, got %d", result.Affected)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	if result.Rows[0]["count"] != int64(42) {
		t.Errorf("expected count=42, got %v", result.Rows[0]["count"])
	}
}

func TestIndexDocument(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.Contains(r.URL.Path, "/_doc") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"_id":    "new_doc_1",
			"result": "created",
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	result, err := conn.Write(context.Background(), &connector.Data{
		Target:    "products",
		Operation: "index",
		Payload: map[string]interface{}{
			"name":  "New Product",
			"price": 19.99,
		},
	})
	if err != nil {
		t.Fatalf("index failed: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("expected affected=1, got %d", result.Affected)
	}
	if result.LastID != "new_doc_1" {
		t.Errorf("expected lastID=new_doc_1, got %v", result.LastID)
	}
}

func TestIndexDocumentWithID(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("expected PUT for index with ID, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/_doc/existing_1") {
			t.Errorf("expected path with doc ID, got %s", r.URL.Path)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"_id":    "existing_1",
			"result": "updated",
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	_, err := conn.Write(context.Background(), &connector.Data{
		Target:    "products",
		Operation: "index",
		Payload:   map[string]interface{}{"name": "Updated"},
		Filters:   map[string]interface{}{"id": "existing_1"},
	})
	if err != nil {
		t.Fatalf("index with ID failed: %v", err)
	}
}

func TestUpdateDocument(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/_update/doc1") {
			t.Errorf("expected _update path, got %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		if _, ok := req["doc"]; !ok {
			t.Error("expected 'doc' wrapper in update body")
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": "updated",
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	result, err := conn.Write(context.Background(), &connector.Data{
		Target:    "products",
		Operation: "update",
		Payload:   map[string]interface{}{"price": 39.99},
		Filters:   map[string]interface{}{"id": "doc1"},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("expected affected=1, got %d", result.Affected)
	}
}

func TestUpdateMissingID(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	defer server.Close()

	conn := newTestConnector(t, server)
	_, err := conn.Write(context.Background(), &connector.Data{
		Target:    "products",
		Operation: "update",
		Payload:   map[string]interface{}{"price": 39.99},
	})
	if err == nil {
		t.Error("expected error for missing id in update")
	}
}

func TestDeleteDocument(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE method, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/_doc/doc1") {
			t.Errorf("expected _doc path with ID, got %s", r.URL.Path)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": "deleted",
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	result, err := conn.Write(context.Background(), &connector.Data{
		Target:    "products",
		Operation: "delete",
		Filters:   map[string]interface{}{"id": "doc1"},
	})
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("expected affected=1, got %d", result.Affected)
	}
}

func TestDeleteMissingID(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	defer server.Close()

	conn := newTestConnector(t, server)
	_, err := conn.Write(context.Background(), &connector.Data{
		Target:    "products",
		Operation: "delete",
	})
	if err == nil {
		t.Error("expected error for missing id in delete")
	}
}

func TestBulkOperation(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/_bulk" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		lines := strings.Split(strings.TrimSpace(string(body)), "\n")
		// 2 items: index (2 lines each) = 4 lines
		if len(lines) != 4 {
			t.Errorf("expected 4 NDJSON lines, got %d: %s", len(lines), string(body))
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": false,
			"items":  []interface{}{},
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	result, err := conn.Write(context.Background(), &connector.Data{
		Target:    "products",
		Operation: "bulk",
		Payload: map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{"_id": "1", "name": "Product A"},
				map[string]interface{}{"_id": "2", "name": "Product B"},
			},
		},
	})
	if err != nil {
		t.Fatalf("bulk failed: %v", err)
	}
	if result.Affected != 2 {
		t.Errorf("expected affected=2, got %d", result.Affected)
	}
}

func TestBulkMissingItems(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	defer server.Close()

	conn := newTestConnector(t, server)
	_, err := conn.Write(context.Background(), &connector.Data{
		Target:    "products",
		Operation: "bulk",
		Payload:   map[string]interface{}{},
	})
	if err == nil {
		t.Error("expected error for missing items in bulk")
	}
}

func TestBasicAuth(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "elastic" || pass != "secret" {
			t.Errorf("expected basic auth elastic:secret, got %s:%s (ok=%v)", user, pass, ok)
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 0},
				"hits":  []interface{}{},
			},
		})
	})
	defer server.Close()

	config := &Config{
		Nodes:    []string{server.URL},
		Username: "elastic",
		Password: "secret",
		Index:    "test",
		Timeout:  5 * time.Second,
	}
	conn := New("auth_es", config, nil)

	_, err := conn.Read(context.Background(), connector.Query{
		Target:    "test",
		Operation: "search",
	})
	if err != nil {
		t.Fatalf("search with auth failed: %v", err)
	}
}

func TestNodeRoundRobin(t *testing.T) {
	callCounts := make(map[string]int)
	server1 := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCounts["server1"]++
		json.NewEncoder(w).Encode(map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 0},
				"hits":  []interface{}{},
			},
		})
	})
	defer server1.Close()

	server2 := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCounts["server2"]++
		json.NewEncoder(w).Encode(map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 0},
				"hits":  []interface{}{},
			},
		})
	})
	defer server2.Close()

	config := &Config{
		Nodes:   []string{server1.URL, server2.URL},
		Index:   "test",
		Timeout: 5 * time.Second,
	}
	conn := New("rr_es", config, nil)

	for i := 0; i < 4; i++ {
		conn.Read(context.Background(), connector.Query{
			Target:    "test",
			Operation: "search",
		})
	}

	if callCounts["server1"] != 2 || callCounts["server2"] != 2 {
		t.Errorf("expected round-robin (2,2), got server1=%d server2=%d",
			callCounts["server1"], callCounts["server2"])
	}
}

func TestHealth(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_cluster/health" {
			t.Errorf("expected /_cluster/health, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "green",
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	if err := conn.Health(context.Background()); err != nil {
		t.Errorf("health check failed: %v", err)
	}
}

func TestHealthUnhealthy(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("cluster unavailable"))
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	if err := conn.Health(context.Background()); err == nil {
		t.Error("expected health check to fail")
	}
}

func TestSearchError(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"type":   "parsing_exception",
				"reason": "invalid query",
			},
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	_, err := conn.Read(context.Background(), connector.Query{
		Target:    "products",
		Operation: "search",
	})
	if err == nil {
		t.Error("expected error for bad request")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected status 400 in error, got %v", err)
	}
}

func TestNoIndex(t *testing.T) {
	config := &Config{
		Nodes:   []string{"http://localhost:9200"},
		Timeout: 5 * time.Second,
	}
	conn := New("no_index", config, nil)

	_, err := conn.Read(context.Background(), connector.Query{
		Operation: "search",
	})
	if err == nil {
		t.Error("expected error for missing index")
	}
}

func TestUnsupportedReadOperation(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	defer server.Close()

	conn := newTestConnector(t, server)
	_, err := conn.Read(context.Background(), connector.Query{
		Target:    "test",
		Operation: "invalid_op",
	})
	if err == nil {
		t.Error("expected error for unsupported read operation")
	}
}

func TestUnsupportedWriteOperation(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	defer server.Close()

	conn := newTestConnector(t, server)
	_, err := conn.Write(context.Background(), &connector.Data{
		Target:    "test",
		Operation: "invalid_op",
	})
	if err == nil {
		t.Error("expected error for unsupported write operation")
	}
}

func TestDefaultSearchOperation(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/_search") {
			t.Errorf("expected _search path, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 0},
				"hits":  []interface{}{},
			},
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	// Empty operation should default to "search"
	_, err := conn.Read(context.Background(), connector.Query{
		Target: "test",
	})
	if err != nil {
		t.Fatalf("default search failed: %v", err)
	}
}

func TestFieldSelection(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)

		source, ok := req["_source"].([]interface{})
		if !ok || len(source) != 2 {
			t.Errorf("expected _source=[name,price], got %v", req["_source"])
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 0},
				"hits":  []interface{}{},
			},
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	_, err := conn.Read(context.Background(), connector.Query{
		Target: "test",
		Fields: []string{"name", "price"},
	})
	if err != nil {
		t.Fatalf("search with fields failed: %v", err)
	}
}

func TestConnectClose(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "green"})
	})
	defer server.Close()

	conn := newTestConnector(t, server)

	if err := conn.Connect(context.Background()); err != nil {
		t.Errorf("connect failed: %v", err)
	}
	if err := conn.Close(context.Background()); err != nil {
		t.Errorf("close failed: %v", err)
	}
}

func TestAggregate(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)

		if req["size"] != float64(0) {
			t.Errorf("expected size=0 for aggregation, got %v", req["size"])
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"hits": map[string]interface{}{
				"total": map[string]interface{}{"value": 100},
				"hits":  []interface{}{},
			},
			"aggregations": map[string]interface{}{
				"avg_price": map[string]interface{}{
					"value": 29.99,
				},
			},
		})
	})
	defer server.Close()

	conn := newTestConnector(t, server)
	result, err := conn.Read(context.Background(), connector.Query{
		Target:    "products",
		Operation: "aggregate",
		RawQuery: map[string]interface{}{
			"aggs": map[string]interface{}{
				"avg_price": map[string]interface{}{
					"avg": map[string]interface{}{"field": "price"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("aggregate failed: %v", err)
	}

	if result.Metadata == nil {
		t.Fatal("expected metadata with aggregations")
	}
	aggs, ok := result.Metadata["aggregations"].(map[string]interface{})
	if !ok {
		t.Fatal("expected aggregations in metadata")
	}
	avgPrice, ok := aggs["avg_price"].(map[string]interface{})
	if !ok {
		t.Fatal("expected avg_price aggregation")
	}
	if avgPrice["value"] != 29.99 {
		t.Errorf("expected avg_price=29.99, got %v", avgPrice["value"])
	}
}
