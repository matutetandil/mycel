package connector

import "context"

// Headers a single request carries, as opposed to the ones a connector sends
// on every request.
//
// A step (or a destination) aimed at an HTTP-shaped connector may need to set
// a header from the message — the store view, the tenant, the locale — and
// the connector's static `headers = {...}` cannot say that, since the value
// differs per request. They travel on the context because every ability a
// connector has (Read, Write, Call) takes one and none of them takes a header
// map; the connectors that speak HTTP read them in the one place they build
// a request, and every other connector never looks.

type requestHeadersKey struct{}

// WithRequestHeaders returns a context carrying the headers the next request
// should send, over and above the connector's own. Nothing to add leaves the
// context as it was; a later set replaces an earlier one, so a step's headers
// are its own and not the previous step's.
func WithRequestHeaders(ctx context.Context, headers map[string]string) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	return context.WithValue(ctx, requestHeadersKey{}, headers)
}

// RequestHeaders returns the headers the request on this context should send,
// or nil when it declared none.
func RequestHeaders(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}
	headers, _ := ctx.Value(requestHeadersKey{}).(map[string]string)
	return headers
}
