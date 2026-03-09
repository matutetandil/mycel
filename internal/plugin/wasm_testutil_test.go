package plugin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// TestGenerateAndValidateMinimalWASM generates a minimal WASM module
// that exports alloc, free, memory, and validate_always_valid.
// It validates the binary is correct by loading it with wazero.
func TestGenerateAndValidateMinimalWASM(t *testing.T) {
	wasmBytes := MinimalValidatorWASM()

	// Validate with wazero
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	wasi_snapshot_preview1.Instantiate(ctx, r)

	compiled, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		t.Fatalf("failed to compile WASM: %v", err)
	}

	instance, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName("test"))
	if err != nil {
		t.Fatalf("failed to instantiate WASM: %v", err)
	}
	defer instance.Close(ctx)

	// Verify exports
	exports := []string{"memory", "alloc", "free", "validate_always_valid"}
	for _, name := range exports {
		if name == "memory" {
			if instance.Memory() == nil {
				t.Errorf("expected memory export")
			}
			continue
		}
		fn := instance.ExportedFunction(name)
		if fn == nil {
			t.Errorf("expected export %q", name)
		}
	}

	// Call alloc — should return non-zero pointer
	allocFn := instance.ExportedFunction("alloc")
	results, err := allocFn.Call(ctx, 16)
	if err != nil {
		t.Fatalf("alloc failed: %v", err)
	}
	if results[0] == 0 {
		t.Error("alloc returned null pointer")
	}
	ptr := results[0]

	// Write some JSON to memory
	data := []byte(`{"value":"test"}`)
	if !instance.Memory().Write(uint32(ptr), data) {
		t.Fatal("failed to write to memory")
	}

	// Call validate — should return 0 (valid)
	validateFn := instance.ExportedFunction("validate_always_valid")
	results, err = validateFn.Call(ctx, ptr, uint64(len(data)))
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if results[0] != 0 {
		t.Errorf("expected 0 (valid), got %d", results[0])
	}

	// Write the WASM to the fixtures directory for integration tests
	_, thisFile, _, _ := runtime.Caller(0)
	fixtureDir := filepath.Dir(thisFile)
	outPath := filepath.Join(fixtureDir, "validator.wasm")
	if err := os.WriteFile(outPath, wasmBytes, 0644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}
	t.Logf("wrote %d bytes to %s", len(wasmBytes), outPath)
}

// MinimalValidatorWASM returns a minimal valid WASM binary that exports:
//   - memory (1 page = 64KB)
//   - alloc(i32) -> i32 (returns offset 1024)
//   - free(i32, i32) (no-op)
//   - validate_always_valid(i32, i32) -> i32 (returns 0 = valid)
func MinimalValidatorWASM() []byte {
	// Hand-encoded WASM binary.
	// Module structure:
	//   Header | Type Section | Function Section | Memory Section | Export Section | Code Section

	var w []byte

	// === Header ===
	w = append(w, 0x00, 0x61, 0x73, 0x6d) // magic: \0asm
	w = append(w, 0x01, 0x00, 0x00, 0x00) // version: 1

	// === Type Section (id=1) ===
	// 3 function types:
	//   type 0: (i32) -> (i32)        [alloc]
	//   type 1: (i32, i32) -> ()      [free]
	//   type 2: (i32, i32) -> (i32)   [validate]
	typeSection := []byte{
		0x03,                         // count: 3 types
		0x60, 0x01, 0x7f, 0x01, 0x7f, // type 0: (i32) -> (i32)
		0x60, 0x02, 0x7f, 0x7f, 0x00, // type 1: (i32, i32) -> ()
		0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f, // type 2: (i32, i32) -> (i32)
	}
	w = append(w, 0x01)                   // section id
	w = append(w, byte(len(typeSection))) // section length
	w = append(w, typeSection...)

	// === Function Section (id=3) ===
	// 3 functions: type indices [0, 1, 2]
	funcSection := []byte{
		0x03,       // count: 3 functions
		0x00, 0x01, 0x02, // type indices
	}
	w = append(w, 0x03)
	w = append(w, byte(len(funcSection)))
	w = append(w, funcSection...)

	// === Memory Section (id=5) ===
	// 1 memory, min=1 page (64KB), no max
	memSection := []byte{
		0x01,       // count: 1 memory
		0x00, 0x01, // limits: no-max, min=1
	}
	w = append(w, 0x05)
	w = append(w, byte(len(memSection)))
	w = append(w, memSection...)

	// === Export Section (id=7) ===
	// 4 exports: memory, alloc, free, validate_always_valid
	exportBody := []byte{0x04} // count: 4

	// Export "memory" -> memory 0
	exportBody = append(exportBody, 0x06) // name length
	exportBody = append(exportBody, []byte("memory")...)
	exportBody = append(exportBody, 0x02, 0x00) // kind=memory, index=0

	// Export "alloc" -> func 0
	exportBody = append(exportBody, 0x05)
	exportBody = append(exportBody, []byte("alloc")...)
	exportBody = append(exportBody, 0x00, 0x00) // kind=func, index=0

	// Export "free" -> func 1
	exportBody = append(exportBody, 0x04)
	exportBody = append(exportBody, []byte("free")...)
	exportBody = append(exportBody, 0x00, 0x01) // kind=func, index=1

	// Export "validate_always_valid" -> func 2
	name := "validate_always_valid"
	exportBody = append(exportBody, byte(len(name)))
	exportBody = append(exportBody, []byte(name)...)
	exportBody = append(exportBody, 0x00, 0x02) // kind=func, index=2

	w = append(w, 0x07)
	w = append(w, byte(len(exportBody)))
	w = append(w, exportBody...)

	// === Code Section (id=10) ===
	// 3 function bodies

	// Body 0 (alloc): return 1024  =>  i32.const 1024; end
	// 1024 in LEB128 = 0x80 0x08
	body0 := []byte{0x00, 0x41, 0x80, 0x08, 0x0b} // 0 locals, i32.const 1024, end

	// Body 1 (free): no-op  =>  end
	body1 := []byte{0x00, 0x0b} // 0 locals, end

	// Body 2 (validate): return 0  =>  i32.const 0; end
	body2 := []byte{0x00, 0x41, 0x00, 0x0b} // 0 locals, i32.const 0, end

	codeBody := []byte{0x03} // count: 3 bodies
	codeBody = append(codeBody, byte(len(body0)))
	codeBody = append(codeBody, body0...)
	codeBody = append(codeBody, byte(len(body1)))
	codeBody = append(codeBody, body1...)
	codeBody = append(codeBody, byte(len(body2)))
	codeBody = append(codeBody, body2...)

	w = append(w, 0x0a)
	w = append(w, byte(len(codeBody)))
	w = append(w, codeBody...)

	return w
}
