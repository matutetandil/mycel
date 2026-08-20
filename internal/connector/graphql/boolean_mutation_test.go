package graphql

import (
	"testing"

	"github.com/graphql-go/graphql"
)

// A mutation that returns Boolean is answered by whether the write happened.
//
// `deleteUser(id: ID!): Boolean!` is served by a flow whose result is
// {"affected": 1}, and nothing turned that into a boolean: graphql-go coerced
// the map and the caller was told `false` for a row that had just been
// deleted. Verified against a running service — the row was gone and the
// answer said it was not.
func TestABooleanMutationAnswersWhetherTheWriteHappened(t *testing.T) {
	for _, c := range []struct {
		name    string
		returns graphql.Output
		result  interface{}
		want    interface{}
	}{
		{
			name:    "a delete that removed a row",
			returns: graphql.NewNonNull(graphql.Boolean),
			result:  map[string]interface{}{"affected": 1},
			want:    true,
		},
		{
			name:    "a delete that matched nothing",
			returns: graphql.NewNonNull(graphql.Boolean),
			result:  map[string]interface{}{"affected": 0},
			want:    false,
		},
		{
			name:    "a nullable Boolean is the same question",
			returns: graphql.Boolean,
			result:  map[string]interface{}{"affected": int64(3)},
			want:    true,
		},
		{
			name:    "nothing came back at all",
			returns: graphql.Boolean,
			result:  nil,
			want:    false,
		},
		{
			name:    "a flow that answered with a boolean already",
			returns: graphql.Boolean,
			result:  true,
			want:    true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := graphql.ResolveParams{Info: graphql.ResolveInfo{ReturnType: c.returns}}
			if got := asReturnType(p, c.result); got != c.want {
				t.Errorf("answered %#v, want %#v", got, c.want)
			}
		})
	}
}

// A field that returns anything else is untouched: an object stays an object.
func TestAnObjectFieldIsNotCoerced(t *testing.T) {
	user := graphql.NewObject(graphql.ObjectConfig{
		Name:   "User",
		Fields: graphql.Fields{"id": &graphql.Field{Type: graphql.ID}},
	})
	row := map[string]interface{}{"id": "u1", "affected": 1}

	p := graphql.ResolveParams{Info: graphql.ResolveInfo{ReturnType: graphql.NewNonNull(user)}}
	got, ok := asReturnType(p, row).(map[string]interface{})
	if !ok || got["id"] != "u1" {
		t.Errorf("an object field came back as %#v", got)
	}
}
