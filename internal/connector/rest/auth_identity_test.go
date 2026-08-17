package rest

import "testing"

// The connector built this and nothing read it. What a flow sees has to be the
// same regardless of which of the three credential types answered, or an
// expression would have to know how the service authenticates.

func TestTheIdentityIsTheSameShapeForEveryCredential(t *testing.T) {
	jwt := (&AuthContext{
		Authenticated: true,
		UserID:        "u-42",
		Claims: map[string]interface{}{
			"email": "person@example.com",
			"roles": []interface{}{"admin", "ops"},
		},
	}).identity()

	if jwt == nil {
		t.Fatal("an authenticated request produced no identity")
	}
	if jwt.UserID != "u-42" || jwt.Email != "person@example.com" {
		t.Errorf("identity = %+v", jwt)
	}
	if len(jwt.Roles) != 2 || jwt.Roles[0] != "admin" {
		t.Errorf("roles = %v", jwt.Roles)
	}

	// Basic auth has no subject beyond the name it authenticated, and a flow
	// asking for auth.user_id should still get an answer.
	basic := (&AuthContext{Authenticated: true, Username: "service-account"}).identity()
	if basic == nil || basic.UserID != "service-account" {
		t.Errorf("basic identity = %+v", basic)
	}
}

func TestAnUnauthenticatedContextHasNoIdentity(t *testing.T) {
	if id := (&AuthContext{Authenticated: false, UserID: "u-1"}).identity(); id != nil {
		t.Errorf("an unauthenticated context produced %+v", id)
	}
	var nilCtx *AuthContext
	if id := nilCtx.identity(); id != nil {
		t.Errorf("a nil context produced %+v", id)
	}
}

func TestRolesArriveInEveryShapeJSONProduces(t *testing.T) {
	for name, claims := range map[string]map[string]interface{}{
		"a list from JSON": {"roles": []interface{}{"admin", "ops"}},
		"a list of string": {"roles": []string{"admin", "ops"}},
		"a single role":    {"roles": "admin"},
	} {
		t.Run(name, func(t *testing.T) {
			id := (&AuthContext{Authenticated: true, UserID: "u", Claims: claims}).identity()
			if len(id.Roles) == 0 || id.Roles[0] != "admin" {
				t.Errorf("roles = %v", id.Roles)
			}
		})
	}

	// A claim that is not roles at all must not become one.
	id := (&AuthContext{Authenticated: true, UserID: "u", Claims: map[string]interface{}{"roles": 7}}).identity()
	if len(id.Roles) != 0 {
		t.Errorf("roles = %v, want none", id.Roles)
	}
}
