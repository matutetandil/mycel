package soap

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnwrap_XXE_EntityRefNotExpanded(t *testing.T) {
	// With Strict=false, Go's XML decoder passes unknown entity refs
	// through as literal text "&xxe;" — the entity is NOT expanded,
	// so no file read or SSRF occurs. This is safe behavior.
	input := `<?xml version="1.0"?>
<!DOCTYPE foo [
  <!ENTITY xxe SYSTEM "file:///etc/passwd">
]>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUser>&xxe;</GetUser>
  </soap:Body>
</soap:Envelope>`

	op, body, fault, err := Unwrap([]byte(input))
	if err != nil {
		// If the decoder errors, that's also safe — entity was blocked
		t.Logf("decoder correctly rejected entity ref: %v", err)
		return
	}
	if fault != nil {
		t.Fatalf("unexpected fault: %v", fault)
	}
	if op != "GetUser" {
		t.Errorf("expected operation 'GetUser', got %q", op)
	}
	// The key check: the value must NOT contain /etc/passwd contents.
	// It should either be empty, literal "&xxe;", or the element text.
	val := fmt.Sprintf("%v", body)
	if strings.Contains(val, "root:") || strings.Contains(val, "/bin/") {
		t.Fatal("XXE entity was expanded — file contents leaked!")
	}
	t.Logf("entity ref safely passed through without expansion: body=%v", body)
}

func TestUnwrap_XXE_DTDOnlyPassesSafely(t *testing.T) {
	// Go's XML decoder ignores DTD declarations entirely.
	// Even if a DOCTYPE is present, as long as no entity references
	// are used in the body, the document parses safely.
	input := `<?xml version="1.0"?>
<!DOCTYPE foo [
  <!ENTITY xxe SYSTEM "file:///etc/passwd">
]>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUser><id>1</id></GetUser>
  </soap:Body>
</soap:Envelope>`

	op, body, fault, err := Unwrap([]byte(input))
	if err != nil {
		t.Fatalf("DTD without entity refs should parse safely: %v", err)
	}
	if fault != nil {
		t.Fatalf("unexpected fault: %v", fault)
	}
	if op != "GetUser" {
		t.Errorf("expected operation 'GetUser', got %q", op)
	}
	if body["id"] != "1" {
		t.Errorf("expected id '1', got %v", body["id"])
	}
}

func TestUnwrap_SafeSOAP_StillWorks(t *testing.T) {
	input := `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUserResponse>
      <name>John</name>
      <email>john@test.com</email>
    </GetUserResponse>
  </soap:Body>
</soap:Envelope>`

	operation, body, fault, err := Unwrap([]byte(input))
	if err != nil {
		t.Fatalf("safe SOAP should unwrap: %v", err)
	}
	if fault != nil {
		t.Fatalf("no fault expected: %v", fault)
	}
	if operation != "GetUser" {
		t.Errorf("expected operation 'GetUser', got %q", operation)
	}
	if body["name"] != "John" {
		t.Errorf("expected name 'John', got %v", body["name"])
	}
}
