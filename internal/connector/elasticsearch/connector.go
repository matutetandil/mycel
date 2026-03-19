// Package elasticsearch provides an Elasticsearch connector for full-text search and analytics.
// It supports search, indexing, updating, deleting, and bulk operations over Elasticsearch's REST API.
package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/matutetandil/mycel/internal/connector"
)

// Connector implements Elasticsearch operations via its REST API.
type Connector struct {
	name     string
	nodes    []string
	username string
	password string
	index    string
	timeout  time.Duration
	client   *http.Client
	logger   *slog.Logger
	nodeIdx  uint64
}

// New creates a new Elasticsearch connector.
func New(name string, config *Config, logger *slog.Logger) *Connector {
	if logger == nil {
		logger = slog.Default()
	}
	return &Connector{
		name:     name,
		nodes:    config.Nodes,
		username: config.Username,
		password: config.Password,
		index:    config.Index,
		timeout:  config.Timeout,
		client:   &http.Client{Timeout: config.Timeout},
		logger:   logger,
	}
}

// Name returns the connector name.
func (c *Connector) Name() string { return c.name }

// Type returns the connector type.
func (c *Connector) Type() string { return "elasticsearch" }

// Connect verifies connectivity to the Elasticsearch cluster.
func (c *Connector) Connect(ctx context.Context) error {
	if err := c.Health(ctx); err != nil {
		return err
	}
	c.logger.Info("elasticsearch connected",
		"nodes", c.nodes,
		"default_index", c.index,
	)
	return nil
}

// Close is a no-op for Elasticsearch (HTTP-based, no persistent connection).
func (c *Connector) Close(ctx context.Context) error { return nil }

// Health checks cluster health via GET /_cluster/health.
func (c *Connector) Health(ctx context.Context) error {
	resp, err := c.doRequest(ctx, "GET", "/_cluster/health", nil)
	if err != nil {
		return fmt.Errorf("elasticsearch health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("elasticsearch health check returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// Read executes a read operation against Elasticsearch.
// Supported operations: search, get, count, aggregate.
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	index := query.Target
	if index == "" {
		index = c.index
	}
	if index == "" {
		return nil, fmt.Errorf("no index specified (set target or default index)")
	}

	operation := strings.ToLower(query.Operation)
	if operation == "" {
		operation = "search"
	}

	switch operation {
	case "search":
		return c.search(ctx, index, query)
	case "get":
		return c.getDocument(ctx, index, query)
	case "count":
		return c.count(ctx, index, query)
	case "aggregate":
		return c.aggregate(ctx, index, query)
	default:
		return nil, fmt.Errorf("unsupported read operation: %s", operation)
	}
}

// Write executes a write operation against Elasticsearch.
// Supported operations: index, update, delete, bulk.
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	index := data.Target
	if index == "" {
		index = c.index
	}
	if index == "" {
		return nil, fmt.Errorf("no index specified (set target or default index)")
	}

	operation := strings.ToLower(data.Operation)
	if operation == "" {
		operation = "index"
	}

	switch operation {
	case "index":
		return c.indexDocument(ctx, index, data)
	case "update":
		return c.updateDocument(ctx, index, data)
	case "delete":
		return c.deleteDocument(ctx, index, data)
	case "bulk":
		return c.bulkOperation(ctx, index, data)
	default:
		return nil, fmt.Errorf("unsupported write operation: %s", operation)
	}
}

// search executes a search query against an index.
func (c *Connector) search(ctx context.Context, index string, query connector.Query) (*connector.Result, error) {
	body := buildSearchBody(query)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search body: %w", err)
	}

	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/%s/_search", index), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	return parseSearchResponse(resp)
}

// getDocument retrieves a single document by ID.
func (c *Connector) getDocument(ctx context.Context, index string, query connector.Query) (*connector.Result, error) {
	docID := ""
	if query.Filters != nil {
		if id, ok := query.Filters["id"].(string); ok {
			docID = id
		}
	}
	if docID == "" {
		return nil, fmt.Errorf("get operation requires 'id' in filters")
	}

	path := fmt.Sprintf("/%s/_doc/%s", index, docID)
	if len(query.Fields) > 0 {
		path += "?_source_includes=" + strings.Join(query.Fields, ",")
	}

	resp, err := c.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("get request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &connector.Result{Rows: nil}, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read get response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(respBody, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse get response: %w", err)
	}

	source, _ := doc["_source"].(map[string]interface{})
	if source == nil {
		return &connector.Result{Rows: nil}, nil
	}

	// Include _id in the result
	source["_id"] = doc["_id"]

	return &connector.Result{
		Rows: []map[string]interface{}{source},
	}, nil
}

