package runtime

import (
	"context"

	"github.com/matutetandil/mycel/v3/internal/aspect"
	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/parser"
	"github.com/matutetandil/mycel/v3/internal/transform"
	"github.com/matutetandil/mycel/v3/internal/validate"
)

// A hot reload replaces everything a service is built from at once, and until
// now it did so by dismantling what was running before it knew the replacement
// worked: the old connectors were closed and the flow registry emptied, and
// only then were the new ones built. Anything that failed after that point —
// a driver name with a typo, an aspect naming a flow that no longer exists, a
// connector that cannot reach its destination — left the process alive and
// serving nothing at all. Measured on a two-connector service: one flow
// registered before, zero after, with the failure reported only to the log.
//
// "Rollback" restored the configuration pointer alone, which is the one thing
// that mattered least; the registry, the flows, the aspects and the closed
// connections all stayed as the failed reload had left them.
//
// So the new configuration is now built beside the old one and only swapped in
// once it stands up. Nothing is closed until then, which is safe because a
// connector binds its port in Start and not in Connect — the two generations
// can be connected at once, and only the winner is ever started.

// reloadSnapshot is everything a reload replaces, kept so a failed one can put
// it all back.
type reloadSnapshot struct {
	config            *parser.Configuration
	connectors        *connector.Registry
	operationResolver *connector.OperationResolver
	transforms        map[string]*transform.Config
	types             map[string]*validate.TypeSchema
	namedCaches       map[string]*flow.NamedCacheConfig
	aspectRegistry    *aspect.Registry
	aspectExecutor    *aspect.Executor
	flows             *FlowRegistry
	suspendedStarters []suspendedConnector
}

// snapshotForReload records the running state before a reload touches it.
func (r *Runtime) snapshotForReload() *reloadSnapshot {
	return &reloadSnapshot{
		config:            r.config,
		connectors:        r.connectors,
		operationResolver: r.operationResolver,
		transforms:        r.transforms,
		types:             r.types,
		namedCaches:       r.namedCaches,
		aspectRegistry:    r.aspectRegistry,
		aspectExecutor:    r.aspectExecutor,
		flows:             r.flows,
		suspendedStarters: r.suspendedStarters,
	}
}

// restore puts the running state back after a reload that did not stand up.
//
// The connectors it restores were never closed, and the flows never
// unregistered, so the service carries on with the configuration it already
// had — which is the only configuration known to work.
func (r *Runtime) restore(previous *reloadSnapshot) {
	r.config = previous.config
	r.connectors = previous.connectors
	r.operationResolver = previous.operationResolver
	r.transforms = previous.transforms
	r.types = previous.types
	r.namedCaches = previous.namedCaches
	r.aspectRegistry = previous.aspectRegistry
	r.aspectExecutor = previous.aspectExecutor
	r.flows = previous.flows
	r.suspendedStarters = previous.suspendedStarters
}

// abandon closes whatever the failed reload managed to open, so a reload that
// fails repeatedly does not leak a connection to every destination each time.
func (r *Runtime) abandon(ctx context.Context, built *connector.Registry) {
	if built == nil || built == r.connectors {
		return
	}
	if err := built.CloseAll(ctx); err != nil {
		r.logger.Debug("closing the connectors of an abandoned reload", "error", err)
	}
}
