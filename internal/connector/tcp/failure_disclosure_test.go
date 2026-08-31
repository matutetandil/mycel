package tcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

func TestWhatAProductionClientIsToldAboutAFailure(t *testing.T) {
	// The REST connector was the only server that asked what environment it
	// was running in. This one framed the raw error text into its reply
	// wherever it ran, and a database error is exactly the kind that carries
	// the name of a table.
	server, client := serverAndClient(t, "json")
	server.environment = "production"

	server.RegisterRoute("get_user", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, errors.New("query failed: no such table: internal_billing_table")
	})

	_, err := client.Write(context.Background(), &connector.Data{
		Target:  "get_user",
		Payload: map[string]interface{}{"id": "u-1"},
	})
	if err == nil {
		t.Fatal("a failed flow came back as a success")
	}
	if strings.Contains(err.Error(), "internal_billing_table") {
		t.Errorf("the name of an internal table was sent to the caller: %v", err)
	}
}

func TestWhatADevelopmentClientIsToldAboutAFailure(t *testing.T) {
	server, client := serverAndClient(t, "json")

	server.RegisterRoute("get_user", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return nil, errors.New("query failed: no such table: internal_billing_table")
	})

	_, err := client.Write(context.Background(), &connector.Data{
		Target:  "get_user",
		Payload: map[string]interface{}{"id": "u-1"},
	})
	if err == nil {
		t.Fatal("a failed flow came back as a success")
	}
	if !strings.Contains(err.Error(), "internal_billing_table") {
		t.Errorf("a developer was not told what actually failed: %v", err)
	}
}

// TestAServerCanBeFoundAsARouteRegistrar guards the reason none of the above
// could happen through a running Mycel until now.
//
// RegisterRoute took a HandlerFunc, and the runtime looks a connector up as a
// runtime.RouteRegistrar, an interface written with the unnamed function type.
// A defined type is not identical to it, so the assertion failed, silently, and
// no flow was ever registered on a TCP server: every message it received was
// answered "unknown message type", whatever the configuration said.
//
// The interface is restated here rather than imported because internal/runtime
// imports this package.
func TestAServerCanBeFoundAsARouteRegistrar(t *testing.T) {
	type routeRegistrar interface {
		RegisterRoute(operation string, handler func(ctx context.Context, input map[string]interface{}) (interface{}, error))
	}

	var server interface{} = &ServerConnector{}
	if _, ok := server.(routeRegistrar); !ok {
		t.Error("a TCP server cannot be found as a RouteRegistrar, so the runtime will register no flow on it")
	}
}
