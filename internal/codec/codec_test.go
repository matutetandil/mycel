package codec

import (
	"context"
	"strings"
	"testing"
)

// --- JSON Codec Tests ---

func TestJSONCodecRoundTrip(t *testing.T) {
	c := &JSONCodec{}

	input := map[string]interface{}{
		"name":  "Alice",
		"age":   float64(30),
		"email": "alice@example.com",
	}

	encoded, err := c.Encode(input)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	decoded, err := c.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if decoded["name"] != "Alice" {
		t.Errorf("expected name=Alice, got %v", decoded["name"])
	}
	if decoded["email"] != "alice@example.com" {
		t.Errorf("expected email=alice@example.com, got %v", decoded["email"])
	}
}

func TestJSONCodecContentType(t *testing.T) {
	c := &JSONCodec{}
	if ct := c.ContentType(); ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
	if name := c.Name(); name != "json" {
		t.Errorf("expected json, got %s", name)
	}
}

// --- XML Codec Tests ---

func TestXMLCodecEncodeFlatMap(t *testing.T) {
	c := &XMLCodec{RootElement: "user"}

	input := map[string]interface{}{
		"name":  "Alice",
		"email": "alice@example.com",
	}

	encoded, err := c.Encode(input)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	s := string(encoded)
	if !strings.Contains(s, "<user>") {
		t.Errorf("expected <user> root element, got: %s", s)
	}
	if !strings.Contains(s, "<name>Alice</name>") {
		t.Errorf("expected <name>Alice</name>, got: %s", s)
	}
	if !strings.Contains(s, "<email>alice@example.com</email>") {
		t.Errorf("expected <email> element, got: %s", s)
	}
}

func TestXMLCodecEncodeNestedMap(t *testing.T) {
	c := &XMLCodec{RootElement: "order"}

	input := map[string]interface{}{
		"id": "123",
		"customer": map[string]interface{}{
			"name":  "Bob",
			"email": "bob@example.com",
		},
	}

	encoded, err := c.Encode(input)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	s := string(encoded)
	if !strings.Contains(s, "<customer>") {
		t.Errorf("expected nested <customer>, got: %s", s)
	}
	if !strings.Contains(s, "<name>Bob</name>") {
		t.Errorf("expected <name>Bob</name>, got: %s", s)
	}
}

func TestXMLCodecEncodeArray(t *testing.T) {
	c := &XMLCodec{RootElement: "data"}

	input := map[string]interface{}{
		"item": []interface{}{"a", "b", "c"},
	}

	encoded, err := c.Encode(input)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	s := string(encoded)
	count := strings.Count(s, "<item>")
	if count != 3 {
		t.Errorf("expected 3 <item> elements, got %d in: %s", count, s)
	}
}

func TestXMLCodecEncodeAttributes(t *testing.T) {
	c := &XMLCodec{RootElement: "product"}

	input := map[string]interface{}{
		"@id":   "42",
		"#text": "Widget",
	}

	encoded, err := c.Encode(input)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	s := string(encoded)
	if !strings.Contains(s, `id="42"`) {
		t.Errorf("expected id attribute, got: %s", s)
	}
	if !strings.Contains(s, "Widget") {
		t.Errorf("expected Widget text, got: %s", s)
	}
}

func TestXMLCodecDecodeFlatElements(t *testing.T) {
	c := &XMLCodec{}

	xmlData := `<user><name>Alice</name><email>alice@example.com</email></user>`

	decoded, err := c.Decode([]byte(xmlData))
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if decoded["name"] != "Alice" {
		t.Errorf("expected name=Alice, got %v", decoded["name"])
	}
	if decoded["email"] != "alice@example.com" {
		t.Errorf("expected email, got %v", decoded["email"])
	}
}

func TestXMLCodecDecodeNestedElements(t *testing.T) {
	c := &XMLCodec{}

	xmlData := `<order><id>123</id><customer><name>Bob</name></customer></order>`

	decoded, err := c.Decode([]byte(xmlData))
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	customer, ok := decoded["customer"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected customer as map, got %T", decoded["customer"])
	}
	if customer["name"] != "Bob" {
		t.Errorf("expected customer.name=Bob, got %v", customer["name"])
	}
}

