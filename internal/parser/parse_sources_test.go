package parser

import (
	"context"
	"strings"
	"testing"
)

// A configuration can be parsed from sources that are not on disk — the
// editor's unsaved buffers — with the same result as parsing the directory.
func TestParseSourcesReadsAProjectFromMemory(t *testing.T) {
	files := map[string][]byte{
		"/p/constants.mycel": []byte("constants {\n  port = 3000\n}\n"),
		"/p/connectors.mycel": []byte(`
connector "api" {
  type = "rest"
  port = constants.port
}
`),
		"/p/flows.mycel": []byte(`
flow "ping" {
  from {
    connector = "api"
    operation = "GET /ping"
  }
  response {
    ok = "true"
  }
}
`),
	}
	cfg, err := NewHCLParser().ParseSources(context.Background(), files)
	if err != nil {
		t.Fatalf("ParseSources: %v", err)
	}
	if len(cfg.Connectors) != 1 || len(cfg.Flows) != 1 {
		t.Fatalf("connectors=%d flows=%d", len(cfg.Connectors), len(cfg.Flows))
	}
	// A constant declared in one source is seen by another, whatever the order.
	if got := cfg.Connectors[0].Properties["port"]; got != int64(3000) && got != 3000 && got != float64(3000) {
		t.Errorf("port = %#v, want the constant", got)
	}
	if cfg.Flows[0].SourceFile != "/p/flows.mycel" {
		t.Errorf("SourceFile = %q", cfg.Flows[0].SourceFile)
	}
}

func TestAParseErrorNamesTheFileAndTheBlock(t *testing.T) {
	files := map[string][]byte{
		"/p/flows.mycel": []byte(`
flow "gallery" {
  from {
    connector = "api"
    operation = "GET /gallery"
  }
  cache {
    storage  = "redis"
    key      = "gallery:${input.store}"
    key_from = "'gallery:' + input.store"
  }
}
`),
	}
	_, err := NewHCLParser().ParseSources(context.Background(), files)
	if err == nil {
		t.Fatal("key and key_from together parsed")
	}
	for _, want := range []string{"/p/flows.mycel", `flow "gallery"`, "both key and key_from"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}
