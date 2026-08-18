package functions

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Calling a function somebody added to the expression language.
//
// A `functions` block loads a WASM module and names what it exports, and from
// then on a transform can call it like any built-in. Everything up to the
// loading was tested and nothing past it: whether the call reaches the module,
// what comes back, and what happens when the module says no.
//
// The module used here is the one the integration suite ships — a few dozen
// bytes of hand-written WebAssembly exporting `shout`, which answers with a
// fixed record. Small enough to read, and real: it goes through the same
// runtime a plugin does.
const testModule = "../../tests/integration/config/plugins/test-plugin/functions.wasm"

func registryWith(t *testing.T, exports ...string) *Registry {
	t.Helper()

	path, err := filepath.Abs(testModule)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry()
	if err := r.Register(&Config{Name: "custom", WASM: path, Exports: exports}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestAFunctionAddedByAPluginCanBeCalled(t *testing.T) {
	r := registryWith(t, "shout")

	fn, err := r.GetFunction("custom", "shout")
	if err != nil {
		t.Fatalf("GetFunction: %v", err)
	}
	if fn.Name() != "shout" {
		t.Errorf("name = %q", fn.Name())
	}

	result, err := fn.Call("hello")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	// The module answers with a record carrying `result`, and that is what a
	// CEL expression receives — not the record around it.
	if result != "PLUGIN" {
		t.Errorf("the call answered %#v, want the value the module returned", result)
	}
}

func TestOnlyWhatTheBlockNamedCanBeCalled(t *testing.T) {
	// The exports list is the contract: a module may contain anything, and
	// only what the configuration named is reachable from an expression.
	r := registryWith(t, "shout")

	if _, err := r.GetFunction("custom", "alloc"); err == nil {
		t.Error("a function the block did not name was callable from an expression")
	} else if !strings.Contains(err.Error(), "alloc") {
		t.Errorf("the error does not name what was asked for: %v", err)
	}

	if _, err := r.GetFunction("other", "shout"); err == nil {
		t.Error("a module nobody registered answered")
	}
}

func TestEveryFunctionTheExpressionEnvironmentGets(t *testing.T) {
	// What is handed to CEL when the environment is built. A name missing
	// here is an expression that fails to compile, and it takes every
	// transform in the service with it — not only the one that uses it.
	r := registryWith(t, "shout")

	all := r.GetAllFunctions()
	if len(all) != 1 {
		t.Fatalf("functions = %v", all)
	}
	fn, ok := all["shout"]
	if !ok || fn.Name() != "shout" {
		t.Errorf("functions = %v", all)
	}

	names, err := r.FunctionNames("custom")
	if err != nil {
		t.Fatalf("FunctionNames: %v", err)
	}
	if len(names) != 1 || names[0] != "shout" {
		t.Errorf("names = %v", names)
	}
}

func TestAModuleThatDoesNotExportWhatWasPromised(t *testing.T) {
	// Caught at start-up rather than at the first message: a transform
	// calling a function that is not there fails to compile, and that takes
	// the whole flow down.
	path, _ := filepath.Abs(testModule)
	r := NewRegistry()
	defer r.Close()

	err := r.Register(&Config{Name: "custom", WASM: path, Exports: []string{"shout", "whisper"}})
	if err == nil {
		t.Fatal("a module was accepted while promising a function it does not have")
	}
	if !strings.Contains(err.Error(), "whisper") {
		t.Errorf("the error does not name the missing function: %v", err)
	}
	// And nothing was half-registered.
	if _, err := r.GetFunction("custom", "shout"); err == nil {
		t.Error("a module that failed to register is callable anyway")
	}
}

func TestCallingWithArgumentsAsJSON(t *testing.T) {
	// The path a plugin host takes: arguments arrive encoded and the answer
	// goes back encoded.
	r := registryWith(t, "shout")

	fn, err := r.GetFunction("custom", "shout")
	if err != nil {
		t.Fatalf("GetFunction: %v", err)
	}
	callable, ok := fn.(interface {
		CallWithJSON([]byte) ([]byte, error)
	})
	if !ok {
		t.Fatal("the function cannot be called with encoded arguments")
	}

	answer, err := callable.CallWithJSON([]byte(`["hello"]`))
	if err != nil {
		t.Fatalf("CallWithJSON: %v", err)
	}
	var value interface{}
	if err := json.Unmarshal(answer, &value); err != nil {
		t.Fatalf("the answer is not JSON: %s", answer)
	}
	if value != "PLUGIN" {
		t.Errorf("answer = %#v", value)
	}

	// Arguments that are not a list at all are refused rather than passed on
	// as one.
	if _, err := callable.CallWithJSON([]byte(`{"not":"a list"}`)); err == nil {
		t.Error("arguments that are not a list were accepted")
	}
}

func TestAModuleThatIsNotThere(t *testing.T) {
	r := NewRegistry()
	defer r.Close()

	err := r.Register(&Config{Name: "custom", WASM: "/nonexistent/functions.wasm", Exports: []string{"shout"}})
	if err == nil {
		t.Fatal("a module that does not exist was registered")
	}
	// The path is in the message: the usual failure is a file that was not
	// deployed beside the configuration.
	if !strings.Contains(err.Error(), "/nonexistent/functions.wasm") {
		t.Errorf("the error does not name the file: %v", err)
	}
}
