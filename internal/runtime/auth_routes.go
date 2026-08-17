package runtime

import (
	"log/slog"
	"net/http"

	"github.com/matutetandil/mycel/v2/internal/connector/rest"
	"github.com/matutetandil/mycel/v2/internal/connector/webhook"
)

// mountAuthEndpoints attaches the auth endpoints to every REST server.
//
// The auth block builds a manager and a handler at startup and logs that the
// system is initialised. Until this existed, that was the whole of it: nothing
// ever called the handler's RegisterRoutes, so /auth/login, /auth/register,
// /auth/me and the rest — documented, configurable, and enabled by default —
// answered 404 on a running service.
//
// They are mounted on each REST connector because that is where an HTTP client
// can reach them; a service with two REST servers on different ports gets them
// on both, since either is a legitimate front door.
func (r *Runtime) mountAuthEndpoints() {
	if r.authHandler == nil {
		return
	}

	mounted := 0
	for _, name := range r.connectors.Names() {
		conn, err := r.connectors.Get(name)
		if err != nil {
			continue
		}
		server, ok := conn.(*rest.Connector)
		if !ok {
			continue
		}
		r.authHandler.RegisterRoutes(restMux{server})
		mounted++
	}

	switch mounted {
	case 0:
		// Worth saying: the configuration asks for an auth system and there is
		// no HTTP server to reach it through.
		slog.Warn("auth is configured but no rest connector is defined, so its endpoints are not reachable")
	default:
		slog.Info("auth endpoints mounted", "rest_connectors", mounted)
	}
}

// restMux adapts a REST connector to the shape the auth handler mounts on.
type restMux struct {
	conn *rest.Connector
}

func (m restMux) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	m.conn.MountHandler(pattern, handler)
}

// mountInboundWebhooks gives every inbound webhook connector a path to be
// reached at.
//
// The connector verifies signatures, checks the allowed addresses and builds
// the event — and nothing served it: the server that mounts its path was never
// constructed, so a webhook connector in inbound mode could be configured,
// could start, and could not receive anything.
//
// They are mounted on the REST servers rather than on a listener of their own,
// so that a service exposes one port and a webhook is one more path on it.
func (r *Runtime) mountInboundWebhooks() {
	var servers []*rest.Connector
	for _, name := range r.connectors.Names() {
		conn, err := r.connectors.Get(name)
		if err != nil {
			continue
		}
		if server, ok := conn.(*rest.Connector); ok {
			servers = append(servers, server)
		}
	}

	for _, name := range r.connectors.Names() {
		conn, err := r.connectors.Get(name)
		if err != nil {
			continue
		}
		inbound, ok := conn.(*webhook.InboundConnector)
		if !ok {
			continue
		}

		if len(servers) == 0 {
			slog.Warn("an inbound webhook has no rest connector to be reached through",
				"connector", name, "path", inbound.Path())
			continue
		}
		for _, server := range servers {
			server.MountHandler(inbound.Path(), inbound.HandleHTTP)
		}

		// And connect the flows that read from it. The usual registration only
		// covers connectors that start a listener of their own, and this one is
		// served by the REST connector above, so it would otherwise receive
		// requests with no flow behind them.
		r.registerFlowHandlers(name, inbound)

		slog.Info("inbound webhook mounted", "connector", name, "path", inbound.Path())
	}
}
