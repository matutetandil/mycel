package connector

import (
	"context"
	"log/slog"
	"sync"
)

// HandlerFunc is the universal handler signature used by all connectors.
type HandlerFunc func(ctx context.Context, input map[string]interface{}) (interface{}, error)

// ChainRequestResponse creates a composite handler for request-response connectors (REST, gRPC, TCP, etc.).
// The existing handler is the primary and returns the response to the caller.
// The additional handler runs concurrently as fire-and-forget in a background goroutine.
func ChainRequestResponse(existing, additional HandlerFunc, logger *slog.Logger) HandlerFunc {
	return func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		// Copy input before launching goroutine to avoid races with primary handler
		inputCopy := CopyInput(input)
		// Fire-and-forget the additional handler
		go func() {
			if _, err := additional(context.WithoutCancel(ctx), inputCopy); err != nil && logger != nil {
				logger.Warn("fan-out handler error", "error", err)
			}
		}()
		// Primary handler returns the response
		return existing(ctx, input)
	}
}

// ChainEventDriven creates a composite handler for event-driven connectors (MQ, CDC, etc.).
// Both handlers run concurrently. The function waits for all handlers to complete
// and returns the first error encountered (if any).
func ChainEventDriven(existing, additional HandlerFunc, logger *slog.Logger) HandlerFunc {
	return func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		var wg sync.WaitGroup
		var result interface{}
		var err1, err2 error

		// Copy input before launching goroutines to avoid races
		inputCopy := CopyInput(input)

		wg.Add(2)
		go func() {
			defer wg.Done()
			result, err1 = existing(ctx, input)
		}()
		go func() {
			defer wg.Done()
			_, err2 = additional(ctx, inputCopy)
		}()
		wg.Wait()

		if err1 != nil {
			return nil, err1
		}
		if err2 != nil {
			return nil, err2
		}
		return result, nil
	}
}

// CopyInput creates a shallow copy of an input map.
func CopyInput(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	cp := make(map[string]interface{}, len(input))
	for k, v := range input {
		cp[k] = v
	}
	return cp
}
