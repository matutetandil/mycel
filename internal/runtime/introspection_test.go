package runtime

import (
	"context"
	"testing"
)

// What the runtime can be asked about itself.
//
// This is the whole surface the debugger, the Studio protocol and the DAP
// adapter read: a flow somebody set a breakpoint on, the connectors on offer,
// the types a payload is checked against. An answer missing here is a feature
// that shows an empty panel, which reads as "nothing is running".

const introspectableService = `
service {
  name       = "orders"
  version    = "2.0.0"
  admin_port = 9399
}

connector "api" {
  type = "rest"
  port = 3399
}

connector "store" {
  type     = "database"
  driver   = "sqlite"
  database = ":memory:"
}

type "order" {
  id    = string
  total = number
}

transform "normalise" {
  reference = "upper(input.reference)"
}

flow "list_orders" {
  from {
    connector = "api"
    operation = "GET /orders"
  }
  to {
    connector = "store"
    target    = "orders"
  }
}

flow "create_order" {
  from {
    connector = "api"
    operation = "POST /orders"
  }
  to {
    connector = "store"
    target    = "orders"
  }
}
`

func TestTheRuntimeCanBeAskedWhatItIsRunning(t *testing.T) {
	rt := newCheckRuntime(t, introspectableService)

	// Registering flows is what InitForTrace does without opening a port —
	// the debugger attaches to a service it does not want to start serving.
	if err := rt.InitForTrace(context.Background()); err != nil {
		t.Fatalf("InitForTrace: %v", err)
	}

	flows := rt.ListFlows()
	if len(flows) != 2 {
		t.Errorf("flows = %v, want both", flows)
	}

	handler, ok := rt.GetFlow("create_order")
	if !ok || handler == nil {
		t.Fatal("a flow that is running cannot be found by name")
	}

	config, ok := rt.GetFlowConfig("create_order")
	if !ok {
		t.Fatal("no configuration for a flow that is running")
	}
	if config.From.GetOperation() != "POST /orders" {
		t.Errorf("operation = %q", config.From.GetOperation())
	}

	if _, ok := rt.GetFlow("a_flow_nobody_declared"); ok {
		t.Error("a flow nobody declared was found")
	}
	if _, ok := rt.GetFlowConfig("a_flow_nobody_declared"); ok {
		t.Error("a configuration was returned for a flow nobody declared")
	}
}

func TestTheConnectorsAreListedWithTheirConfiguration(t *testing.T) {
	rt := newCheckRuntime(t, introspectableService)
	if err := rt.InitForTrace(context.Background()); err != nil {
		t.Fatalf("InitForTrace: %v", err)
	}

	names := rt.ListConnectors()
	if len(names) < 2 {
		t.Errorf("connectors = %v, want both", names)
	}

	config, ok := rt.GetConnectorConfig("store")
	if !ok {
		t.Fatal("no configuration for a connector that is running")
	}
	if config.Driver != "sqlite" {
		t.Errorf("driver = %q", config.Driver)
	}

	if _, ok := rt.GetConnectorConfig("a_connector_nobody_declared"); ok {
		t.Error("a configuration was returned for a connector nobody declared")
	}
}

func TestTheTypesAndTransformsAreListed(t *testing.T) {
	// A debugger shows the contract a payload is checked against, and the
	// named transforms a flow can refer to.
	rt := newCheckRuntime(t, introspectableService)
	if err := rt.InitForTrace(context.Background()); err != nil {
		t.Fatalf("InitForTrace: %v", err)
	}

	types := rt.ListTypes()
	if len(types) != 1 || types[0].Name != "order" {
		t.Errorf("types = %v", types)
	}

	transforms := rt.ListTransforms()
	if len(transforms) != 1 || transforms[0].Name != "normalise" {
		t.Errorf("transforms = %v", transforms)
	}
}
