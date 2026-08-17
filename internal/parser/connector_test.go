package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeConfigAndParse parses a document and returns the error rather than
// failing, for the cases where being rejected is the expected outcome.
func writeConfigAndParse(t *testing.T, dir, body string) error {
	t.Helper()
	path := filepath.Join(dir, "c.mycel")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := NewHCLParser().ParseFile(context.Background(), path)
	return err
}

// The connector block is the most-used part of the language and the parser is
// its real gatekeeper: an attribute missing from the allow-list is rejected no
// matter what the schema or the documentation says, and one that is accepted
// but never read lands in Properties and is silently ignored downstream. These
// tests parse realistic connectors and assert the values arrive.

func connectorNamed(t *testing.T, cfg *Configuration, name string) map[string]interface{} {
	t.Helper()
	for _, c := range cfg.Connectors {
		if c.Name == name {
			return c.Properties
		}
	}
	t.Fatalf("no connector named %q (have %d)", name, len(cfg.Connectors))
	return nil
}

func TestParseConnectorTypes(t *testing.T) {
	cfg := parseOne(t, `
connector "db" {
  type     = "database"
  driver   = "postgres"
  host     = "localhost"
  port     = 5432
  database = "app"
  user     = "postgres"
  password = "secret"

  pool {
    min = 5
    max = 20
  }
}

connector "api" {
  type = "rest"
  port = 3000

  cors {
    origins = ["*"]
    methods = ["GET", "POST"]
  }
}

connector "upstream" {
  type     = "http"
  base_url = "https://api.example.com"
  timeout  = "30s"

  auth {
    type  = "bearer"
    token = "t0ken"
  }

  retry {
    attempts = 3
  }
}

connector "rabbit" {
  type     = "mq"
  driver   = "rabbitmq"
  host     = "localhost"
  port     = 5672
  username = "guest"
  password = "guest"
  vhost    = "/"

  queue {
    name    = "orders"
    durable = true
  }

  exchange {
    name = "events"
    type = "topic"
  }

  consumer {
    auto_ack = false
    prefetch = 10

    dlq {
      enabled     = true
      queue       = "orders.dlq"
      max_retries = 3
    }
  }
}

connector "events" {
  type    = "mq"
  driver  = "kafka"
  brokers = ["broker-1:9092", "broker-2:9092"]
  client_id = "mycel"
}

connector "script" {
  type        = "exec"
  driver      = "local"
  command     = "./run.sh"
  working_dir = "/app"
  timeout     = "3m"
}
`)

	if len(cfg.Connectors) != 6 {
		t.Fatalf("parsed %d connectors, want 6", len(cfg.Connectors))
	}

	t.Run("database with a pool", func(t *testing.T) {
		p := connectorNamed(t, cfg, "db")
		if p["driver"] != "postgres" {
			t.Errorf("driver = %v", p["driver"])
		}
		// Ports arrive as numbers, not strings; a connector that gets a string
		// here fails at dial time rather than at parse time.
		if p["port"] != 5432 {
			t.Errorf("port = %#v, want the number 5432", p["port"])
		}
		pool, ok := p["pool"].(map[string]interface{})
		if !ok {
			t.Fatalf("pool did not survive as a map: %#v", p["pool"])
		}
		if pool["min"] != 5 || pool["max"] != 20 {
			t.Errorf("pool = %#v, want min 5 max 20", pool)
		}
	})

	t.Run("rest with cors", func(t *testing.T) {
		p := connectorNamed(t, cfg, "api")
		cors, ok := p["cors"].(map[string]interface{})
		if !ok {
			t.Fatalf("cors did not survive as a map: %#v", p["cors"])
		}
		origins, ok := cors["origins"].([]interface{})
		if !ok || len(origins) != 1 || origins[0] != "*" {
			t.Errorf("cors.origins = %#v", cors["origins"])
		}
		if methods, ok := cors["methods"].([]interface{}); !ok || len(methods) != 2 {
			t.Errorf("cors.methods = %#v, want two entries", cors["methods"])
		}
	})

	t.Run("http with auth and retry", func(t *testing.T) {
		p := connectorNamed(t, cfg, "upstream")
		if p["base_url"] != "https://api.example.com" {
			t.Errorf("base_url = %v", p["base_url"])
		}
		auth, ok := p["auth"].(map[string]interface{})
		if !ok {
			t.Fatalf("auth did not survive: %#v", p["auth"])
		}
		if auth["type"] != "bearer" || auth["token"] != "t0ken" {
			t.Errorf("auth = %#v", auth)
		}
		retry, ok := p["retry"].(map[string]interface{})
		if !ok {
			t.Fatalf("retry did not survive: %#v", p["retry"])
		}
		if retry["attempts"] != 3 {
			t.Errorf("retry.attempts = %#v", retry["attempts"])
		}
	})

	t.Run("rabbitmq keeps queue, exchange and the nested dlq", func(t *testing.T) {
		p := connectorNamed(t, cfg, "rabbit")
		queue, ok := p["queue"].(map[string]interface{})
		if !ok || queue["name"] != "orders" || queue["durable"] != true {
			t.Errorf("queue = %#v", p["queue"])
		}
		if ex, ok := p["exchange"].(map[string]interface{}); !ok || ex["type"] != "topic" {
			t.Errorf("exchange = %#v", p["exchange"])
		}
		consumer, ok := p["consumer"].(map[string]interface{})
		if !ok {
			t.Fatalf("consumer did not survive: %#v", p["consumer"])
		}
		if consumer["auto_ack"] != false || consumer["prefetch"] != 10 {
			t.Errorf("consumer = %#v", consumer)
		}
		// dlq nests one level deeper; it was unreachable until 1.21.3, so it
		// is worth pinning that it still arrives.
		dlq, ok := consumer["dlq"].(map[string]interface{})
		if !ok {
			t.Fatalf("dlq did not survive inside consumer: %#v", consumer["dlq"])
		}
		if dlq["enabled"] != true || dlq["queue"] != "orders.dlq" || dlq["max_retries"] != 3 {
			t.Errorf("dlq = %#v", dlq)
		}
	})

	t.Run("kafka keeps the broker list", func(t *testing.T) {
		p := connectorNamed(t, cfg, "events")
		brokers, ok := p["brokers"].([]interface{})
		if !ok || len(brokers) != 2 {
			t.Fatalf("brokers = %#v, want two entries", p["brokers"])
		}
		if brokers[0] != "broker-1:9092" {
			t.Errorf("brokers[0] = %v", brokers[0])
		}
	})

	t.Run("exec", func(t *testing.T) {
		p := connectorNamed(t, cfg, "script")
		if p["command"] != "./run.sh" || p["working_dir"] != "/app" {
			t.Errorf("command/working_dir = %v / %v", p["command"], p["working_dir"])
		}
	})
}

