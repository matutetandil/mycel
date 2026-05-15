package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestClassifyFlowError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"context deadline", context.DeadlineExceeded, "timeout"},
		{"context canceled", context.Canceled, "canceled"},
		{"wrapped deadline", fmt.Errorf("flow failed: %w", context.DeadlineExceeded), "timeout"},
		{"timeout in message", errors.New("Client.Timeout exceeded while awaiting headers"), "timeout"},
		{"deadline in message", errors.New("request failed: context deadline exceeded"), "timeout"},
		{"validation", errors.New("input validation failed: email is required"), "validation"},
		{"invalid", errors.New("invalid payload shape"), "validation"},
		{"connection refused", errors.New("dial tcp 10.0.0.1:5672: connection refused"), "connection"},
		{"generic", errors.New("HTTP 500: internal server error"), "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyFlowError(tt.err); got != tt.want {
				t.Errorf("classifyFlowError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
