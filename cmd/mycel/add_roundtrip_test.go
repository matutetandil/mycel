package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/parser"
	"github.com/matutetandil/mycel/v2/internal/runtime"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// If we describe every block in the schema, then what we generate from it has
// to be valid. That is the whole claim, and it is only worth making if
// something checks it: each generator's output is written to disk and put
// through the real parser, the same one `mycel validate` uses.
//
// Generated files carry TODOs, so they are not finished services — but they
// must parse. A skeleton that does not is worse than no skeleton, because the
// first thing anyone does with it is run validate.
func TestGeneratedDeclarationsParse(t *testing.T) {
	cases := []struct {
		kind string
		body string
	}{
		{"saga", renderSaga("place_order", "orders", []string{"reserve", "charge"}, schema.SagaSchema())},
		{"saga_one_step", renderSaga("place_order", "orders", []string{"reserve"}, schema.SagaSchema())},
		{"state_machine", renderStateMachine("order", "pending",
			[]string{"pending", "paid", "shipped"}, schema.StateMachineSchema())},
		{"state_machine_single", renderStateMachine("toggle", "on",
			[]string{"on"}, schema.StateMachineSchema())},
		{"validator_regex", renderValidator("nz_phone", "regex", `^\+64[0-9]{8,9}$`, schema.ValidatorSchema())},
		{"validator_cel", renderValidator("adult", "cel", "input.age >= 18", schema.ValidatorSchema())},
		{"validator_wasm", renderValidator("tax_id", "wasm", "./tax_id.wasm", schema.ValidatorSchema())},
		{"transform", renderTransform("normalize", []string{"id", "email"}, schema.TransformSchema())},
		{"transform_empty", renderTransform("passthrough", nil, schema.TransformSchema())},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			if err := parseGenerated(t, tc.body); err != nil {
				t.Fatalf("`mycel add` generated something that does not parse: %v\n\n%s", err, tc.body)
			}
		})
	}
}

// A validator carries its rule in one of three attributes, and the parser
// rejects an empty one by name. Generating a TODO there would produce a file
// that fails to parse, so the flag is demanded instead — the same call made for
// an aspect with no action.
func TestAddValidator_DemandsItsRule(t *testing.T) {
	for _, tc := range []struct{ vType, flag string }{
		{"regex", "--pattern"}, {"cel", "--expr"}, {"wasm", "--wasm"},
	} {
		addValidatorType = tc.vType
		addPattern, addExpr, addWASM = "", "", ""

		err := runAddValidator(nil, []string{"pending"})
		if err == nil {
			t.Errorf("a %s validator with no rule should be refused, not written", tc.vType)
			continue
		}
		if !strings.Contains(err.Error(), tc.flag) {
			t.Errorf("the error should name %s, got: %v", tc.flag, err)
		}
	}
	addValidatorType = ""
}

// The saga parser refuses one with no steps, and a step with no action, delay
// or await. The generator must therefore never emit either, whatever flags it
// was given.
func TestGeneratedSagaAlwaysHasAStepWithAnAction(t *testing.T) {
	body := renderSaga("empty", "orders", nil, schema.SagaSchema())

	if !strings.Contains(body, "step ") {
		t.Fatalf("a saga with no steps is rejected by the parser:\n%s", body)
	}
	if !strings.Contains(body, "action {") {
		t.Errorf("a step with no action is rejected by the parser:\n%s", body)
	}
	if !strings.Contains(body, "compensate {") {
		t.Errorf("compensation is the reason a saga exists; it should be generated:\n%s", body)
	}
}

// Every state final means every transition is refused, which reads as a bug in
// the runtime rather than an unfinished file.
func TestGeneratedStateMachineCanActuallyTransition(t *testing.T) {
	body := renderStateMachine("order", "pending", []string{"pending", "paid"}, schema.StateMachineSchema())

	if !strings.Contains(body, "transition_to") {
		t.Errorf("a machine with no transition can never leave its initial state:\n%s", body)
	}
	if !strings.Contains(body, "final = true") {
		t.Errorf("the last state should be terminal:\n%s", body)
	}
}

// The comments come from the schema's own documentation, so they cannot
// contradict what the schema declares.
func TestGeneratedCommentsComeFromTheSchema(t *testing.T) {
	body := renderValidator("x", "wasm", "", schema.ValidatorSchema())

	want := docFor(schema.ValidatorSchema(), "entrypoint")
	if want == "" {
		t.Fatal("the schema documents no entrypoint, so this test is checking nothing")
	}
	if !strings.Contains(body, want) {
		t.Errorf("generated comment does not use the schema's wording (%q):\n%s", want, body)
	}
}

// parseGenerated writes the declaration into a project and parses it the way
// mycel validate does.
func parseGenerated(t *testing.T, body string) error {
	t.Helper()

	dir := t.TempDir()
	// A minimal host project, so the declaration is parsed in the context it
	// would really live in.
	mustWrite(t, filepath.Join(dir, "config.mycel"), `
service {
  name = "generated"
}

connector "orders" {
  type   = "database"
  driver = "sqlite"

  database = ":memory:"
}
`)
	mustWrite(t, filepath.Join(dir, "generated.mycel"), body)

	p := parser.NewHCLParserWithRegistry(runtime.NewSchemaRegistry())
	_, err := p.Parse(context.Background(), dir)
	return err
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
