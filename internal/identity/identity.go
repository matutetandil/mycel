// Package identity carries who a request belongs to, from the connector that
// authenticated it to the expressions that read it.
//
// It exists because the two ends cannot see each other: a connector
// authenticates a request and a transform evaluates `auth.user_id`, and neither
// package should know about the other. Before it, the REST connector built an
// authentication context that nothing ever read, and `auth` was not a CEL
// variable at all — so `auth.user_id` and `auth.claims.*`, which the auth guide
// documents and an example uses, could not be written anywhere.
package identity

import "context"

type contextKey struct{}

// Identity is what a connector learned about the caller.
type Identity struct {
	// UserID is who the credential belongs to, when the credential says.
	UserID string

	// Email, when it is known.
	Email string

	// Roles carried by the credential.
	Roles []string

	// Claims is everything else the credential or the provider returned, so a
	// field nobody thought to map is still reachable.
	Claims map[string]interface{}
}

// With returns a context carrying the identity.
func With(ctx context.Context, id *Identity) context.Context {
	if id == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, id)
}

// From returns the identity on the context, or nil for an unauthenticated
// request.
func From(ctx context.Context) *Identity {
	id, _ := ctx.Value(contextKey{}).(*Identity)
	return id
}

// Activation renders the identity the way an expression reads it.
//
// An unauthenticated request gets an empty map rather than nothing, so that
// `auth.user_id` on a public endpoint is empty instead of an evaluation error —
// the same treatment a missing input field gets.
func Activation(ctx context.Context) map[string]interface{} {
	id := From(ctx)
	if id == nil {
		return map[string]interface{}{
			"authenticated": false,
			"user_id":       "",
			"email":         "",
			"roles":         []interface{}{},
			"claims":        map[string]interface{}{},
		}
	}

	roles := make([]interface{}, 0, len(id.Roles))
	for _, role := range id.Roles {
		roles = append(roles, role)
	}
	claims := id.Claims
	if claims == nil {
		claims = map[string]interface{}{}
	}

	return map[string]interface{}{
		"authenticated": true,
		"user_id":       id.UserID,
		"email":         id.Email,
		"roles":         roles,
		"claims":        claims,
	}
}
