package transform

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// TestCELValueToNativeNil covers nil and types.Null inputs.
func TestCELValueToNativeNil(t *testing.T) {
	if got := CELValueToNative(nil); got != nil {
		t.Errorf("CELValueToNative(nil) = %v, want nil", got)
	}
	if got := CELValueToNative(types.NullValue); got != nil {
		t.Errorf("CELValueToNative(types.NullValue) = %v, want nil", got)
	}
}

// TestCELValueToNativeMapRefVal exercises the case the bug is about: a Go
// map with ref.Val keys/values should unwrap fully so json.Marshal accepts
// the result.
func TestCELValueToNativeMapRefVal(t *testing.T) {
	in := map[ref.Val]ref.Val{
		types.String("us"): types.Bool(true),
		types.String("uk"): types.Bool(false),
	}
	got := CELValueToNative(in)
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	// The two key orderings are both valid since maps are unordered.
	if string(out) != `{"uk":false,"us":true}` && string(out) != `{"us":true,"uk":false}` {
		t.Errorf("unexpected JSON: %s", out)
	}
}

// TestCELValueToNativeListRefVal: []ref.Val should unwrap to []interface{}.
func TestCELValueToNativeListRefVal(t *testing.T) {
	in := []ref.Val{types.String("x"), types.Int(42), types.Bool(true)}
	got := CELValueToNative(in)
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	want := `["x",42,true]`
	if string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}
}

// TestCELValueToNativeNonStringKeys: maps keyed by something other than a
// string must be stringified for JSON output.
func TestCELValueToNativeNonStringKeys(t *testing.T) {
	in := map[ref.Val]ref.Val{
		types.Int(1): types.String("one"),
		types.Int(2): types.String("two"),
	}
	got := CELValueToNative(in)
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	// One of the two valid orderings.
	if string(out) != `{"1":"one","2":"two"}` && string(out) != `{"2":"two","1":"one"}` {
		t.Errorf("unexpected JSON: %s", out)
	}
}

// TestTransformNestedMapEncodesAsJSON is the end-to-end scenario from the bug
// report: a transform that passes a nested object straight through must
// produce JSON-encodable output.
func TestTransformNestedMapEncodesAsJSON(t *testing.T) {
	tr, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	// Mimics a JSON-decoded RabbitMQ message body.
	input := map[string]interface{}{
		"body": map[string]interface{}{
			"payload": map[string]interface{}{
				"styleNumber": "AI02LT",
				"styleName":   "Axil",
				"websites": map[string]interface{}{
					"us": true,
					"uk": true,
					"fr": false,
				},
				"items": []interface{}{
					map[string]interface{}{"sku": "A", "qty": 1},
					map[string]interface{}{"sku": "B", "qty": 2},
				},
			},
		},
	}

	rules := []Rule{
		{Target: "styleNumber", Expression: "input.body.payload.styleNumber"},
		{Target: "name", Expression: "coalesce(input.body.payload.styleName, '')"},
		{Target: "websites", Expression: "input.body.payload.websites"},
		{Target: "items", Expression: "input.body.payload.items"},
	}

	out, err := tr.Transform(context.Background(), input, rules)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal of transform output failed: %v", err)
	}

	// Round-trip and check structure rather than exact bytes (map ordering).
	var back map[string]interface{}
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v", err)
	}
	if back["styleNumber"] != "AI02LT" {
		t.Errorf("styleNumber: got %v", back["styleNumber"])
	}
	websites, ok := back["websites"].(map[string]interface{})
	if !ok {
		t.Fatalf("websites not a map: %T", back["websites"])
	}
	if websites["us"] != true || websites["uk"] != true || websites["fr"] != false {
		t.Errorf("websites mismatch: %+v", websites)
	}
	items, ok := back["items"].([]interface{})
	if !ok || len(items) != 2 {
		t.Fatalf("items not a 2-element list: %T %v", back["items"], back["items"])
	}
	first, _ := items[0].(map[string]interface{})
	if first["sku"] != "A" {
		t.Errorf("items[0].sku: got %v", first["sku"])
	}
}

// TestTransformCELLiteralMapEncodesAsJSON: maps constructed inside CEL via
// `{}` literals must also unwrap correctly. This catches the pure
// CEL-internal `map[ref.Val]ref.Val` case.
func TestTransformCELLiteralMapEncodesAsJSON(t *testing.T) {
	tr, err := NewCELTransformer()
	if err != nil {
		t.Fatalf("NewCELTransformer: %v", err)
	}

	out, err := tr.Transform(context.Background(),
		map[string]interface{}{},
		[]Rule{
			{Target: "obj", Expression: `{"a": 1, "b": "x"}`},
			{Target: "list", Expression: `[1, "two", true]`},
		},
	)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var back map[string]interface{}
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("round-trip failed: %v", err)
	}
	obj, _ := back["obj"].(map[string]interface{})
	if obj["a"] != float64(1) || obj["b"] != "x" {
		t.Errorf("obj mismatch: %+v", obj)
	}
	list, _ := back["list"].([]interface{})
	if len(list) != 3 {
		t.Fatalf("list len: %d", len(list))
	}
}
