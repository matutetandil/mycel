package validator

import (
	"path/filepath"
	"strings"
	"testing"
)

// A rule somebody wrote in another language.
//
// A WASM validator is the escape hatch: an IRD number, a tax id, a check the
// expression language cannot express. It is applied to values arriving from
// outside, so the two things that matter are that a refusal actually refuses,
// and that a validator which failed to load does not quietly accept
// everything.
//
// The module here is the one the integration suite ships — hand-written
// WebAssembly exporting `validate_always_valid`, small enough to read.
const alwaysValid = "../../tests/integration/config/plugins/test-plugin/validators.wasm"

func modulePath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(alwaysValid)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestARuleWrittenInWASM(t *testing.T) {
	v, err := NewWASMValidator("nz_ird", modulePath(t), "validate_always_valid", "that is not an IRD number")
	if err != nil {
		t.Fatalf("NewWASMValidator: %v", err)
	}

	if v.Name() != "nz_ird" {
		t.Errorf("name = %q", v.Name())
	}
	if v.Type() != ValidatorTypeWASM {
		t.Errorf("type = %v", v.Type())
	}
	if err := v.Validate("123-456-789"); err != nil {
		t.Errorf("a value the module accepted was refused: %v", err)
	}
}

func TestAValidatorThatCouldNotBeLoaded(t *testing.T) {
	// The failure that matters: a validator nobody could load must not accept
	// everything. A rule that silently stops applying is worse than one that
	// was never written, because the configuration says it is there.
	if _, err := NewWASMValidator("nz_ird", "/nonexistent/rules.wasm", "validate", "no"); err == nil {
		t.Error("a validator was built from a module that does not exist")
	}

	if _, err := NewWASMValidator("nz_ird", "", "validate", "no"); err == nil {
		t.Error("a validator was built with no module at all")
	}

	// And one whose module never loaded refuses rather than passes.
	unloaded := &WASMValidator{name: "nz_ird", message: "that is not an IRD number"}
	err := unloaded.Validate("anything")
	if err == nil {
		t.Fatal("a validator with no module accepted the value")
	}
	if !strings.Contains(err.Error(), "not loaded") {
		t.Errorf("the error does not say why: %v", err)
	}
}

func TestWhatTheCallerIsToldWhenARuleRefuses(t *testing.T) {
	// The message comes from the configuration, because only whoever wrote
	// the rule can say what it means. Without one, a caller gets "validation
	// failed" and no idea which field or why.
	v, err := NewWASMValidator("nz_ird", modulePath(t), "validate_always_valid", "")
	if err != nil {
		t.Fatalf("NewWASMValidator: %v", err)
	}
	if v.message == "" {
		t.Error("a validator with no message would refuse without saying anything")
	}

	// A validator naming a function the module does not have is refused when
	// it is built, which is start-up — not at the first message, by which
	// time it is refusing real traffic for a reason nobody wrote down. The
	// entrypoint defaults to "validate", so this is also what a block that
	// names only the module gets when the module calls it something else.
	_, err = NewWASMValidator("nz_ird", modulePath(t), "", "no")
	if err == nil {
		t.Error("a validator was built against an entrypoint the module does not export")
	} else if !strings.Contains(err.Error(), "validate") {
		t.Errorf("the error does not name the function it looked for: %v", err)
	}

	// The error carries which rule refused and what it was given, which is
	// what a flow logs when a message is rejected.
	failure := &ValidationError{ValidatorName: "nz_ird", Value: "123", Message: "that is not an IRD number"}
	if failure.Error() != "that is not an IRD number" {
		t.Errorf("error = %q", failure.Error())
	}
}

func TestReloadingARuleAfterItChanged(t *testing.T) {
	// A hot reload replaces the module on disk; the validator has to pick up
	// the new one rather than keep answering from the old.
	v, err := NewWASMValidator("nz_ird", modulePath(t), "validate_always_valid", "no")
	if err != nil {
		t.Fatalf("NewWASMValidator: %v", err)
	}

	if err := v.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	// Still usable afterwards: a reload that leaves the validator without a
	// module would refuse every value from then on.
	if err := v.Validate("123-456-789"); err != nil {
		t.Errorf("the validator stopped working after a reload: %v", err)
	}
}

func TestClosingTheRuntimeIsSafe(t *testing.T) {
	// Called on the way out, and the runtime may never have been built —
	// which is the case for every service that uses no WASM at all.
	if err := CloseWASMRuntime(); err != nil {
		t.Errorf("CloseWASMRuntime: %v", err)
	}
}
