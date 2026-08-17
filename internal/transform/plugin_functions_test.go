package transform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/functions"
)

// A plugin can add a function to the expression language, so a transform can
// call something Mycel does not know how to do — a checksum, somebody's tax
// rules, a format nobody else speaks. The path from a functions block to a
// name that works inside a transform had no tests at all, which is awkward for
// the part of the system that exists to be extended.
//
// These run against a real WebAssembly module, hand-encoded here so there is no
// toolchain to install and no binary in the repository to drift.

// pluginWASM builds a module exporting three functions over the protocol the
// host speaks — input arrives as {"args": [...]}, the answer is {"result": ...}
// or {"error": "..."}:
//
//	shout   answers with a fixed result
//	refuse  answers with an error, the way a plugin rejects what it was given
//	mirror  hands back exactly what it was sent
func pluginWASM() []byte {
	const answer = `{"result":"SHOUTED"}`
	const refusal = `{"error":"this plugin does not accept that"}`

	var w []byte
	w = append(w, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)

	section := func(id byte, body []byte) {
		w = append(w, id, byte(len(body)))
		w = append(w, body...)
	}

	// (i32) -> i32 for alloc, (i32,i32) -> () for free, (i32,i32) -> (i32,i32)
	// for anything answering with a document.
	section(0x01, []byte{
		0x03,
		0x60, 0x01, 0x7f, 0x01, 0x7f,
		0x60, 0x02, 0x7f, 0x7f, 0x00,
		0x60, 0x02, 0x7f, 0x7f, 0x02, 0x7f, 0x7f,
	})
	section(0x03, []byte{0x05, 0x00, 0x01, 0x02, 0x02, 0x02})
	section(0x05, []byte{0x01, 0x00, 0x01})

	exports := []byte{0x05}
	addExport := func(name string, kind, index byte) {
		exports = append(exports, byte(len(name)))
		exports = append(exports, name...)
		exports = append(exports, kind, index)
	}
	addExport("memory", 0x02, 0x00)
	addExport("alloc", 0x00, 0x00)
	addExport("shout", 0x00, 0x02)
	addExport("refuse", 0x00, 0x03)
	addExport("mirror", 0x00, 0x04)
	section(0x07, exports)

	code := []byte{0x05}
	addBody := func(body []byte) {
		entry := append([]byte{0x00}, body...)
		code = append(code, byte(len(entry)))
		code = append(code, entry...)
	}
	// Input is written at 2048 by the host's allocator below, so the two fixed
	// documents sit safely under it.
	addBody([]byte{0x41, 0x80, 0x10, 0x0b})                     // alloc: 2048
	addBody([]byte{0x0b})                                       // free
	addBody([]byte{0x41, 0x00, 0x41, byte(len(answer)), 0x0b})  // shout
	addBody([]byte{0x41, 0x40, 0x41, byte(len(refusal)), 0x0b}) // refuse (offset 64)
	addBody([]byte{0x20, 0x00, 0x20, 0x01, 0x0b})               // mirror
	section(0x0a, code)

	// The two fixed documents.
	data := []byte{0x02}
	addData := func(offset byte, content string) {
		data = append(data, 0x00, 0x41, offset, 0x0b, byte(len(content)))
		data = append(data, content...)
	}
	addData(0x00, answer)
	addData(0x38, refusal)
	section(0x0b, data)

	return w
}

func pluginRegistry(t *testing.T) *functions.Registry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugin.wasm")
	if err := os.WriteFile(path, pluginWASM(), 0o644); err != nil {
		t.Fatalf("writing the plugin: %v", err)
	}

	registry := functions.NewRegistry()
	if err := registry.Register(&functions.Config{
		Name: "custom", WASM: path,
		Exports: []string{"shout", "refuse", "mirror"},
	}); err != nil {
		t.Fatalf("registering the plugin: %v", err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	return registry
}

func pluginTransformer(t *testing.T) *CELTransformer {
	t.Helper()
	transformer, err := NewCELTransformerWithOptions(CreateWASMFunctionOptions(pluginRegistry(t))...)
	if err != nil {
		t.Fatalf("NewCELTransformerWithOptions: %v", err)
	}
	return transformer
}

func TestATransformCanCallAFunctionAPluginBrought(t *testing.T) {
	transformer := pluginTransformer(t)

	out, err := transformer.Transform(context.Background(),
		map[string]interface{}{"name": "ada"},
		[]Rule{{Target: "loud", Expression: "shout(input.name)"}})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if out["loud"] != "SHOUTED" {
		t.Errorf("loud = %#v, want what the plugin answered", out["loud"])
	}
}

func TestTheArgumentsReachThePlugin(t *testing.T) {
	// The host sends them as a document; a plugin that receives nothing would
	// still answer, and the transform would look like it worked.
	transformer := pluginTransformer(t)

	out, err := transformer.Transform(context.Background(),
		map[string]interface{}{"name": "ada", "age": 36},
		[]Rule{{Target: "sent", Expression: "mirror(input.name, input.age)"}})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	echoed, ok := out["sent"].(map[string]interface{})
	if !ok {
		t.Fatalf("sent = %#v", out["sent"])
	}
	args, _ := echoed["args"].([]interface{})
	if len(args) != 2 || args[0] != "ada" {
		t.Errorf("the plugin was given %#v, want both arguments in order", echoed["args"])
	}
}

func TestAPluginThatRefusesFailsTheTransform(t *testing.T) {
	// A plugin saying no is not a value: the field must not quietly become
	// whatever the refusal looked like.
	transformer := pluginTransformer(t)

	_, err := transformer.Transform(context.Background(),
		map[string]interface{}{"name": "ada"},
		[]Rule{{Target: "value", Expression: "refuse(input.name)"}})
	if err == nil {
		t.Fatal("a plugin that refused produced a value")
	}
	if !strings.Contains(err.Error(), "refuse") {
		t.Errorf("error = %q, want it to name the function", err)
	}
}

func TestAFunctionNoPluginBroughtIsRefusedWhenTheExpressionIsCompiled(t *testing.T) {
	// Better at startup than per message: the expression is compiled once, so
	// a name nobody provides is a configuration error rather than a field that
	// is empty in production.
	transformer := pluginTransformer(t)

	_, err := transformer.Transform(context.Background(),
		map[string]interface{}{"name": "ada"},
		[]Rule{{Target: "value", Expression: "no_such_plugin_function(input.name)"}})
	if err == nil {
		t.Error("an expression calling a function nobody provides was accepted")
	}
}

func TestAModuleThatDoesNotExportWhatItPromisedIsRefused(t *testing.T) {
	// The exports list is the contract in the configuration file, and a
	// mismatch has to be found at startup rather than the first time a message
	// reaches that transform.
	path := filepath.Join(t.TempDir(), "plugin.wasm")
	if err := os.WriteFile(path, pluginWASM(), 0o644); err != nil {
		t.Fatalf("writing the plugin: %v", err)
	}

	registry := functions.NewRegistry()
	t.Cleanup(func() { _ = registry.Close() })

	err := registry.Register(&functions.Config{
		Name: "custom", WASM: path,
		Exports: []string{"shout", "compute_tax"},
	})
	if err == nil {
		t.Fatal("a plugin was registered promising a function it does not have")
	}
	if !strings.Contains(err.Error(), "compute_tax") {
		t.Errorf("error = %q, want it to name the function that is missing", err)
	}
}
