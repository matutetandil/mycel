package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/graphql-go/graphql"
)

// Fields in one GraphQL query are resolved one after another, so a query that
// asks for three things backed by three flows costs the sum of them. Measured:
// three fields over a backend that answers in 100ms took 318ms.
//
// Nothing about that is inherent. graphql-go completes a whole level before
// asking for the values it was handed — a resolver that returns a func is
// called back later, once every sibling has registered. Measured rather than
// assumed: for three siblings the order is register, register, register, then
// three resolves.
//
// That window is enough. The work starts when the field registers and the thunk
// waits for it, so the flows overlap instead of queueing, and two fields asking
// for exactly the same thing share one execution.
//
// This is what the dataloader package was there for and could not do. Batching
// in the usual sense — many keys folded into one query — needs a flow that
// accepts many keys, and a flow takes one input. It also needs somewhere to
// batch: a field of an object type cannot have a flow at all, only Query,
// Mutation and Subscription can, so the N+1 that batching exists to prevent has
// nowhere to arise.

// pendingKey is the context key for the per-request set of resolutions.
type pendingKey struct{}

// pending is one resolution in flight.
type pending struct {
	done   chan struct{}
	result interface{}
	err    error
}

// resolutions holds the work of a single request.
type resolutions struct {
	mu      sync.Mutex
	pending map[string]*pending
}

// WithResolutions returns a context that shares resolutions across the fields
// of one request. Without it every field simply runs on its own.
func WithResolutions(ctx context.Context) context.Context {
	return context.WithValue(ctx, pendingKey{}, &resolutions{pending: map[string]*pending{}})
}

// resolutionsFrom returns the request's set, or nil outside a request.
func resolutionsFrom(ctx context.Context) *resolutions {
	set, _ := ctx.Value(pendingKey{}).(*resolutions)
	return set
}

// resolutionKey identifies a resolution: the field and the arguments that make
// it different from its siblings.
//
// The whole input is encoded rather than a chosen part of it, because a flow
// decides for itself what identifies a result, and a key that guessed would
// either merge resolutions that differ or separate ones that do not.
func resolutionKey(p graphql.ResolveParams, input map[string]interface{}) string {
	field := p.Info.FieldName
	if p.Info.ParentType != nil {
		field = p.Info.ParentType.Name() + "." + field
	}

	encoded, err := json.Marshal(input)
	if err != nil {
		// Something unencodable cannot be compared with anything, so it gets a
		// key of its own and shares nothing.
		return fmt.Sprintf("%s|%p", field, input)
	}
	return field + "|" + string(encoded)
}

// startResolution begins a field's work and returns a thunk that waits for it.
//
// Returning the thunk is what buys the overlap: graphql-go asks for the value
// after every sibling has registered, by which time all of them are running.
func startResolution(ctx context.Context, p graphql.ResolveParams, input map[string]interface{}, handler HandlerFunc) func() (interface{}, error) {
	set := resolutionsFrom(ctx)
	if set == nil {
		return func() (interface{}, error) { return handler(ctx, input) }
	}

	key := resolutionKey(p, input)

	set.mu.Lock()
	if existing, running := set.pending[key]; running {
		set.mu.Unlock()
		// The same field with the same arguments, twice in one query. One
		// execution answers both.
		return existing.wait
	}
	work := &pending{done: make(chan struct{})}
	set.pending[key] = work
	set.mu.Unlock()

	go func() {
		defer close(work.done)
		work.result, work.err = handler(ctx, input)
	}()

	return work.wait
}

// wait blocks until the resolution finishes.
func (p *pending) wait() (interface{}, error) {
	<-p.done
	return p.result, p.err
}
