package parser

import (
	"reflect"
	"testing"
)

// Parsing an attribute is only half of reaching the connector: a nested block
// the parser accepts but never stores is still a setting with no effect. These
// cover the two blocks added with the attributes, where the value has to be
// reshaped rather than copied.

func TestEnvBlockReachesTheConnector(t *testing.T) {
	cfg, err := tryParse(t, `
connector "runner" {
  type    = "exec"
  command = "/usr/bin/env"

  env {
    NODE_ENV = "production"
    API_KEY  = "secret"
  }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	env, ok := cfg.Connectors[0].Properties["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("env is %#v, want the map the exec connector reads", cfg.Connectors[0].Properties["env"])
	}
	if env["NODE_ENV"] != "production" || env["API_KEY"] != "secret" {
		t.Errorf("env = %#v", env)
	}
}

func TestReplicaBlocksBecomeTheListTheFactoriesRead(t *testing.T) {
	// The SQL factories read Properties["replicas"] as a list of maps, so the
	// blocks have to be collected rather than overwrite one another. Writing
	// one block per replica is the shape every other repeated thing in the
	// language uses.
	cfg, err := tryParse(t, `
connector "db" {
  type         = "database"
  driver       = "postgres"
  host         = "primary.internal"
  database     = "app"
  use_replicas = true

  replicas {
    host   = "replica-1.internal"
    port   = 5432
    weight = 2
  }

  replicas {
    host            = "replica-2.internal"
    max_connections = 10
  }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	replicas, ok := cfg.Connectors[0].Properties["replicas"].([]interface{})
	if !ok {
		t.Fatalf("replicas is %T, want the list the factories read", cfg.Connectors[0].Properties["replicas"])
	}
	if len(replicas) != 2 {
		t.Fatalf("got %d replicas, want both blocks", len(replicas))
	}

	first, _ := replicas[0].(map[string]interface{})
	if first["host"] != "replica-1.internal" || first["weight"] != 2 {
		t.Errorf("first replica = %#v", first)
	}
	second, _ := replicas[1].(map[string]interface{})
	if second["host"] != "replica-2.internal" || second["max_connections"] != 10 {
		t.Errorf("second replica = %#v", second)
	}
}

func TestWebhookRetrySpellingsFoldOntoTheSharedOnes(t *testing.T) {
	// The webhook connector grew its own words for three settings the retry
	// block already had. Both spellings work and arrive under one name, so the
	// connector reads a single vocabulary.
	for _, tc := range []struct {
		name string
		body string
	}{
		{"webhook's own spelling", `max_attempts = 5
    initial_delay = "2s"`},
		{"the shared spelling", `attempts = 5
    delay    = "2s"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := tryParse(t, `
connector "hook" {
  type = "webhook"
  url  = "https://example.com/hook"

  retry {
    `+tc.body+`
    multiplier = 2.0
  }
}
`)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			retry, _ := cfg.Connectors[0].Properties["retry"].(map[string]interface{})
			if retry["attempts"] != 5 {
				t.Errorf("attempts = %#v, want 5", retry["attempts"])
			}
			if retry["delay"] != "2s" {
				t.Errorf("delay = %#v, want 2s", retry["delay"])
			}
			// A whole number arrives as an int; the connector reads it either
			// way, which it did not before.
			if retry["multiplier"] != 2 {
				t.Errorf("multiplier = %#v", retry["multiplier"])
			}
			for _, gone := range []string{"max_attempts", "initial_delay"} {
				if _, kept := retry[gone]; kept {
					t.Errorf("the old name %s survived alongside the canonical one", gone)
				}
			}
		})
	}
}

func TestRetryRejectsTwoSpellingsOfOneSetting(t *testing.T) {
	_, err := tryParse(t, `
connector "hook" {
  type = "webhook"
  url  = "https://example.com/hook"

  retry {
    attempts     = 3
    max_attempts = 5
  }
}
`)
	if err == nil {
		t.Fatal("both spellings were accepted, so one was silently discarded")
	}
}

func TestTheNewConnectorAttributesLandInProperties(t *testing.T) {
	cfg, err := tryParse(t, `
connector "reports" {
  type          = "pdf"
  template      = "invoice.html"
  output_dir    = "/var/reports"
  page_size     = "A4"
  font          = "Helvetica"
  margin_left   = 10
  margin_top    = 15
  margin_right  = 10
}

connector "csvs" {
  type           = "file"
  path           = "/data"
  csv_delimiter  = ";"
  csv_comment    = "#"
  csv_no_header  = true
  csv_trim_space = true
  csv_skip_rows  = 2
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	pdf := cfg.Connectors[0].Properties
	for k, want := range map[string]interface{}{
		"template": "invoice.html", "output_dir": "/var/reports",
		"page_size": "A4", "font": "Helvetica",
		"margin_left": 10, "margin_top": 15, "margin_right": 10,
	} {
		if !reflect.DeepEqual(pdf[k], want) {
			t.Errorf("pdf %s = %#v, want %#v", k, pdf[k], want)
		}
	}

	csv := cfg.Connectors[1].Properties
	for k, want := range map[string]interface{}{
		"csv_delimiter": ";", "csv_comment": "#",
		"csv_no_header": true, "csv_trim_space": true, "csv_skip_rows": 2,
	} {
		if !reflect.DeepEqual(csv[k], want) {
			t.Errorf("file %s = %#v, want %#v", k, csv[k], want)
		}
	}
}
