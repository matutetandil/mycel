package examples

import (
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The PDF example, checked on what it produces rather than on its status.
//
// A README command answering 200 says nothing here: the interesting failure is
// a 200 carrying JSON that describes a PDF instead of the PDF itself, or a
// document rendered without the data it was supposed to carry.
func TestThePDFExampleProducesAPDF(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	svc := start(t, "pdf")
	port := svc.ports[3000]
	if port == 0 {
		t.Fatal("the example's REST port was not moved; nothing to talk to")
	}
	base := fmt.Sprintf("http://localhost:%d", port)

	status, body := svc.run(t, fmt.Sprintf(`curl %s/invoices/1/pdf`, base))
	if status != 200 {
		t.Fatalf("downloading answered %d", status)
	}
	if !strings.HasPrefix(body, "%PDF-") {
		t.Fatalf("what came back is not a PDF: %.60q", body)
	}
	// The template is filled from the database row, so the invoice's own
	// details have to be in the document. They are inside a compressed content
	// stream, which is why this looks rather than grepping the file.
	text := pdfText(t, body)
	for _, wanted := range []string{"INV-001", "Acme"} {
		if !strings.Contains(text, wanted) {
			t.Errorf("the rendered PDF does not carry %q", wanted)
		}
	}

	// The other half of the connector: same template, written to disk.
	status, answer := svc.run(t, fmt.Sprintf(`curl -X POST %s/invoices/2/archive`, base))
	if status != 200 {
		t.Fatalf("archiving answered %d: %s", status, answer)
	}

	var saved struct {
		FilePath string `json:"file_path"`
		Filename string `json:"filename"`
		Size     int    `json:"size"`
	}
	if err := json.Unmarshal([]byte(answer), &saved); err != nil {
		t.Fatalf("archiving did not answer with JSON: %v: %s", err, answer)
	}
	if saved.Filename != "invoice-INV-002.pdf" {
		t.Errorf("filename = %q, want the one the flow built", saved.Filename)
	}
	if saved.Size == 0 {
		t.Errorf("a file of no size was written: %s", answer)
	}

	written, err := os.ReadFile(saved.FilePath)
	if err != nil {
		t.Fatalf("the file the answer names is not there: %v", err)
	}
	if !strings.HasPrefix(string(written), "%PDF-") {
		t.Errorf("the saved file is not a PDF: %.60q", written)
	}
}

// pdfText returns what a PDF's content streams say, which is where the text of
// the document lives — deflated, so grepping the file finds nothing.
func pdfText(t *testing.T, document string) string {
	t.Helper()

	var text strings.Builder
	text.WriteString(document) // uncompressed streams are readable as they are

	streams := regexp.MustCompile(`(?s)stream\r?\n(.*?)endstream`)
	for _, match := range streams.FindAllStringSubmatch(document, -1) {
		reader, err := zlib.NewReader(strings.NewReader(match[1]))
		if err != nil {
			continue
		}
		inflated, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			continue
		}
		text.Write(inflated)
	}
	return text.String()
}
