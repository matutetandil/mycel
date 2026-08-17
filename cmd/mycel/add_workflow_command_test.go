package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The rest of the add commands, driven the way somebody runs them: each writes
// one file, refuses a name already taken, and — the point of generating from
// the schema rather than a template — produces something the parser accepts.

// workflowProject prepares a directory with the connectors these commands
// check against, and clears the flags they read.
func workflowProject(t *testing.T) string {
	t.Helper()
	dir := addProject(t)

	addSagaFrom, addSagaSteps = "", ""
	addStates, addInitialState = "", ""
	addValidatorType, addPattern, addExpr, addWASM = "", "", "", ""
	addTransformFields, addFields = "", ""
	addWhen, addOn = "", ""
	addActionConnector, addActionFlow = "", ""
	t.Cleanup(func() {
		addSagaFrom, addSagaSteps = "", ""
		addStates, addInitialState = "", ""
		addValidatorType, addPattern, addExpr, addWASM = "", "", "", ""
		addTransformFields, addFields = "", ""
		addWhen, addOn = "", ""
		addActionConnector, addActionFlow = "", ""
	})

	addType = "rest"
	if err := runAddConnector(addConnectorCmd, []string{"api"}); err != nil {
		t.Fatalf("add connector: %v", err)
	}
	addType, addDriver = "database", "postgres"
	if err := runAddConnector(addConnectorCmd, []string{"orders_db"}); err != nil {
		t.Fatalf("add connector: %v", err)
	}
	addType, addDriver = "", ""

	addFrom, addTo = "api", "orders_db"
	if err := runAddFlow(addFlowCmd, []string{"create_order"}); err != nil {
		t.Fatalf("add flow: %v", err)
	}
	addFrom, addTo = "", ""

	return dir
}

// written reads back what a command wrote.
func written(t *testing.T, dir, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatalf("nothing was written to %s: %v", rel, err)
	}
	return string(body)
}

func TestEverythingTheseCommandsGenerateParses(t *testing.T) {
	// The whole claim of generating from the schema: what comes out is valid.
	// A skeleton that does not parse is worse than none, because the first
	// thing anybody does with it is run validate.
	dir := workflowProject(t)

	addSagaFrom, addSagaSteps = "api", "reserve,charge"
	if err := runAddSaga(addSagaCmd, []string{"place_order"}); err != nil {
		t.Fatalf("add saga: %v", err)
	}

	addStates, addInitialState = "pending,paid,shipped", "pending"
	if err := runAddStateMachine(addStateMachineCmd, []string{"order_status"}); err != nil {
		t.Fatalf("add state-machine: %v", err)
	}

	addValidatorType, addPattern = "regex", `^\+64[0-9]{8,9}$`
	if err := runAddValidator(addValidatorCmd, []string{"nz_phone"}); err != nil {
		t.Fatalf("add validator: %v", err)
	}

	addTransformFields = "id,email"
	if err := runAddTransform(addTransformCmd, []string{"normalise"}); err != nil {
		t.Fatalf("add transform: %v", err)
	}

	addFields = "id:number,email:string:email,name:string"
	if err := runAddType(addTypeCmd, []string{"customer"}); err != nil {
		t.Fatalf("add type: %v", err)
	}

	addOn = "create_*"
	addWhen, addActionConnector = "after", "orders_db"
	if err := runAddAspect(addAspectCmd, []string{"audit_log"}); err != nil {
		t.Fatalf("add aspect: %v", err)
	}

	if _, err := parseConfigDirQuietly(); err != nil {
		t.Fatalf("a project built entirely from these commands does not parse: %v", err)
	}

	// And each landed where the documentation says it does.
	for _, rel := range []string{
		filepath.Join("sagas", "place_order.mycel"),
		filepath.Join("state_machines", "order_status.mycel"),
		filepath.Join("validators", "nz_phone.mycel"),
		filepath.Join("transforms", "normalise.mycel"),
		filepath.Join("types", "customer.mycel"),
		filepath.Join("aspects", "audit_log.mycel"),
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("nothing at %s: %v", rel, err)
		}
	}
}