func TestConnectorAcceptsBothUserAndUsername(t *testing.T) {
	// MQ connectors spell it username, databases spell it user. Both have to
	// reach Properties or half the examples stop connecting.
	cfg := parseOne(t, `
connector "a" {
  type   = "database"
  driver = "postgres"
  user   = "alice"
}
connector "b" {
  type     = "mq"
  driver   = "rabbitmq"
  username = "bob"
}
`)
	if got := connectorNamed(t, cfg, "a")["user"]; got != "alice" {
		t.Errorf("user = %v", got)
	}
	if got := connectorNamed(t, cfg, "b")["username"]; got != "bob" {
		t.Errorf("username = %v", got)
	}
}

func TestConnectorRejectsAnUnknownAttribute(t *testing.T) {
	// The allow-list is the gatekeeper: an attribute nobody added is a parse
	// error, not a silently ignored value.
	dir := t.TempDir()
	if err := writeConfigAndParse(t, dir, `
connector "x" {
  type            = "rest"
  definitely_not_a_real_attribute = true
}
`); err == nil {
		t.Error("an unknown connector attribute was accepted")
	}
}

func TestConnectorRequiresANameLabel(t *testing.T) {
	dir := t.TempDir()
	if err := writeConfigAndParse(t, dir, `
connector {
  type = "rest"
}
`); err == nil {
		t.Error("a connector without a name label was accepted")
	}
}

func TestConnectorTLSAndHeaders(t *testing.T) {
	cfg := parseOne(t, `
connector "secure" {
  type     = "http"
  base_url = "https://api.example.com"

  tls {
    insecure_skip_verify = true
    ca_cert              = "/etc/ssl/ca.pem"
  }

  headers {
    X-Api-Version = "2"
  }
}
`)
	p := connectorNamed(t, cfg, "secure")
	tls, ok := p["tls"].(map[string]interface{})
	if !ok {
		t.Fatalf("tls did not survive: %#v", p["tls"])
	}
	// This one is worth asserting exactly: a skipped verification that quietly
	// defaulted to false, or a set one that quietly defaulted to true, are both
	// security-relevant.
	if tls["insecure_skip_verify"] != true {
		t.Errorf("tls.insecure_skip_verify = %#v", tls["insecure_skip_verify"])
	}
	if tls["ca_cert"] != "/etc/ssl/ca.pem" {
		t.Errorf("tls.ca_cert = %v", tls["ca_cert"])
	}
	headers, ok := p["headers"].(map[string]interface{})
	if !ok || headers["X-Api-Version"] != "2" {
		t.Errorf("headers = %#v", p["headers"])
	}
}

func TestConnectorValueTypes(t *testing.T) {
	// ctyValueToGo is what turns HCL values into the Properties map every
	// connector factory reads. Each shape has to come out as the Go type the
	// factories type-assert on.
	cfg := parseOne(t, `
connector "types" {
  type          = "rest"
  port          = 8080
  playground    = true
  introspection = false
  timeout       = "30s"

  cors {
    origins = ["a", "b", "c"]
  }
}
`)
	p := connectorNamed(t, cfg, "types")

	if _, ok := p["port"].(int); !ok {
		t.Errorf("port is %T, want int", p["port"])
	}
	if v, ok := p["playground"].(bool); !ok || !v {
		t.Errorf("playground is %#v, want true", p["playground"])
	}
	if v, ok := p["introspection"].(bool); !ok || v {
		t.Errorf("introspection is %#v, want false", p["introspection"])
	}
	if _, ok := p["timeout"].(string); !ok {
		t.Errorf("timeout is %T, want string", p["timeout"])
	}
	cors := p["cors"].(map[string]interface{})
	origins, ok := cors["origins"].([]interface{})
	if !ok || len(origins) != 3 {
		t.Fatalf("origins = %#v, want a three-element list", cors["origins"])
	}
	for i, want := range []string{"a", "b", "c"} {
		if origins[i] != want {
			t.Errorf("origins[%d] = %v, want %v", i, origins[i], want)
		}
	}
}
