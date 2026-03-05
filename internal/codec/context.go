package codec

import "context"

type contextKey string

const formatKey contextKey = "mycel_format"

// WithFormat returns a new context with the specified format.
func WithFormat(ctx context.Context, format string) context.Context {
	return context.WithValue(ctx, formatKey, format)
}

// FormatFromContext returns the format from the context, or empty string if not set.
func FormatFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(formatKey).(string); ok {
		return v
	}
	return ""
}
