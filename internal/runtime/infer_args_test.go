package runtime

import (
	"testing"

	"github.com/matutetandil/mycel/v2/internal/flow"
)

// A GraphQL field publishes the arguments a caller may pass. Mycel works them
// out from what the flow's steps read off the input, so an argument that is not
// spotted is one a client cannot send: the query is rejected by the server as
// an unknown argument, and the field looks broken from the outside.

func argsFor(t *testing.T, params ...map[string]interface{}) map[string]*ArgDef {
	t.Helper()
	cfg := &flow.Config{
		Name: "get_user",
		From: &flow.FromConfig{Connector: "graphql"},
	}
	for _, p := range params {
		cfg.Steps = append(cfg.Steps, &flow.StepConfig{ConnectorParams: map[string]interface{}{"params": p}})
	}

	byName := make(map[string]*ArgDef)
	for _, arg := range inferArgsFromFlow(cfg, nil) {
		byName[arg.Name] = arg
	}
	return byName
}

func TestAnArgumentIsSpottedWhereTheStepReadsIt(t *testing.T) {
	args := argsFor(t, map[string]interface{}{
		"id":     "input.id",
		"tenant": "input.tenant",
	})
	if len(args) != 2 || args["id"] == nil || args["tenant"] == nil {
		t.Errorf("arguments = %v", args)
	}
}

func TestAnArgumentInsideAnExpressionIsSpottedToo(t *testing.T) {
	// A step rarely passes the field through untouched. Only a value that
	// began with "input." was read, so every one of these was invisible and
	// the field published no argument at all.
	for name, expression := range map[string]string{
		"normalised":       "lower(input.email)",
		"joined":           `input.first + " " + input.last`,
		"defaulted":        `input.limit ?? 25`,
		"inside a compare": "input.total > 100",
	} {
		t.Run(name, func(t *testing.T) {
			args := argsFor(t, map[string]interface{}{"value": expression})
			if len(args) == 0 {
				t.Errorf("%q published no argument, so a client cannot send one", expression)
			}
		})
	}
}

func TestEveryFieldAnExpressionReadsBecomesAnArgument(t *testing.T) {
	args := argsFor(t, map[string]interface{}{"name": `input.first + " " + input.last`})
	if args["first"] == nil || args["last"] == nil {
		t.Errorf("arguments = %v, want both fields the expression reads", args)
	}
}

func TestOnlyTheFieldIsTakenNotThePathBelowIt(t *testing.T) {
	// input.address.city is one argument — address — because that is what the
	// caller passes.
	args := argsFor(t, map[string]interface{}{"city": "input.address.city"})
	if args["address"] == nil || args["address.city"] != nil {
		t.Errorf("arguments = %v", args)
	}
}

func TestArgumentsAreFoundWhereverTheParamsPutThem(t *testing.T) {
	args := argsFor(t, map[string]interface{}{
		"filter": map[string]interface{}{
			"status": "input.status",
			"nested": map[string]interface{}{"owner": "input.owner"},
		},
		"ids": []interface{}{"input.primary_id", "constant"},
	})
	for _, want := range []string{"status", "owner", "primary_id"} {
		if args[want] == nil {
			t.Errorf("argument %q was not found: %v", want, args)
		}
	}
}

func TestTheSameFieldReadTwiceIsOneArgument(t *testing.T) {
	args := argsFor(t,
		map[string]interface{}{"a": "input.id"},
		map[string]interface{}{"b": "input.id"},
	)
	if len(args) != 1 {
		t.Errorf("arguments = %v, want one", args)
	}
}

func TestTheArgumentsComeOutInTheSameOrderEveryTime(t *testing.T) {
	// They are published in a schema. Built by ranging over a map, the order
	// changed on every start, so two replicas of the same service printed
	// different schemas and an exported schema file differed run to run.
	cfg := &flow.Config{
		Name: "search",
		From: &flow.FromConfig{Connector: "graphql"},
		Steps: []*flow.StepConfig{{ConnectorParams: map[string]interface{}{
			"params": map[string]interface{}{
				"a": "input.zulu", "b": "input.alpha", "c": "input.mike", "d": "input.echo",
			},
		}}},
	}

	first := names(inferArgsFromFlow(cfg, nil))
	for i := 0; i < 20; i++ {
		if got := names(inferArgsFromFlow(cfg, nil)); !equal(got, first) {
			t.Fatalf("the arguments came out as %v and then %v", first, got)
		}
	}
}

func TestAMutationWithNothingToInferTakesATypedInput(t *testing.T) {
	cfg := &flow.Config{
		Name:    "create_user",
		Returns: "user",
		From:    &flow.FromConfig{Connector: "graphql", ConnectorParams: map[string]interface{}{"operation": "Mutation.createUser"}},
	}
	args := inferArgsFromFlow(cfg, nil)
	if len(args) != 1 || args[0].Name != "input" || args[0].Type != "user" {
		t.Errorf("arguments = %+v, want a single typed input", args)
	}
}

func names(args []*ArgDef) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, a.Name)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