// count returns the number of documents matching a query.
func (c *Connector) count(ctx context.Context, index string, query connector.Query) (*connector.Result, error) {
	body := make(map[string]interface{})
	if query.RawQuery != nil {
		body["query"] = query.RawQuery
	} else if len(query.Filters) > 0 {
		body["query"] = buildBoolMustQuery(query.Filters)
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal count body: %w", err)
	}

	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/%s/_count", index), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("count request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read count response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("count failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse count response: %w", err)
	}

	count := int64(0)
	if c, ok := result["count"].(float64); ok {
		count = int64(c)
	}

	return &connector.Result{
		Rows:     []map[string]interface{}{{"count": count}},
		Affected: count,
	}, nil
}

// aggregate executes an aggregation query.
func (c *Connector) aggregate(ctx context.Context, index string, query connector.Query) (*connector.Result, error) {
	body := make(map[string]interface{})
	body["size"] = 0

	if query.RawQuery != nil {
		// RawQuery should contain the full body with "aggs" or "aggregations"
		for k, v := range query.RawQuery {
			body[k] = v
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal aggregate body: %w", err)
	}

	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/%s/_search", index), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("aggregate request failed: %w", err)
	}
	defer resp.Body.Close()

	return parseSearchResponse(resp)
}

// indexDocument indexes (creates or replaces) a document.
func (c *Connector) indexDocument(ctx context.Context, index string, data *connector.Data) (*connector.Result, error) {
	docID := ""
	if data.Filters != nil {
		if id, ok := data.Filters["id"].(string); ok {
			docID = id
		}
	}

	jsonBody, err := json.Marshal(data.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal document: %w", err)
	}

	path := fmt.Sprintf("/%s/_doc", index)
	method := "POST"
	if docID != "" {
		path = fmt.Sprintf("/%s/_doc/%s", index, docID)
		method = "PUT"
	}

	resp, err := c.doRequest(ctx, method, path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("index request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read index response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("index failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse index response: %w", err)
	}

	return &connector.Result{
		Affected: 1,
		LastID:   result["_id"],
	}, nil
}

// updateDocument performs a partial update on a document.
func (c *Connector) updateDocument(ctx context.Context, index string, data *connector.Data) (*connector.Result, error) {
	docID := ""
	if data.Filters != nil {
		if id, ok := data.Filters["id"].(string); ok {
			docID = id
		}
	}
	if docID == "" {
		return nil, fmt.Errorf("update operation requires 'id' in filters")
	}

	body := map[string]interface{}{
		"doc": data.Payload,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal update body: %w", err)
	}

	resp, err := c.doRequest(ctx, "POST", fmt.Sprintf("/%s/_update/%s", index, docID), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("update request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read update response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return &connector.Result{Affected: 1}, nil
}

// deleteDocument deletes a document by ID.
func (c *Connector) deleteDocument(ctx context.Context, index string, data *connector.Data) (*connector.Result, error) {
	docID := ""
	if data.Filters != nil {
		if id, ok := data.Filters["id"].(string); ok {
			docID = id
		}
	}
	if docID == "" {
		return nil, fmt.Errorf("delete operation requires 'id' in filters")
	}

	resp, err := c.doRequest(ctx, "DELETE", fmt.Sprintf("/%s/_doc/%s", index, docID), nil)
	if err != nil {
		return nil, fmt.Errorf("delete request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read delete response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("delete failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse delete response: %w", err)
	}

	affected := int64(0)
	if r, ok := result["result"].(string); ok && r == "deleted" {
		affected = 1
	}

	return &connector.Result{Affected: affected}, nil
}

// bulkOperation performs bulk index/update/delete operations.
func (c *Connector) bulkOperation(ctx context.Context, index string, data *connector.Data) (*connector.Result, error) {
	items, ok := data.Payload["items"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("bulk operation requires 'items' array in payload")
	}

	var buf bytes.Buffer
	for _, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		action := "index"
		if a, ok := itemMap["_action"].(string); ok {
			action = a
		}
		docID, _ := itemMap["_id"].(string)

		// Remove meta fields from the document
		doc := make(map[string]interface{})
		for k, v := range itemMap {
			if k != "_action" && k != "_id" {
				doc[k] = v
			}
		}

		// Write action line
		meta := map[string]interface{}{
			"_index": index,
		}
		if docID != "" {
			meta["_id"] = docID
		}

		actionLine := map[string]interface{}{action: meta}
		actionJSON, _ := json.Marshal(actionLine)
		buf.Write(actionJSON)
		buf.WriteByte('\n')

		// Write document line (except for delete)
		if action != "delete" {
			if action == "update" {
				doc = map[string]interface{}{"doc": doc}
			}
			docJSON, _ := json.Marshal(doc)
			buf.Write(docJSON)
			buf.WriteByte('\n')
		}
	}

	resp, err := c.doRequest(ctx, "POST", "/_bulk", &buf)
	if err != nil {
		return nil, fmt.Errorf("bulk request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read bulk response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bulk failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse bulk response: %w", err)
	}

	affected := int64(len(items))
	return &connector.Result{
		Affected: affected,
		Metadata: result,
	}, nil
}

// doRequest executes an HTTP request against the next node in round-robin order.
func (c *Connector) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	idx := atomic.AddUint64(&c.nodeIdx, 1)
	node := c.nodes[int(idx)%len(c.nodes)]
	node = strings.TrimSuffix(node, "/")

	url := node + path

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	return c.client.Do(req)
}

// buildSearchBody constructs the search request body from a Query.
func buildSearchBody(query connector.Query) map[string]interface{} {
	body := make(map[string]interface{})

	// Use raw query if provided
	if query.RawQuery != nil {
		for k, v := range query.RawQuery {
			body[k] = v
		}
	} else if len(query.Filters) > 0 {
		body["query"] = buildBoolMustQuery(query.Filters)
	}

	// Field selection
	if len(query.Fields) > 0 {
		body["_source"] = query.Fields
	}

	// Pagination
	if query.Pagination != nil {
		if query.Pagination.Limit > 0 {
			body["size"] = query.Pagination.Limit
		}
		if query.Pagination.Offset > 0 {
			body["from"] = query.Pagination.Offset
		}
	}

	// Sorting
	if len(query.OrderBy) > 0 {
		sort := make([]map[string]interface{}, 0, len(query.OrderBy))
		for _, ob := range query.OrderBy {
			order := "asc"
			if ob.Desc {
				order = "desc"
			}
			sort = append(sort, map[string]interface{}{
				ob.Field: map[string]interface{}{"order": order},
			})
		}
		body["sort"] = sort
	}

	return body
}

// buildBoolMustQuery converts simple filters to a bool/must term query.
func buildBoolMustQuery(filters map[string]interface{}) map[string]interface{} {
	must := make([]interface{}, 0, len(filters))
	for field, value := range filters {
		must = append(must, map[string]interface{}{
			"term": map[string]interface{}{field: value},
		})
	}
	return map[string]interface{}{
		"bool": map[string]interface{}{
			"must": must,
		},
	}
}

// parseSearchResponse parses a _search response into a Result.
func parseSearchResponse(resp *http.Response) (*connector.Result, error) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read search response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var esResp map[string]interface{}
	if err := json.Unmarshal(respBody, &esResp); err != nil {
		return nil, fmt.Errorf("failed to parse search response: %w", err)
	}

	hits, _ := esResp["hits"].(map[string]interface{})
	hitList, _ := hits["hits"].([]interface{})

	rows := make([]map[string]interface{}, 0, len(hitList))
	for _, hit := range hitList {
		hitMap, ok := hit.(map[string]interface{})
		if !ok {
			continue
		}
		source, _ := hitMap["_source"].(map[string]interface{})
		if source == nil {
			source = make(map[string]interface{})
		}
		// Include _id and _score
		source["_id"] = hitMap["_id"]
		if score, ok := hitMap["_score"]; ok {
			source["_score"] = score
		}
		rows = append(rows, source)
	}

	result := &connector.Result{Rows: rows}

	// Include total count in metadata
	if total, ok := hits["total"].(map[string]interface{}); ok {
		if val, ok := total["value"].(float64); ok {
			result.Affected = int64(val)
		}
	}

	// Include aggregations if present
	if aggs, ok := esResp["aggregations"]; ok {
		result.Metadata = map[string]interface{}{
			"aggregations": aggs,
		}
	}

	return result, nil
}
