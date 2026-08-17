package sanitize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A sanitiser somebody wrote for their own business.
//
// The built-in pipeline refuses what everybody has to refuse — SQL fragments,
// XML entities, null bytes. What it cannot know is what counts as sensitive
// here, which is what this extension point is for, and it had no tests at all:
// the rule that loads the module, calls it, and treats an empty answer as a
// refusal was never run.

func sanitizerModule(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "examples", "plugin", "plugins",
		"pii-sanitizer", "sanitizer.wasm")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("the example sanitiser is not built: %v", err)
	}
	return path
}

func TestAValueTheModuleMasksComesBackMasked(t *testing.T) {
	rule, err := NewWASMRule("pii", sanitizerModule(t), "sanitize")
	if err != nil {
		t.Fatalf("NewWASMRule: %v", err)
	}
	if rule.Name() != "pii" {
		t.Errorf("name = %q", rule.Name())
	}

	cleaned, err := rule.Sanitize(map[string]interface{}{
		"reference": "ORD-1",
		"card":      "4111 1111 1111 1111",
	})
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}

	row, ok := cleaned.(map[string]interface{})
	if !ok {
		t.Fatalf("answer = %#v, want the value to use instead", cleaned)
	}
	if row["card"] != "****1111" {
		t.Errorf("card = %v, want it masked before it reaches a flow", row["card"])
	}
	// What it had no reason to touch comes back as it was.
	if row["reference"] != "ORD-1" {
		t.Errorf("reference = %v", row["reference"])
	}
}

func TestAValueTheModuleRefusesStopsTheRequest(t *testing.T) {
	// Answering with nothing is a refusal, and the request is turned away
	// before any flow runs — which is the whole point of sanitising on the way
	// in rather than filtering on the way out.
	rule, err := NewWASMRule("pii", sanitizerModule(t), "sanitize")
	if err != nil {
		t.Fatalf("NewWASMRule: %v", err)
	}

	_, err = rule.Sanitize(map[string]interface{}{"tax_number": "123-456-789"})
	if err == nil {
		t.Fatal("a value the module refused was let through")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("error = %q, want it to say the sanitiser refused it", err)
	}
}

func TestAModuleReachesEveryPartOfTheValue(t *testing.T) {
	// A payload is nested, so a sanitiser that only looked at the top level
	// would leave a card number one field down.
	rule, err := NewWASMRule("pii", sanitizerModule(t), "sanitize")
	if err != nil {
		t.Fatalf("NewWASMRule: %v", err)
	}

	cleaned, err := rule.Sanitize(map[string]interface{}{
		"customer": map[string]interface{}{
			"payment": map[string]interface{}{"card": "4111111111111111"},
		},
		"items": []interface{}{
			map[string]interface{}{"card": "5555 5555 5555 4444"},
		},
	})
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}

	row := cleaned.(map[string]interface{})
	customer := row["customer"].(map[string]interface{})
	payment := customer["payment"].(map[string]interface{})
	if payment["card"] != "****1111" {
		t.Errorf("a card one level down was left alone: %v", payment["card"])
	}
	items := row["items"].([]interface{})
	first := items[0].(map[string]interface{})
	if first["card"] != "****4444" {
		t.Errorf("a card inside a list was left alone: %v", first["card"])
	}
}

func TestASanitiserWithNoModuleIsRefused(t *testing.T) {
	// At startup, where the path can be fixed — rather than as a request that
	// fails because a rule silently does nothing.
	if _, err := NewWASMRule("pii", "", "sanitize"); err == nil {
		t.Error("a sanitiser with no module was built")
	}
	if _, err := NewWASMRule("pii", filepath.Join(t.TempDir(), "absent.wasm"), "sanitize"); err == nil {
		t.Error("a sanitiser whose module is not there was built")
	}
}

func TestAModuleMissingItsEntryPointIsRefused(t *testing.T) {
	// The name is configurable, so a typo produces a rule that loads and can
	// never be called. Saying so at startup is the difference between a
	// configuration error and a security control that quietly does nothing.
	_, err := NewWASMRule("pii", sanitizerModule(t), "a_function_nobody_exported")
	if err == nil {
		t.Fatal("a sanitiser naming a function the module does not export was built")
	}
	if !strings.Contains(err.Error(), "a_function_nobody_exported") {
		t.Errorf("error = %q, want it to name what is missing", err)
	}
}

func TestTheEntryPointHasAUsualName(t *testing.T) {
	// So that a configuration naming none still works.
	rule, err := NewWASMRule("pii", sanitizerModule(t), "")
	if err != nil {
		t.Fatalf("NewWASMRule: %v", err)
	}
	if _, err := rule.Sanitize(map[string]interface{}{"reference": "ORD-1"}); err != nil {
		t.Errorf("Sanitize: %v", err)
	}
}

func TestAModuleCanBeReplacedWhileTheServiceRuns(t *testing.T) {
	// Hot reload: the rule holds a compiled module, and reloading has to pick
	// up the file on disk rather than keep answering from the old one.
	rule, err := NewWASMRule("pii-reload", sanitizerModule(t), "sanitize")
	if err != nil {
		t.Fatalf("NewWASMRule: %v", err)
	}

	if err := rule.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// And it still works afterwards, which is what says the module was put
	// back rather than merely dropped.
	cleaned, err := rule.Sanitize(map[string]interface{}{"card": "4111111111111111"})
	if err != nil {
		t.Fatalf("Sanitize after reload: %v", err)
	}
	if cleaned.(map[string]interface{})["card"] != "****1111" {
		t.Errorf("answer = %v", cleaned)
	}
}