func TestXMLCodecDecodeRepeatedElements(t *testing.T) {
	c := &XMLCodec{}

	xmlData := `<data><item>a</item><item>b</item><item>c</item></data>`

	decoded, err := c.Decode([]byte(xmlData))
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	items, ok := decoded["item"].([]interface{})
	if !ok {
		t.Fatalf("expected item as slice, got %T", decoded["item"])
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
	if items[0] != "a" || items[1] != "b" || items[2] != "c" {
		t.Errorf("expected [a, b, c], got %v", items)
	}
}

func TestXMLCodecDecodeAttributes(t *testing.T) {
	c := &XMLCodec{}

	xmlData := `<product id="42" category="widgets"><name>Widget</name></product>`

	decoded, err := c.Decode([]byte(xmlData))
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if decoded["@id"] != "42" {
		t.Errorf("expected @id=42, got %v", decoded["@id"])
	}
	if decoded["@category"] != "widgets" {
		t.Errorf("expected @category=widgets, got %v", decoded["@category"])
	}
	if decoded["name"] != "Widget" {
		t.Errorf("expected name=Widget, got %v", decoded["name"])
	}
}

func TestXMLCodecDecodeTextWithAttributes(t *testing.T) {
	c := &XMLCodec{}

	xmlData := `<price currency="USD">19.99</price>`

	decoded, err := c.Decode([]byte(xmlData))
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if decoded["@currency"] != "USD" {
		t.Errorf("expected @currency=USD, got %v", decoded["@currency"])
	}
	if decoded["#text"] != "19.99" {
		t.Errorf("expected #text=19.99, got %v", decoded["#text"])
	}
}

func TestXMLCodecRoundTrip(t *testing.T) {
	c := &XMLCodec{RootElement: "order"}

	input := map[string]interface{}{
		"id":     "ORD-001",
		"status": "pending",
	}

	encoded, err := c.Encode(input)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	decoded, err := c.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}

	if decoded["id"] != "ORD-001" {
		t.Errorf("expected id=ORD-001, got %v", decoded["id"])
	}
	if decoded["status"] != "pending" {
		t.Errorf("expected status=pending, got %v", decoded["status"])
	}
}

// --- Registry Tests ---

func TestRegistryGet(t *testing.T) {
	c := Get("json")
	if c.Name() != "json" {
		t.Errorf("expected json codec, got %s", c.Name())
	}

	c = Get("xml")
	if c.Name() != "xml" {
		t.Errorf("expected xml codec, got %s", c.Name())
	}
}

func TestRegistryGetDefault(t *testing.T) {
	c := Get("")
	if c.Name() != "json" {
		t.Errorf("expected json default, got %s", c.Name())
	}

	c = Get("unknown")
	if c.Name() != "json" {
		t.Errorf("expected json fallback, got %s", c.Name())
	}
}

func TestDetectFromContentType(t *testing.T) {
	tests := []struct {
		contentType string
		expected    string
	}{
		{"application/json", "json"},
		{"application/json; charset=utf-8", "json"},
		{"text/json", "json"},
		{"application/xml", "xml"},
		{"text/xml", "xml"},
		{"text/xml; charset=utf-8", "xml"},
		{"application/soap+xml", "xml"},
		{"application/vnd.api+json", "json"},
		{"text/plain", "json"}, // fallback
		{"", "json"},           // empty
	}

	for _, tt := range tests {
		c := DetectFromContentType(tt.contentType)
		if c.Name() != tt.expected {
			t.Errorf("DetectFromContentType(%q) = %s, want %s", tt.contentType, c.Name(), tt.expected)
		}
	}
}

// --- Context Tests ---

func TestFormatContext(t *testing.T) {
	ctx := context.Background()

	// No format set
	if f := FormatFromContext(ctx); f != "" {
		t.Errorf("expected empty format, got %s", f)
	}

	// Set format
	ctx = WithFormat(ctx, "xml")
	if f := FormatFromContext(ctx); f != "xml" {
		t.Errorf("expected xml, got %s", f)
	}
}

func TestXMLCodecContentType(t *testing.T) {
	c := &XMLCodec{}
	if ct := c.ContentType(); ct != "application/xml" {
		t.Errorf("expected application/xml, got %s", ct)
	}
	if name := c.Name(); name != "xml" {
		t.Errorf("expected xml, got %s", name)
	}
}

func TestXMLCodecEncodeSpecialChars(t *testing.T) {
	c := &XMLCodec{RootElement: "data"}

	input := map[string]interface{}{
		"message": "Price < $10 & tax > 0",
	}

	encoded, err := c.Encode(input)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	s := string(encoded)
	if strings.Contains(s, "< $") {
		t.Errorf("expected escaped < character, got: %s", s)
	}
	if strings.Contains(s, "& tax") {
		t.Errorf("expected escaped & character, got: %s", s)
	}
}
