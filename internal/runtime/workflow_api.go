package runtime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/matutetandil/mycel/v3/internal/connector/rest"
	"github.com/matutetandil/mycel/v3/internal/parser"
)

// The workflow endpoints — read one, wake one, cancel one — used to be mounted
// on the admin server the moment a workflow engine was configured. That port
// carries health and metrics: read-only, unauthenticated, and reachable by
// anything on the network the process is on. These three are not read-only.
// Signalling a workflow wakes it with data the caller chooses, and the
// documentation's own example is a loan approval; cancelling stops one
// mid-flight.
//
// So they listen on their own port, they are served only when the
// configuration asks for them, and they are not served at all without
// something to check callers against. What checks them is the same
// Authenticator a REST connector uses, configured with the same auth block —
// jwt, api_key or basic — rather than a second vocabulary meaning the same
// thing.

// workflowAPIHandler builds the handler for the workflow endpoints, or nil when
// no api block was configured.
func (r *Runtime) workflowAPIHandler() (http.Handler, error) {
	api := r.workflowAPIConfig()
	if api == nil || r.workflowEngine == nil {
		return nil, nil
	}

	if len(api.Auth) == 0 {
		return nil, fmt.Errorf(
			"workflow api has no auth: these endpoints wake and cancel running workflows, so they are not served without something to check callers against")
	}

	authConfig, err := rest.AuthConfigFromMap(api.Auth)
	if err != nil {
		return nil, fmt.Errorf("workflow api auth: %w", err)
	}
	if authConfig == nil || authConfig.Type == "" {
		return nil, fmt.Errorf("workflow api auth names no way of checking callers")
	}

	mux := http.NewServeMux()
	r.registerWorkflowEndpoints(mux)

	return rest.NewAuthenticator(authConfig, r.logger).Middleware(mux), nil
}

func (r *Runtime) workflowAPIConfig() *parser.WorkflowAPIConfig {
	if r.config == nil || r.config.ServiceConfig == nil || r.config.ServiceConfig.Workflow == nil {
		return nil
	}
	return r.config.ServiceConfig.Workflow.API
}

// startWorkflowAPI brings up the listener, if the configuration asked for one.
func (r *Runtime) startWorkflowAPI() error {
	handler, err := r.workflowAPIHandler()
	if err != nil {
		return err
	}
	if handler == nil {
		return nil
	}

	api := r.workflowAPIConfig()
	addr := net.JoinHostPort(api.Host, fmt.Sprintf("%d", api.Port))

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("workflow api failed to listen on %s: %w", addr, err)
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	r.workflowAPIServer = server

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			r.logger.Error("workflow api server stopped", "error", err)
		}
	}()

	r.logger.Info("workflow api started",
		"addr", listener.Addr().String(),
		"auth", authTypeOf(api.Auth),
		"endpoints", "[/workflows/{id} /workflows/{id}/signal/{event} /workflows/{id}/cancel]")

	return nil
}

// stopWorkflowAPI shuts the listener down with the rest of the service.
func (r *Runtime) stopWorkflowAPI(ctx context.Context) error {
	if r.workflowAPIServer == nil {
		return nil
	}
	err := r.workflowAPIServer.Shutdown(ctx)
	r.workflowAPIServer = nil
	return err
}

func authTypeOf(auth map[string]interface{}) string {
	if kind, ok := auth["type"].(string); ok {
		return kind
	}
	return "unknown"
}
