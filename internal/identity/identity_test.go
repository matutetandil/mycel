package identity

import (
	"context"
	"testing"
)

// An expression reading auth.user_id runs on every request, authenticated or
// not, so the shape has to be there either way: a public endpoint asking for
// auth.user_id should see an empty string, not fail to evaluate.

func TestAnUnauthenticatedRequestStillHasTheShape(t *testing.T) {
	activation := Activation(context.Background())

	if activation["authenticated"] != false {
		t.Errorf("authenticated = %#v, want false", activation["authenticated"])
	}
	for _, key := range []string{"user_id", "email", "roles", "claims"} {
		if _, present := activation[key]; !present {
			t.Errorf("%q is missing, so an expression reading it would fail rather than see nothing", key)
		}
	}
	if roles, ok := activation["roles"].([]interface{}); !ok || len(roles) != 0 {
		t.Errorf("roles = %#v, want an empty list", activation["roles"])
	}
}

func TestTheIdentityReachesTheActivation(t *testing.T) {
	ctx := With(context.Background(), &Identity{
		UserID: "u-42",
		Email:  "person@example.com",
		Roles:  []string{"admin", "ops"},
		Claims: map[string]interface{}{"tenant": "acme"},
	})

	activation := Activation(ctx)
	if activation["authenticated"] != true {
		t.Error("authenticated = false for an authenticated request")
	}
	if activation["user_id"] != "u-42" || activation["email"] != "person@example.com" {
		t.Errorf("activation = %#v", activation)
	}

	// Roles arrive as a list CEL can use `in` against.
	roles, ok := activation["roles"].([]interface{})
	if !ok || len(roles) != 2 || roles[0] != "admin" {
		t.Errorf("roles = %#v", activation["roles"])
	}

	claims, ok := activation["claims"].(map[string]interface{})
	if !ok || claims["tenant"] != "acme" {
		t.Errorf("claims = %#v", activation["claims"])
	}
}

func TestAnIdentityWithNoClaimsIsStillReadable(t *testing.T) {
	// auth.claims.anything on such a request should be empty rather than an
	// error about a nil map.
	ctx := With(context.Background(), &Identity{UserID: "u-1"})
	claims, ok := Activation(ctx)["claims"].(map[string]interface{})
	if !ok {
		t.Fatalf("claims = %#v", Activation(ctx)["claims"])
	}
	if len(claims) != 0 {
		t.Errorf("claims = %#v, want empty", claims)
	}
}

func TestWithNilChangesNothing(t *testing.T) {
	ctx := With(context.Background(), nil)
	if From(ctx) != nil {
		t.Error("a nil identity was stored")
	}
}
