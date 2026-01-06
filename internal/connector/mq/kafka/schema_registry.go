package kafka

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// SchemaRegistryClient handles communication with Schema Registry.
type SchemaRegistryClient struct {
	url        string
	username   string
	password   string
	httpClient *http.Client

	// Cache for schemas
	mu          sync.RWMutex
	schemaCache map[int]string    // ID -> schema
	idCache     map[string]int    // subject-version -> ID
	subjectLock map[string]*sync.Mutex
}

// NewSchemaRegistryClient creates a new Schema Registry client.
func NewSchemaRegistryClient(config *SchemaRegistryConfig) *SchemaRegistryClient {
	return &SchemaRegistryClient{
		url:         config.URL,
		username:    config.Username,
		password:    config.Password,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		schemaCache: make(map[int]string),
		idCache:     make(map[string]int),
		subjectLock: make(map[string]*sync.Mutex),
	}
}

// RegisterSchema registers a schema and returns its ID.
func (c *SchemaRegistryClient) RegisterSchema(subject, schema string) (int, error) {
	c.mu.Lock()
	if _, ok := c.subjectLock[subject]; !ok {
		c.subjectLock[subject] = &sync.Mutex{}
	}
	lock := c.subjectLock[subject]
	c.mu.Unlock()

	lock.Lock()
	defer lock.Unlock()

	// Check cache first
	cacheKey := subject + ":latest"
	c.mu.RLock()
	if id, ok := c.idCache[cacheKey]; ok {
		c.mu.RUnlock()
		return id, nil
	}
	c.mu.RUnlock()

	// Register with Schema Registry
	body := map[string]string{"schema": schema}
	data, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal schema: %w", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/subjects/%s/versions", c.url, subject), bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/vnd.schemaregistry.v1+json")
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("schema registry request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("schema registry error %d: %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	// Cache the result
	c.mu.Lock()
	c.idCache[cacheKey] = result.ID
	c.schemaCache[result.ID] = schema
	c.mu.Unlock()

	return result.ID, nil
}

// GetSchemaByID retrieves a schema by its ID.
func (c *SchemaRegistryClient) GetSchemaByID(id int) (string, error) {
	// Check cache
	c.mu.RLock()
	if schema, ok := c.schemaCache[id]; ok {
		c.mu.RUnlock()
		return schema, nil
	}
	c.mu.RUnlock()

	// Fetch from Schema Registry
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/schemas/ids/%d", c.url, id), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("schema registry request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return "", fmt.Errorf("schema ID %d not found", id)
	}
	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("schema registry error %d: %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		Schema string `json:"schema"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	// Cache the result
	c.mu.Lock()
	c.schemaCache[id] = result.Schema
	c.mu.Unlock()

	return result.Schema, nil
}

// GetLatestSchema retrieves the latest schema for a subject.
func (c *SchemaRegistryClient) GetLatestSchema(subject string) (int, string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/subjects/%s/versions/latest", c.url, subject), nil)
	if err != nil {
		return 0, "", fmt.Errorf("failed to create request: %w", err)
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("schema registry request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return 0, "", fmt.Errorf("subject %s not found", subject)
	}
	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		return 0, "", fmt.Errorf("schema registry error %d: %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		ID     int    `json:"id"`
		Schema string `json:"schema"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, "", fmt.Errorf("failed to decode response: %w", err)
	}

	// Cache the result
	c.mu.Lock()
	c.schemaCache[result.ID] = result.Schema
	c.idCache[subject+":latest"] = result.ID
	c.mu.Unlock()

	return result.ID, result.Schema, nil
}

// CheckCompatibility checks if a schema is compatible with the existing versions.
func (c *SchemaRegistryClient) CheckCompatibility(subject, schema string) (bool, error) {
	body := map[string]string{"schema": schema}
	data, err := json.Marshal(body)
	if err != nil {
		return false, fmt.Errorf("failed to marshal schema: %w", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/compatibility/subjects/%s/versions/latest", c.url, subject), bytes.NewReader(data))
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/vnd.schemaregistry.v1+json")
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("schema registry request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		// No existing schema, so it's compatible
		return true, nil
	}
	if resp.StatusCode >= 400 && resp.StatusCode != 409 {
		errBody, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("schema registry error %d: %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		IsCompatible bool `json:"is_compatible"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.IsCompatible, nil
}

// EncodeWithSchemaID encodes a message with the Schema Registry wire format.
// Format: [magic byte (0)] [4-byte schema ID] [message payload]
func EncodeWithSchemaID(schemaID int, payload []byte) []byte {
	result := make([]byte, 5+len(payload))
	result[0] = 0 // Magic byte
	binary.BigEndian.PutUint32(result[1:5], uint32(schemaID))
	copy(result[5:], payload)
	return result
}

// DecodeSchemaID extracts the schema ID from a Schema Registry wire format message.
// Returns the schema ID and the actual payload.
func DecodeSchemaID(data []byte) (int, []byte, error) {
	if len(data) < 5 {
		return 0, nil, fmt.Errorf("message too short for schema registry wire format")
	}
	if data[0] != 0 {
		return 0, nil, fmt.Errorf("invalid magic byte: %d (expected 0)", data[0])
	}
	schemaID := int(binary.BigEndian.Uint32(data[1:5]))
	return schemaID, data[5:], nil
}

// GetSubjectName generates a subject name based on the naming strategy.
func GetSubjectName(topic, recordName, strategy string, isKey bool) string {
	suffix := "-value"
	if isKey {
		suffix = "-key"
	}

	switch strategy {
	case "record":
		return recordName + suffix
	case "topic_record":
		return topic + "-" + recordName + suffix
	case "topic", "":
		return topic + suffix
	default:
		return topic + suffix
	}
}
