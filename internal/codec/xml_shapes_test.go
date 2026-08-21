package codec

import (
	"encoding/xml"
	"strings"
	"testing"
)

// What a flow hands back is not what a document decodes into.
//
// The encoder knew map[string]interface{} and []interface{} — the shapes its
// own decoder produces — and a database read answers
// []map[string]interface{}. That matched neither, so a list of rows was
// written with Go's default formatting: <root>[map[id:1 name:Widget]]</root>,
// served as application/xml to whoever asked for XML.

func TestRowsFromADatabaseEncodeAsElements(t *testing.T) {
	rows := []map[string]interface{}{
		{"id": 1, "name": "Widget"},
		{"id": 2, "name": "Gadget"},
	}

	out, err := (&XMLCodec{}).Encode(rows)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got := string(out)

	if strings.Contains(got, "map[") {
		t.Errorf("the rows were printed rather than encoded:\n%s", got)
	}
	for _, want := range []string{"<name>Widget</name>", "<name>Gadget</name>", "<id>1</id>", "<id>2</id>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}

	// One root, or it is not a document.
	if strings.Count(got, "<root>") != 1 {
		t.Errorf("a list produced %d roots; a document has one:\n%s", strings.Count(got, "<root>"), got)
	}

	// And it parses.
	var parsed struct {
		Items []struct {
			ID   int    `xml:"id"`
			Name string `xml:"name"`
		} `xml:"item"`
	}
	if err := xml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("the document does not parse: %v\n%s", err, got)
	}
	if len(parsed.Items) != 2 || parsed.Items[0].Name != "Widget" {
		t.Errorf("parsed %+v", parsed.Items)
	}
}

func TestATypedMapEncodesAsElements(t *testing.T) {
	// A map whose values are concrete rather than interface{} — what a
	// hand-built payload often is.
	out, err := (&XMLCodec{}).Encode(map[string]string{"status": "ok"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if got := string(out); !strings.Contains(got, "<status>ok</status>") {
		t.Errorf("a typed map encoded as:\n%s", got)
	}
}

func TestTheShapesTheDecoderProducesStillEncode(t *testing.T) {
	// The paths that already worked, so the reflection fallback does not
	// change them.
	out, err := (&XMLCodec{}).Encode(map[string]interface{}{
		"product": map[string]interface{}{"name": "Widget"},
		"tags":    []interface{}{"a", "b"},
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got := string(out)
	for _, want := range []string{"<name>Widget</name>", "<tags>a</tags>", "<tags>b</tags>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
}
