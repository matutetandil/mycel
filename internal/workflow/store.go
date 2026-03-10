package workflow

import "context"

// Store defines the persistence interface for workflow instances.
type Store interface {
	// EnsureSchema creates the workflow table/collection if it doesn't exist.
	EnsureSchema(ctx context.Context) error

	// Save creates or updates a workflow instance.
	Save(ctx context.Context, instance *Instance) error

	// Get retrieves a workflow instance by ID.
	Get(ctx context.Context, id string) (*Instance, error)

	// FindActive returns all instances with status "running" or "paused".
	FindActive(ctx context.Context) ([]*Instance, error)

	// FindReady returns paused instances whose resume_at has passed (delay expired).
	FindReady(ctx context.Context) ([]*Instance, error)

	// FindExpired returns running or paused instances whose expires_at or step_expires_at has passed.
	FindExpired(ctx context.Context) ([]*Instance, error)

	// FindByEvent returns paused instances awaiting a specific event name.
	FindByEvent(ctx context.Context, event string) ([]*Instance, error)

	// Delete removes a workflow instance.
	Delete(ctx context.Context, id string) error
}
