package transform

import (
	"context"
	"testing"
)

func TestAsListMakesAValueSafeToIterate(t *testing.T) {
	tr, err := NewCELTransformer()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		input map[string]interface{}
		want  int
	}{
		{"a single object", map[string]interface{}{"items": map[string]interface{}{"name": "Widget"}}, 1},
		{"a list of two", map[string]interface{}{"items": []interface{}{1, 2}}, 2},
		{"a list of one", map[string]interface{}{"items": []interface{}{1}}, 1},
		{"nothing at all", map[string]interface{}{"items": nil}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := tr.Transform(context.Background(), c.input,
				[]Rule{{Target: "n", Expression: "size(as_list(input.items))"}})
			if err != nil {
				t.Fatalf("as_list: %v", err)
			}
			if got, _ := out["n"].(int64); int(got) != c.want {
				t.Errorf("size = %v, want %d", out["n"], c.want)
			}
		})
	}
}
