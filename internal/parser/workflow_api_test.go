package parser

import (
	"strings"
	"testing"
)

// A long-running workflow outlives the request that started it, so the only way
// to see one, wake it or stop it is over HTTP. Those three endpoints used to be
// mounted on the admin server — the port that carries health and metrics, which
// is read-only and unauthenticated by design — where anything that could reach
// the port could approve a loan by signalling the workflow waiting for it.
//
// They have their own listener now, and it does not start without something to
// check callers against.

const workflowService = `
service {
  name = "orders"

  workflow {
    storage = "db"

    api {
      port = 9091

      auth {
        type   = "api_key"
        header = "X-Workflow-Key"
        keys   = ["k-1", "k-2"]
      }
    }
  }
}
`

func TestTheWorkflowApiIsWhereTheConfigurationPutsIt(t *testing.T) {
	cfg := mustParseProfiles(t, workflowService)

	api := cfg.ServiceConfig.Workflow.API
	if api == nil {
		t.Fatal("the api block was not read")
	}
	if api.Port != 9091 {
		t.Errorf("port = %d", api.Port)
	}
	// The same shape a connector's auth block parses to, so the words are the
	// ones somebody already knows.
	if api.Auth == nil || api.Auth["type"] != "api_key" {
		t.Fatalf("auth = %+v", api.Auth)
	}
	if api.Auth["header"] != "X-Workflow-Key" {
		t.Errorf("auth = %+v", api.Auth)
	}
	keys, _ := api.Auth["keys"].([]interface{})
	if len(keys) != 2 {
		t.Errorf("keys = %#v", api.Auth["keys"])
	}
}

func TestWithNoApiBlockThereIsNoWorkflowApi(t *testing.T) {
	// Configuring a workflow engine is not asking for an HTTP interface to it.
	cfg := mustParseProfiles(t, `
service {
  name = "orders"
  workflow {
    storage = "db"
  }
}
`)
	if cfg.ServiceConfig.Workflow.API != nil {
		t.Error("an api was configured although nothing asked for one")
	}
}

func TestAWorkflowApiWithNothingToCheckCallersAgainstIsRefused(t *testing.T) {
	// These endpoints wake a paused workflow with data the caller chooses and
	// cancel one that is running. Opening that with no credentials at all is
	// not a default anybody should get by leaving a block out.
	_, err := parsed(t, `
service {
  name = "orders"
  workflow {
    storage = "db"
    api {
      port = 9091
    }
  }
}
`)
	if err == nil {
		t.Fatal("a workflow api with no authentication was accepted")
	}
	if !strings.Contains(err.Error(), "auth") {
		t.Errorf("error = %q, want it to say what is missing", err)
	}
}

func TestAWorkflowApiWithNoKeysIsRefused(t *testing.T) {
	// An auth block that checks nothing is the same as no auth block, and it
	// reads as though it were configured.
	_, err := parsed(t, `
service {
  name = "orders"
  workflow {
    storage = "db"
    api {
      port = 9091
      auth {
        type = "api_key"
      }
    }
  }
}
`)
	if err == nil {
		t.Error("a workflow api whose auth accepts anything was accepted")
	}
}

func TestAWorkflowApiCanBeCheckedWithAPasswordInstead(t *testing.T) {
	cfg := mustParseProfiles(t, `
service {
  name = "orders"
  workflow {
    storage = "db"
    api {
      auth {
        type = "basic"
        users = {
          ops = "s3cret"
        }
      }
    }
  }
}
`)
	api := cfg.ServiceConfig.Workflow.API
	users, _ := api.Auth["users"].(map[string]interface{})
	if api.Auth["type"] != "basic" || users["ops"] != "s3cret" {
		t.Errorf("auth = %+v", api.Auth)
	}
	// A port nobody named still has to be one nobody has to guess.
	if api.Port != 9091 {
		t.Errorf("port = %d, want the documented default", api.Port)
	}
}

func TestAWayOfCheckingCallersNobodyImplementsIsRefused(t *testing.T) {
	_, err := parsed(t, `
service {
  name = "orders"
  workflow {
    storage = "db"
    api {
      auth {
        type = "mtls"
      }
    }
  }
}
`)
	if err == nil {
		t.Error("an authentication type nothing implements was accepted")
	}
}

func TestTheWorkflowApiCannotShareTheAdminPort(t *testing.T) {
	// The admin port carries health and metrics and is read-only by design.
	// Putting a mutating interface back on it, by hand this time, is the thing
	// this block exists to undo.
	_, err := parsed(t, `
service {
  name       = "orders"
  admin_port = 9090

  workflow {
    storage = "db"
    api {
      port = 9090
      auth {
        type = "api_key"
        keys = ["k-1"]
      }
    }
  }
}
`)
	if err == nil {
		t.Fatal("the workflow api was put on the admin port")
	}
	if !strings.Contains(err.Error(), "admin") {
		t.Errorf("error = %q, want it to say why", err)
	}
}
