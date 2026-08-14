package wasm

import (
	"os"
	"path/filepath"
	"testing"
)

// A hand-encoded module, so the tests below run against a real WebAssembly
// binary without a toolchain and without a checked-in file that drifts from
// what the tests expect. It exports what the host actually calls:
//
//	memory, alloc, free                  the ptr/len protocol
//	echo(ptr,len) -> (ptr,len)           hands the input straight back
//	validate_always_valid(ptr,len) -> 0  a check that passes
//	validate_always_invalid -> 1         a check that fails
//	boom(ptr,len)                        traps, the way a broken plugin does
func fixtureWASM(extraExports ...string) []byte {
	var w []byte
	w = append(w, 0x00, 0x61, 0x73, 0x6d) // \0asm
	w = append(w, 0x01, 0x00, 0x00, 0x00) // version 1

	section := func(id byte, body []byte) {
		w = append(w, id, byte(len(body)))
		w = append(w, body...)
	}

	// Types
	section(0x01, []byte{
		0x04,
		0x60, 0x01, 0x7f, 0x01, 0x7f, // 0: (i32) -> i32          alloc
		0x60, 0x02, 0x7f, 0x7f, 0x00, // 1: (i32,i32) -> ()       free
		0x60, 0x02, 0x7f, 0x7f, 0x02, 0x7f, 0x7f, // 2: (i32,i32) -> (i32,i32)  echo
		0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f, // 3: (i32,i32) -> i32      validate
	})

	// Functions: alloc, free, echo, valid, invalid, boom
	section(0x03, []byte{0x06, 0x00, 0x01, 0x02, 0x03, 0x03, 0x03})

	// One page of memory
	section(0x05, []byte{0x01, 0x00, 0x01})

	// Exports
	exports := []byte{0x07}
	addExport := func(name string, kind, index byte) {
		exports = append(exports, byte(len(name)))
		exports = append(exports, name...)
		exports = append(exports, kind, index)
	}
	addExport("memory", 0x02, 0x00)
	addExport("alloc", 0x00, 0x00)
	addExport("free", 0x00, 0x01)
	addExport("echo", 0x00, 0x02)
	addExport("validate_always_valid", 0x00, 0x03)
	addExport("validate_always_invalid", 0x00, 0x04)
	addExport("boom", 0x00, 0x05)
	for _, name := range extraExports {
		// An alias of validate_always_valid, so the module is the same one
		// with a name it did not have before.
		addExport(name, 0x00, 0x03)
	}
	exports[0] = byte(0x07 + len(extraExports))
	section(0x07, exports)

	// Code
	code := []byte{0x06}
	addBody := func(body []byte) {
		entry := append([]byte{0x00}, body...) // no locals
		code = append(code, byte(len(entry)))
		code = append(code, entry...)
	}
	addBody([]byte{0x41, 0x80, 0x08, 0x0b})       // alloc: i32.const 1024
	addBody([]byte{0x0b})                         // free: nothing
	addBody([]byte{0x20, 0x00, 0x20, 0x01, 0x0b}) // echo: the pointer and length it was given
	addBody([]byte{0x41, 0x00, 0x0b})             // valid: 0
	addBody([]byte{0x41, 0x01, 0x0b})             // invalid: 1
	addBody([]byte{0x00, 0x0b})                   // boom: unreachable
	section(0x0a, code)

	return w
}

// fixtureFile writes the module somewhere the runtime can load it from.
func fixtureFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.wasm")
	if err := os.WriteFile(path, fixtureWASM(), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	return path
}

// writeGarbage replaces a file with something that is not WebAssembly.
func writeGarbage(path string) error {
	return os.WriteFile(path, []byte("this is not a module"), 0o644)
}

// upgradedWASM is the same module with one more exported function, standing in
// for a plugin that has been upgraded in place.
func upgradedWASM() []byte {
	return fixtureWASM("added_in_the_new_version")
}
