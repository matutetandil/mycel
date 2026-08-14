package wasm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// This is the host side of a plugin: somebody else's compiled code, loaded into
// the process a service runs in. What it has to guarantee is that the plugin
// gets its input, the host gets the answer back, and a plugin that misbehaves
// is an error rather than an outage.
//
// It had almost no coverage because the tests that exercised a real module
// looked for testdata/test.wasm, which is not in the repository — so they had
// been skipping rather than running.

func loaded(t *testing.T) (*Runtime, *Module) {
	t.Helper()
	runtime, err := NewRuntime(context.Background())
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })

	module, err := runtime.LoadModule("fixture", fixtureFile(t))
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	return runtime, module
}

func TestWhatTheHostSendsIsWhatThePluginReceives(t *testing.T) {
	// The input goes across as JSON in the module's own memory, and the answer
	// comes back the same way. This module hands back what it was given, so a
	// value that survives the round trip proves the whole protocol.
	_, module := loaded(t)

	answer, err := module.CallFunction("echo", map[string]interface{}{
		"email": "ada@example.com",
		"age":   36,
		"tags":  []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("CallFunction: %v", err)
	}

	fields, ok := answer.(map[string]interface{})
	if !ok {
		t.Fatalf("the answer came back as %T", answer)
	}
	if fields["email"] != "ada@example.com" {
		t.Errorf("answer = %v", fields)
	}
	if fields["age"] != float64(36) {
		t.Errorf("a number did not survive the round trip: %#v", fields["age"])
	}
	if tags, _ := fields["tags"].([]interface{}); len(tags) != 2 {
		t.Errorf("a list did not survive the round trip: %#v", fields["tags"])
	}
}

func TestAPluginThatCrashesDoesNotTakeTheServiceWithIt(t *testing.T) {
	// The whole premise of running someone else's code in this process. A trap
	// inside the module has to come back as an error, and the runtime has to
	// keep working afterwards — a plugin that fails must cost one message, not
	// the service.
	_, module := loaded(t)

	_, err := module.CallFunction("boom", map[string]interface{}{"x": 1})
	if err == nil {
		t.Fatal("a module that traps reported success")
	}

	// Still usable.
	if _, err := module.CallFunction("echo", map[string]interface{}{"x": 1}); err != nil {
		t.Errorf("the module was unusable after a failed call: %v", err)
	}
}

func TestAFunctionThePluginDoesNotHaveIsReportedByName(t *testing.T) {
	_, module := loaded(t)

	_, err := module.CallFunction("transform_order", nil)
	if err == nil {
		t.Fatal("a function the plugin does not export was called")
	}
	for _, want := range []string{"transform_order", "fixture"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
}

func TestACheckThatPassesAndACheckThatFails(t *testing.T) {
	// A validator answers with a status rather than a document, and the
	// difference decides whether a message is accepted.
	_, module := loaded(t)

	if err := module.CallValidate("validate_always_valid", "anything"); err != nil {
		t.Errorf("a check that passes was reported as a failure: %v", err)
	}
	if err := module.CallValidate("validate_always_invalid", "anything"); err == nil {
		t.Error("a check that fails was reported as passing")
	}
}

func TestTheHostKnowsWhatAPluginOffers(t *testing.T) {
	_, module := loaded(t)

	if !module.HasFunction("echo") {
		t.Error("a function the plugin exports was not found")
	}
	if module.HasFunction("no_such_function") {
		t.Error("a function the plugin does not export was found")
	}
}

func TestOneModuleServesManyMessagesAtOnce(t *testing.T) {
	// A plugin is called from a flow, and flows run concurrently. The module's
	// memory is shared, so this is where two messages would write over each
	// other — each caller must get its own answer back.
	_, module := loaded(t)

	const callers = 24
	var wg sync.WaitGroup
	answers := make([]interface{}, callers)
	errs := make([]error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			answers[i], errs[i] = module.CallFunction("echo", map[string]interface{}{
				"reference": fmt.Sprintf("order-%d", i),
			})
		}(i)
	}
	wg.Wait()

	for i := 0; i < callers; i++ {
		if errs[i] != nil {
			t.Fatalf("call %d failed: %v", i, errs[i])
		}
		fields, _ := answers[i].(map[string]interface{})
		if fields == nil || fields["reference"] != fmt.Sprintf("order-%d", i) {
			t.Fatalf("call %d got %v, want its own answer", i, answers[i])
		}
	}
}

func TestAModuleIsGoneOnceItIsUnloaded(t *testing.T) {
	// Unloading is what a reload does before installing a new version, so
	// anything still holding the old module must fail rather than run code
	// that was meant to be replaced.
	runtime, module := loaded(t)

	if err := runtime.UnloadModule("fixture"); err != nil {
		t.Fatalf("UnloadModule: %v", err)
	}
	if _, found := runtime.GetModule("fixture"); found {
		t.Error("the module is still there after being unloaded")
	}
	if _, err := module.CallFunction("echo", map[string]interface{}{"x": 1}); err == nil {
		t.Error("a module that was unloaded still answered")
	}
	// Unloading again is not an error: a reload should not fail because the
	// plugin it was replacing had already gone.
	if err := runtime.UnloadModule("fixture"); err != nil {
		t.Errorf("unloading twice reported a failure: %v", err)
	}
}

func TestSomethingThatIsNotAModuleIsRefused(t *testing.T) {
	runtime, err := NewRuntime(context.Background())
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer func() { _ = runtime.Close() }()

	path := fixtureFile(t)
	if err := writeGarbage(path); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if _, err := runtime.LoadModule("broken", path); err == nil {
		t.Error("a file that is not WebAssembly was loaded as a plugin")
	}
}

func TestAModuleIsLoadedOnceAndSharedAfterwards(t *testing.T) {
	// Compiling is the expensive part, and a flow calling a plugin per message
	// must not pay it every time.
	runtime, first := loaded(t)

	second, err := runtime.LoadModule("fixture", first.Path())
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	if second != first {
		t.Error("loading the same plugin twice compiled it twice")
	}
}
