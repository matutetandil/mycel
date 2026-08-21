package runtime

import (
	"fmt"
	"log/slog"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
)

// resolveNamedOperations rewrites every reference to a named operation into the
// inline form the connectors execute.
//
// A connector may declare its operations once and have flows refer to them by
// name:
//
//	connector "api" {
//	  operation "get_user" { method = "GET", path = "/users/:id" }
//	}
//	flow "f" { from { connector = "api", operation = "get_user" } }
//
// The resolver that turns "get_user" into "GET /users/:id" has existed all
// along, but only the startup banner ever called it. Everything that runs — the
// route registration, the destination writes, the steps — read the flow
// configuration directly, so the name reached the connector verbatim. On a REST
// source that meant registering a route literally called "get_user", which
// panics the HTTP mux before the service finishes starting.
//
// Rewriting the configuration once, here, fixes every consumer at the same
// time, because they all read the same ConnectorParams map. It follows the
// pattern reusable blocks already use: references are folded before the runtime
// sees them, so the runtime has one thing to understand rather than two.
func (r *Runtime) resolveNamedOperations() error {
	if r.operationResolver == nil {
		return nil
	}

	for _, cfg := range r.config.Flows {
		if cfg.From != nil {
			if err := r.resolveOperationParam(cfg.Name, cfg.From.GetConnector(), cfg.From.ConnectorParams, "operation"); err != nil {
				return err
			}
		}

		destinations := append([]*flow.ToConfig{}, cfg.MultiTo...)
		if cfg.To != nil {
			destinations = append(destinations, cfg.To)
		}
		for _, to := range destinations {
			for _, key := range []string{"operation", "target"} {
				if err := r.resolveOperationParam(cfg.Name, to.Connector, to.ConnectorParams, key); err != nil {
					return err
				}
			}
		}

		for _, step := range cfg.Steps {
			for _, key := range []string{"operation", "target"} {
				if err := r.resolveOperationParam(cfg.Name, step.Connector, step.ConnectorParams, key); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// resolveOperationParam replaces one parameter with the resolved operation when
// it names one, and leaves it untouched otherwise.
//
// Leaving it untouched is what keeps this invisible to configuration that does
// not use named operations: a value only resolves when the connector declares
// an operation by that exact name, so inline forms — "GET /users", a table, a
// routing key — are never candidates.
func (r *Runtime) resolveOperationParam(flowName, connectorName string, params map[string]interface{}, key string) error {
	if params == nil || connectorName == "" {
		return nil
	}

	name, ok := params[key].(string)
	if !ok || name == "" {
		return nil
	}
	if !r.operationResolver.HasOperation(connectorName, name) {
		return nil
	}

	resolved, err := r.operationResolver.Resolve(connectorName, name)
	if err != nil {
		return fmt.Errorf("flow %q: operation %q on connector %q could not be resolved: %w",
			flowName, name, connectorName, err)
	}

	destKey := r.destinationParam(connectorName, resolved, key)
	if destKey != key {
		// The value moved, so the name must not stay behind under the old key
		// pretending to be a table or an operation.
		delete(params, key)
	}
	params[destKey] = resolved.Inline
	if resolved.Operation != nil {
		params[operationDefKey] = resolved.Operation
	}

	slog.Debug("resolved named operation",
		"flow", flowName, "connector", connectorName, "operation", name,
		"param", destKey, "inline", resolved.Inline)

	return nil
}

// destinationParam says which flow parameter the resolved value belongs in.
//
// For most connectors it is the one the author wrote, because the resolved
// value is the same kind of thing the inline form would have been. Databases
// are the exception: an operation declares either a table or a raw query, and
// the runtime reads those from two different parameters — a query left in
// `target` is used as a table name, which fails as a SQL syntax error naming
// the query itself.
func (r *Runtime) destinationParam(connectorName string, resolved *connector.ResolvedOperation, written string) string {
	cfg := r.operationResolver.GetConnectorConfig(connectorName)
	if cfg == nil || cfg.Type != "database" || resolved.Operation == nil {
		return written
	}

	if resolved.Operation.Query != "" {
		return "query"
	}
	if resolved.Operation.Table != "" {
		return "target"
	}
	return written
}

// operationDefKey is where the resolved definition is stashed alongside the
// rewritten value, so the parameters an operation declares stay reachable after
// the name itself is gone.
const operationDefKey = "__operation_def"

// OperationDefFor returns the definition a resolved parameter came from, if the
// value was a named operation.
func OperationDefFor(params map[string]interface{}) *connector.OperationDef {
	if params == nil {
		return nil
	}
	def, _ := params[operationDefKey].(*connector.OperationDef)
	return def
}