func TestASagaNeedsSomethingToStartIt(t *testing.T) {
	// Nothing else triggers a saga: without a from it loads, validates, and
	// never runs.
	workflowProject(t)

	addSagaSteps = "reserve"
	err := runAddSaga(addSagaCmd, []string{"place_order"})
	if err == nil {
		t.Fatal("a saga nothing can start was generated")
	}
	if !strings.Contains(err.Error(), "--from") {
		t.Errorf("error = %q, want it to name the flag that fixes it", err)
	}

	// And one naming a connector nobody declared.
	addSagaFrom = "a_connector_nobody_declared"
	if err := runAddSaga(addSagaCmd, []string{"place_order"}); err == nil {
		t.Error("a saga was started from a connector that does not exist")
	}
}

func TestAValidatorNeedsARule(t *testing.T) {
	// The parser rejects an empty rule by name, so generating a TODO there
	// would produce a file that does not parse.
	workflowProject(t)

	for _, vType := range []string{"regex", "cel", "wasm"} {
		t.Run(vType, func(t *testing.T) {
			addValidatorType = vType
			addPattern, addExpr, addWASM = "", "", ""
			if err := runAddValidator(addValidatorCmd, []string{"v"}); err == nil {
				t.Error("a validator with no rule was generated")
			}
		})
	}
}

func TestAStateMachineStartsSomewhereItCanBe(t *testing.T) {
	// An initial state that is not one of the states is a machine that cannot
	// make its first transition.
	dir := workflowProject(t)

	addStates, addInitialState = "pending,paid", "somewhere_else"
	if err := runAddStateMachine(addStateMachineCmd, []string{"order_status"}); err == nil {
		t.Error("a machine starting in a state it does not have was generated")
	}

	// The default, when none is named, is the first state listed.
	addStates, addInitialState = "pending,paid", ""
	if err := runAddStateMachine(addStateMachineCmd, []string{"order_status"}); err != nil {
		t.Fatalf("add state-machine: %v", err)
	}
	body := written(t, dir, filepath.Join("state_machines", "order_status.mycel"))
	if !strings.Contains(body, `initial = "pending"`) {
		t.Errorf("the machine does not start at the first state listed:\n%s", body)
	}
}

func TestAnAspectHasToDoSomethingToSomething(t *testing.T) {
	workflowProject(t)

	// An aspect matching no flow is inert, and the name is the only thing
	// tying it to one.
	addOn, addActionConnector = "nothing_matches_this_*", "orders_db"
	if err := runAddAspect(addAspectCmd, []string{"audit_log"}); err == nil {
		t.Error("an aspect matching no flow was generated")
	}

	// And one with nowhere to send what it does.
	addOn, addActionConnector, addActionFlow = "create_*", "", ""
	if err := runAddAspect(addAspectCmd, []string{"audit_log"}); err == nil {
		t.Error("an aspect with no action was generated")
	}

	// A connector and a flow are two different destinations, and an aspect
	// acts on one of them.
	addActionConnector, addActionFlow = "orders_db", "create_order"
	if err := runAddAspect(addAspectCmd, []string{"audit_log"}); err == nil {
		t.Error("an aspect acting on both a connector and a flow was generated")
	}
}

func TestATypeCarriesTheFieldsItWasGiven(t *testing.T) {
	dir := workflowProject(t)

	addFields = "id:number,email:string:email"
	if err := runAddType(addTypeCmd, []string{"customer"}); err != nil {
		t.Fatalf("add type: %v", err)
	}

	body := written(t, dir, filepath.Join("types", "customer.mycel"))
	for _, want := range []string{"id", "number", "email", "format"} {
		if !strings.Contains(body, want) {
			t.Errorf("the type does not carry %q:\n%s", want, body)
		}
	}
}

func TestListsAreReadTheWayTheyAreTyped(t *testing.T) {
	// Spaces after the commas are what anyone writes, and a step named
	// " charge" is one nothing matches.
	got := splitList(" reserve , charge ,, ship ")
	want := []string{"reserve", "charge", "ship"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}

	if len(splitList("")) != 0 {
		t.Error("an empty list produced entries")
	}
}
