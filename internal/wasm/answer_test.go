package wasm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Where a module leaves its answer.
//
// The interface was documented as returning two values, (ptr, len), and no
// toolchain anyone is pointed at can emit that: Rust and TinyGo both lower a
// two-word return through the C ABI, which becomes a pointer argument rather
// than two results. So the documented shape was one nothing could produce,
// which is why no connector plugin had ever been built against it — the packed
// form below is what a plugin in any language can return.
//
// The module under test is the example plugin, compiled and committed, so this
// runs against a real one rather than something shaped like it.

func examplePlugin(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "examples", "plugin", "plugins",
		"inventory-store", "connector.wasm")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("the example plugin is not built: %v", err)
	}
	return path
}

func TestAnAnswerPackedIntoOneValueIsRead(t *testing.T) {
	runtime, err := NewRuntime(context.Background())
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer runtime.Close()

	module, err := runtime.LoadModule("inventory", examplePlugin(t))
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}

	// init takes the connector's configuration and answers.
	result, err := module.CallFunction("init", map[string]interface{}{"warehouse": "auckland"})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	answer, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("result = %#v, want the JSON the module wrote", result)
	}
	if answer["ok"] != true {
		t.Errorf("answer = %v", answer)
	}
}

func TestAModuleAnswersWithWhatItWasAsked(t *testing.T) {
	// The input has to arrive: a module that receives nothing answers the
	// same way whatever it is asked, which is indistinguishable from working
	// until the first question that matters.
	runtime, err := NewRuntime(context.Background())
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer runtime.Close()

	module, err := runtime.LoadModule("inventory", examplePlugin(t))
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	if _, err := module.CallFunction("init", map[string]interface{}{"warehouse": "wellington"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	result, err := module.CallFunction("read", map[string]interface{}{
		"target":  "levels",
		"filters": map[string]interface{}{"sku": "WIDGET-1"},
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	answer, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("result = %#v", result)
	}
	rows, ok := answer["rows"].([]interface{})
	if !ok || len(rows) != 1 {
		t.Fatalf("rows = %v, want the one SKU that was asked for", answer["rows"])
	}
	row := rows[0].(map[string]interface{})
	if row["sku"] != "WIDGET-1" {
		t.Errorf("row = %v", row)
	}
	// The configuration reached the module rather than a default.
	if row["warehouse"] != "wellington" {
		t.Errorf("warehouse = %v, want the one the connector was configured with", row["warehouse"])
	}
}

func TestAModuleThatRefusesIsHeard(t *testing.T) {
	// A plugin's own rules are the reason to write one, so a refusal has to
	// reach the flow rather than being read as an empty answer.
	runtime, err := NewRuntime(context.Background())
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer runtime.Close()

	module, err := runtime.LoadModule("inventory", examplePlugin(t))
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	if _, err := module.CallFunction("init", map[string]interface{}{"warehouse": "auckland"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	result, err := module.CallFunction("call", map[string]interface{}{
		"operation": "reserve",
		"params":    map[string]interface{}{"sku": "WIDGET-2", "quantity": 999},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	answer, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("result = %#v", result)
	}
	if answer["error"] == nil {
		t.Errorf("answer = %v, want the module's refusal", answer)
	}
}
