package runtime

import (
	"fmt"

	"github.com/matutetandil/mycel/internal/banner"
	"github.com/matutetandil/mycel/internal/flow"
	"github.com/matutetandil/mycel/internal/saga"
)

// registerSagas registers all saga configurations as flow handlers.
func (r *Runtime) registerSagas() error {
	if len(r.config.Sagas) == 0 {
		return nil
	}

	fmt.Println()
	fmt.Println("    Sagas:")

	sagaExecutor := saga.NewExecutor(r.connectors)

	for _, cfg := range r.config.Sagas {
		if cfg.From == nil {
			continue
		}

		// Create a flow config that wraps the saga trigger
		var filterConfig *flow.FilterConfig
		if cfg.From.Filter != "" {
			filterConfig = &flow.FilterConfig{
				Condition: cfg.From.Filter,
				OnReject:  "ack",
			}
		}
		flowCfg := &flow.Config{
			Name: cfg.Name,
			From: &flow.FromConfig{
				Connector:    cfg.From.Connector,
				Operation:    cfg.From.Operation,
				Filter:       cfg.From.Filter,
				FilterConfig: filterConfig,
			},
			// Minimal to config (saga handles its own output)
			To: &flow.ToConfig{
				Connector: "response",
			},
		}

		// Get source connector
		source, err := r.connectors.Get(cfg.From.Connector)
		if err != nil {
			return fmt.Errorf("saga %s: source connector not found: %w", cfg.Name, err)
		}

		handler := &FlowHandler{
			Config:            flowCfg,
			Source:            source,
			Connectors:        r.connectors,
			OperationResolver: r.operationResolver,
			NamedTransforms:   r.transforms,
			Types:             r.types,
			NamedCaches:       r.namedCaches,
			AspectExecutor:    r.aspectExecutor,
			FunctionsRegistry: r.functionsRegistry,
			SyncManager:       r.syncManager,
			SagaExecutor:      sagaExecutor,
			SagaConfig:        cfg,
		}

		r.flows.Register(cfg.Name, handler)

		// Display in banner
		method, path := r.parseFlowOperation(cfg.From.Connector, cfg.From.Operation)
		banner.PrintFlow(method, path, "saga:"+cfg.Name)
	}

	return nil
}
