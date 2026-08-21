package rest

import (
	"encoding/base64"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

// Serving something that is not JSON.
//
// A flow that produces a PDF, an image or a spreadsheet answers with `_binary`
// and `_content_type`, and this is what turns that into an HTTP response — the
// mechanism the PDF connector's whole output depends on, and the one any flow
// can use. It was covered at a quarter.
func TestAFlowAnsweringWithBytesIsServedAsThoseBytes(t *testing.T) {
	c := New("api", 8080, nil, slog.Default())
	content := []byte("%PDF-1.4 not really")

	for _, form := range []struct {
		name string
		body interface{}
	}{
		{"raw bytes", content},
		{"base64, which is how it survives JSON", base64.StdEncoding.EncodeToString(content)},
	} {
		t.Run(form.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handled := c.writeBinaryResponse(w, 200, map[string]interface{}{
				"_binary":       form.body,
				"_content_type": "application/pdf",
				"_filename":     "invoice.pdf",
			})
			if !handled {
				t.Fatal("the answer was not served as bytes")
			}
			if got := w.Header().Get("Content-Type"); got != "application/pdf" {
				t.Errorf("content type = %q", got)
			}
			// The filename is what makes a browser save it under a name
			// rather than as the last path segment.
			if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "invoice.pdf") {
				t.Errorf("content disposition = %q", got)
			}
			if w.Body.String() != string(content) {
				t.Errorf("the body came back as %q", w.Body.String())
			}
		})
	}
}

// Anything else is left to the ordinary JSON path rather than being mangled.
func TestWhatIsNotBinaryIsLeftAlone(t *testing.T) {
	c := New("api", 8080, nil, slog.Default())

	for _, answer := range []interface{}{
		map[string]interface{}{"id": 1},
		map[string]interface{}{"_binary": []byte("x")},                      // no content type
		map[string]interface{}{"_content_type": "application/pdf"},          // no bytes
		map[string]interface{}{"_binary": []byte("x"), "_content_type": ""}, // an empty one
		map[string]interface{}{"_binary": 42, "_content_type": "application/pdf"},
		[]interface{}{1, 2, 3},
		"a string",
	} {
		w := httptest.NewRecorder()
		if c.writeBinaryResponse(w, 200, answer) {
			t.Errorf("%#v was served as bytes", answer)
		}
	}

	// And base64 that is not base64 is refused rather than served as garbage.
	w := httptest.NewRecorder()
	if c.writeBinaryResponse(w, 200, map[string]interface{}{
		"_binary":       "this is not base64!!!",
		"_content_type": "application/pdf",
	}) {
		t.Error("something that is not base64 was decoded and served")
	}
}

// Headers an aspect added travel with the answer, and the answer itself is
// what the flow produced rather than the wrapper carrying them.
func TestHeadersAnAspectAddedReachTheResponse(t *testing.T) {
	c := New("api", 8080, nil, slog.Default())
	w := httptest.NewRecorder()

	inner := map[string]interface{}{"id": 1}
	got := c.applyResponseHeaders(w, map[string]interface{}{
		"_response_headers": map[string]string{
			"X-Request-Id":  "abc",
			"Cache-Control": "no-store",
		},
		"_data": inner,
	})

	if w.Header().Get("X-Request-Id") != "abc" {
		t.Errorf("the header did not reach the response: %v", w.Header())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("a second header did not reach the response: %v", w.Header())
	}
	row, ok := got.(map[string]interface{})
	if !ok || row["id"] != 1 {
		t.Errorf("the answer came back as %#v, want what the flow produced", got)
	}

	// An answer with no wrapper is handed back untouched.
	plain := map[string]interface{}{"id": 2}
	if back := c.applyResponseHeaders(httptest.NewRecorder(), plain); back == nil {
		t.Error("an ordinary answer was dropped")
	}
	if back := c.applyResponseHeaders(httptest.NewRecorder(), "a string"); back != "a string" {
		t.Errorf("a string answer came back as %#v", back)
	}
}
