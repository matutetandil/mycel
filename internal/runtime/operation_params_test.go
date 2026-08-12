package runtime

import (
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// A param block reads like a contract. All of it was parsed and none of it was
// enforced: `required` was checked by a function nothing called, `default` was
// computed into a value nothing read, and the constraints sat on the definition
// under a comment marking where the check would go.

func f64(v float64) *float64 { return &v }
func iptr(v int) *int        { return &v }

func TestDefaultsFillInWhatWasNotSent(t *testing.T) {
	op := &connector.OperationDef{Name: "list", Params: []*connector.ParamDef{
		{Name: "limit", Type: "number", Default: 100},
		{Name: "sort", Type: "string", Default: "name"},
	}}

	input := map[string]interface{}{}
	if err := applyOperationParams(op, input); err != nil {
		t.Fatalf("applyOperationParams: %v", err)
	}
	if input["limit"] != 100 || input["sort"] != "name" {
		t.Errorf("defaults not applied: %#v", input)
	}

	// A value that was sent is never replaced by the default.
	given := map[string]interface{}{"limit": 5}
	if err := applyOperationParams(op, given); err != nil {
		t.Fatalf("applyOperationParams: %v", err)
	}
	if given["limit"] != 5 {
		t.Errorf("limit = %#v, want the value that was sent", given["limit"])
	}
}

func TestRequiredIsEnforced(t *testing.T) {
	op := &connector.OperationDef{Name: "get", Params: []*connector.ParamDef{
		{Name: "id", Type: "string", Required: true},
	}}

	err := applyOperationParams(op, map[string]interface{}{})
	if err == nil {
		t.Fatal("a missing required parameter was accepted")
	}
	if !strings.Contains(err.Error(), "id is required") {
		t.Errorf("error = %q", err)
	}

	if err := applyOperationParams(op, map[string]interface{}{"id": "7"}); err != nil {
		t.Errorf("a present required parameter was rejected: %v", err)
	}
}

func TestARequiredParameterWithADefaultIsSatisfiedByIt(t *testing.T) {
	// Otherwise the two attributes contradict each other on every request that
	// omits the parameter, which is the case the default exists for.
	op := &connector.OperationDef{Name: "list", Params: []*connector.ParamDef{
		{Name: "page", Type: "number", Required: true, Default: 1},
	}}
	input := map[string]interface{}{}
	if err := applyOperationParams(op, input); err != nil {
		t.Fatalf("applyOperationParams: %v", err)
	}
	if input["page"] != 1 {
		t.Errorf("page = %#v", input["page"])
	}
}

func TestNumbersArriveAsNumbersEvenFromAQueryString(t *testing.T) {
	// Path and query parameters are strings, always. Enforcing `type = "number"`
	// strictly would reject every request that uses the feature where it is
	// most useful, so the declared type converts.
	op := &connector.OperationDef{Name: "list", Params: []*connector.ParamDef{
		{Name: "limit", Type: "number"},
		{Name: "ratio", Type: "number"},
		{Name: "active", Type: "boolean"},
	}}

	input := map[string]interface{}{"limit": "25", "ratio": "1.5", "active": "true"}
	if err := applyOperationParams(op, input); err != nil {
		t.Fatalf("applyOperationParams: %v", err)
	}
	if input["limit"] != 25 {
		t.Errorf("limit = %#v (%T), want the int 25", input["limit"], input["limit"])
	}
	if input["ratio"] != 1.5 {
		t.Errorf("ratio = %#v, want 1.5", input["ratio"])
	}
	if input["active"] != true {
		t.Errorf("active = %#v, want true", input["active"])
	}
}

func TestAValueOfTheWrongTypeIsReportedRatherThanCoerced(t *testing.T) {
	op := &connector.OperationDef{Name: "list", Params: []*connector.ParamDef{
		{Name: "limit", Type: "number"},
	}}
	err := applyOperationParams(op, map[string]interface{}{"limit": "abc"})
	if err == nil {
		t.Fatal("a non-numeric value was accepted for a number parameter")
	}
	if !strings.Contains(err.Error(), "expected a number") {
		t.Errorf("error = %q", err)
	}
}

func TestConstraintsAreEnforced(t *testing.T) {
	op := &connector.OperationDef{Name: "search", Params: []*connector.ParamDef{
		{Name: "limit", Type: "number", Min: f64(1), Max: f64(500)},
		{Name: "tenant", Type: "string", MinLength: iptr(3), MaxLength: iptr(10)},
		{Name: "sort", Type: "string", Enum: []string{"name", "email"}},
		{Name: "code", Type: "string", Pattern: "^[A-Z]{3}$"},
	}}

	for _, tc := range []struct {
		name  string
		input map[string]interface{}
		want  string
	}{
		{"below the minimum", map[string]interface{}{"limit": 0}, "at least 1"},
		{"above the maximum", map[string]interface{}{"limit": 600}, "at most 500"},
		{"too short", map[string]interface{}{"tenant": "ab"}, "at least 3"},
		{"too long", map[string]interface{}{"tenant": "abcdefghijk"}, "at most 10"},
		{"outside the enum", map[string]interface{}{"sort": "nope"}, "must be one of"},
		{"against the pattern", map[string]interface{}{"code": "abc"}, "pattern"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := applyOperationParams(op, tc.input)
			if err == nil {
				t.Fatalf("%#v was accepted", tc.input)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}

	ok := map[string]interface{}{"limit": "10", "tenant": "acme", "sort": "email", "code": "ABC"}
	if err := applyOperationParams(op, ok); err != nil {
		t.Errorf("a valid request was rejected: %v", err)
	}
}

func TestEveryProblemIsReportedAtOnce(t *testing.T) {
	// Returning on the first one makes a caller fix a request one round trip at
	// a time.
	op := &connector.OperationDef{Name: "search", Params: []*connector.ParamDef{
		{Name: "limit", Type: "number", Max: f64(10)},
		{Name: "tenant", Type: "string", Required: true},
	}}
	err := applyOperationParams(op, map[string]interface{}{"limit": 99})
	if err == nil {
		t.Fatal("two problems were accepted")
	}
	for _, want := range []string{"at most 10", "tenant is required"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q is missing %q", err, want)
		}
	}
}

func TestTheErrorReadsAsAClientError(t *testing.T) {
	// The REST connector picks a status from the error text, and a rejected
	// parameter is the caller's fault: it has to be a 400, not a 500.
	op := &connector.OperationDef{Name: "get", Params: []*connector.ParamDef{
		{Name: "id", Required: true},
	}}
	err := applyOperationParams(op, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "invalid") && !strings.Contains(msg, "required") {
		t.Errorf("error %q would be served as a 500", msg)
	}
}

func TestAnOperationWithoutParamsChangesNothing(t *testing.T) {
	input := map[string]interface{}{"anything": 1}
	if err := applyOperationParams(&connector.OperationDef{Name: "x"}, input); err != nil {
		t.Fatalf("applyOperationParams: %v", err)
	}
	if err := applyOperationParams(nil, input); err != nil {
		t.Fatalf("applyOperationParams(nil): %v", err)
	}
	if len(input) != 1 || input["anything"] != 1 {
		t.Errorf("input changed: %#v", input)
	}
}

func TestAnUndeclaredTypeLetsTheValueThrough(t *testing.T) {
	// A type name nobody recognises is not a reason to reject a request, but
	// the constraints still apply to whatever arrived.
	op := &connector.OperationDef{Name: "x", Params: []*connector.ParamDef{
		{Name: "thing", Type: "widget", MinLength: iptr(2)},
	}}
	input := map[string]interface{}{"thing": "ok"}
	if err := applyOperationParams(op, input); err != nil {
		t.Errorf("a value of an unknown declared type was rejected: %v", err)
	}
	if err := applyOperationParams(op, map[string]interface{}{"thing": "x"}); err == nil {
		t.Error("the constraint was skipped along with the type")
	}
}

func TestTheRemainingDeclaredTypes(t *testing.T) {
	op := &connector.OperationDef{Name: "x", Params: []*connector.ParamDef{
		{Name: "tags", Type: "array"},
		{Name: "meta", Type: "object"},
		{Name: "anything", Type: "any"},
		{Name: "untyped"},
	}}

	ok := map[string]interface{}{
		"tags":     []interface{}{"a", "b"},
		"meta":     map[string]interface{}{"k": "v"},
		"anything": 42,
		"untyped":  "whatever",
	}
	if err := applyOperationParams(op, ok); err != nil {
		t.Errorf("valid values were rejected: %v", err)
	}

	for _, tc := range []struct {
		name, param string
		value       interface{}
		want        string
	}{
		{"a scalar where an array is declared", "tags", "not-a-list", "expected an array"},
		{"a scalar where an object is declared", "meta", 7, "expected an object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := applyOperationParams(op, map[string]interface{}{tc.param: tc.value})
			if err == nil {
				t.Fatalf("%#v was accepted", tc.value)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want %q", err, tc.want)
			}
		})
	}
}

func TestANumberDeclaredAsAStringIsConverted(t *testing.T) {
	// A path parameter declared as a string may still arrive as a number from
	// a JSON body, and rejecting that would be pedantic rather than useful.
	op := &connector.OperationDef{Name: "x", Params: []*connector.ParamDef{
		{Name: "id", Type: "string"},
		{Name: "flag", Type: "string"},
	}}
	input := map[string]interface{}{"id": 7, "flag": true}
	if err := applyOperationParams(op, input); err != nil {
		t.Fatalf("applyOperationParams: %v", err)
	}
	if input["id"] != "7" || input["flag"] != "true" {
		t.Errorf("got %#v", input)
	}

	// A composite is not something to stringify silently.
	err := applyOperationParams(op, map[string]interface{}{"id": []interface{}{1}})
	if err == nil {
		t.Error("a list was accepted for a string parameter")
	}
}

func TestNumbersArriveFromEveryShapeTheParserProduces(t *testing.T) {
	op := &connector.OperationDef{Name: "x", Params: []*connector.ParamDef{
		{Name: "a", Type: "integer"},
		{Name: "b", Type: "float"},
		{Name: "c", Type: "int"},
	}}
	input := map[string]interface{}{"a": int64(3), "b": 1.25, "c": 9}
	if err := applyOperationParams(op, input); err != nil {
		t.Fatalf("applyOperationParams: %v", err)
	}
	if input["a"] != 3 || input["b"] != 1.25 || input["c"] != 9 {
		t.Errorf("got %#v", input)
	}

	if err := applyOperationParams(op, map[string]interface{}{"a": []interface{}{1}}); err == nil {
		t.Error("a list was accepted for a number parameter")
	}
	if err := applyOperationParams(op, map[string]interface{}{"a": map[string]interface{}{}}); err == nil {
		t.Error("an object was accepted for a boolean-free number parameter")
	}
}

func TestBooleanFromAnUnconvertibleValue(t *testing.T) {
	op := &connector.OperationDef{Name: "x", Params: []*connector.ParamDef{{Name: "on", Type: "boolean"}}}
	if err := applyOperationParams(op, map[string]interface{}{"on": "maybe"}); err == nil {
		t.Error(`"maybe" was accepted as a boolean`)
	}
	if err := applyOperationParams(op, map[string]interface{}{"on": 3}); err == nil {
		t.Error("a number was accepted as a boolean")
	}
}
