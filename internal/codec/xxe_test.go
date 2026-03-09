package codec

import (
	"testing"
)

// TestXXE_EntityExpansionBlocked verifies that the XML codec
// blocks entity expansion at the decoder level.
func TestXXE_EntityExpansionBlocked(t *testing.T) {
	c := &XMLCodec{}

	tests := []struct {
		name  string
		input string
	}{
		{
			name: "external entity file read",
			input: `<?xml version="1.0"?>
<!DOCTYPE foo [
  <!ENTITY xxe SYSTEM "file:///etc/passwd">
]>
<root>&xxe;</root>`,
		},
		{
			name: "external entity SSRF",
			input: `<?xml version="1.0"?>
<!DOCTYPE foo [
  <!ENTITY xxe SYSTEM "http://169.254.169.254/latest/meta-data/">
]>
<root>&xxe;</root>`,
		},
		{
			name: "billion laughs (XML bomb)",
			input: `<?xml version="1.0"?>
<!DOCTYPE lolz [
  <!ENTITY lol "lol">
  <!ENTITY lol2 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;">
  <!ENTITY lol3 "&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;">
]>
<root>&lol3;</root>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.Decode([]byte(tt.input))
			if err == nil {
				t.Fatal("XXE payload should cause a decode error or produce safe output")
			}
			t.Logf("correctly blocked: %v", err)
		})
	}
}

// TestXXE_SafeXMLPassesThrough verifies that normal XML without
// entities decodes correctly after the XXE fix.
func TestXXE_SafeXMLPassesThrough(t *testing.T) {
	c := &XMLCodec{}

	input := `<?xml version="1.0" encoding="UTF-8"?>
<user>
  <name>John Doe</name>
  <email>john@example.com</email>
  <age>30</age>
</user>`

	result, err := c.Decode([]byte(input))
	if err != nil {
		t.Fatalf("safe XML should decode: %v", err)
	}

	if result["name"] != "John Doe" {
		t.Errorf("expected name 'John Doe', got %v", result["name"])
	}
	if result["email"] != "john@example.com" {
		t.Errorf("expected email 'john@example.com', got %v", result["email"])
	}
}

// TestXXE_SOAPEnvelopePassesThrough verifies that normal SOAP
// envelopes work after XXE protection.
func TestXXE_SOAPEnvelopePassesThrough(t *testing.T) {
	c := &XMLCodec{}

	input := `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUserResponse>
      <name>Jane</name>
      <id>42</id>
    </GetUserResponse>
  </soap:Body>
</soap:Envelope>`

	result, err := c.Decode([]byte(input))
	if err != nil {
		t.Fatalf("SOAP envelope should decode: %v", err)
	}

	// The SOAP structure should be parsed into nested maps
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
