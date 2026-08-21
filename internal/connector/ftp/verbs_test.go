package ftp

import (
	"os"
	"strings"
	"testing"
)

// The runtime's words for a read and a write reach every connector.
//
// A flow that does not name an operation gets SELECT on a read and INSERT on a
// write — database words, because that is where the defaults come from. A file
// server refused them outright, so fetching a file or uploading one from a
// flow written the ordinary way came back as "unknown read operation: SELECT",
// which names nothing anybody wrote.
//
// This checks the lists rather than the transfers: what a real server does
// with them is the integration suite's business.
func TestTheRuntimesDefaultVerbsAreUnderstood(t *testing.T) {
	source := readFile(t, "ftp.go")

	readVerbs := betweenMarkers(t, source, `case "LIST":`, `default:`)
	for _, verb := range []string{"GET", "SELECT"} {
		if !strings.Contains(readVerbs, `"`+verb+`"`) {
			t.Errorf("a read does not accept %s", verb)
		}
	}

	writeVerbs := betweenMarkers(t, source, `case "PUT",`, `case "MKDIR":`)
	for _, verb := range []string{"PUT", "UPLOAD", "INSERT", "UPDATE"} {
		if !strings.Contains(writeVerbs, `"`+verb+`"`) {
			t.Errorf("a write does not accept %s", verb)
		}
	}
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(body)
}

func betweenMarkers(t *testing.T, source, from, to string) string {
	t.Helper()
	start := strings.Index(source, from)
	if start < 0 {
		t.Fatalf("%q is no longer in the source, so this test is checking nothing", from)
	}
	rest := source[start:]
	end := strings.Index(rest, to)
	if end < 0 {
		t.Fatalf("%q is no longer in the source", to)
	}
	return rest[:end]
}
