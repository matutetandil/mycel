package runtime

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/auth"
	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/parser"
)

// What a service says about itself when it starts somewhere that matters.
//
// These are the settings that are fine on a laptop and expensive in
// production, and the only thing standing between them and a deployment is
// this function saying so.

func warningsFrom(t *testing.T, environment string, cfg *parser.Configuration) string {
	t.Helper()

	logs := &bytes.Buffer{}
	r := &Runtime{
		config:      cfg,
		environment: environment,
		logger:      slog.New(slog.NewTextHandler(logs, nil)),
	}
	r.printStartupWarnings()
	return logs.String()
}

func connectorWith(name, kind, driver string, props map[string]interface{}) *connector.Config {
	return &connector.Config{Name: name, Type: kind, Driver: driver, Properties: props}
}

func TestSQLiteInProductionIsWorthSaying(t *testing.T) {
	said := warningsFrom(t, "production", &parser.Configuration{
		Connectors: []*connector.Config{connectorWith("db", "database", "sqlite", nil)},
		Auth:       &auth.Config{},
	})

	if !strings.Contains(said, "SQLite") || !strings.Contains(said, "db") {
		t.Errorf("nothing was said about a sqlite database in production:\n%s", said)
	}
}

func TestNoAuthInProductionIsWorthSaying(t *testing.T) {
	said := warningsFrom(t, "production", &parser.Configuration{
		Connectors: []*connector.Config{connectorWith("api", "rest", "", nil)},
	})

	if !strings.Contains(said, "authentication") {
		t.Errorf("nothing was said about a production service with no auth:\n%s", said)
	}
}

func TestCertificateVerificationTurnedOffIsWorthSaying(t *testing.T) {
	// The setting people reach for to get past a self-signed certificate in
	// development and forget to take out. It does not relax verification, it
	// removes it: the connection stays encrypted and stops being
	// authenticated, so anything that can answer for the address can read it.
	for name, tls := range map[string]map[string]interface{}{
		"the canonical spelling": {"insecure_skip_verify": true},
		"grpc's older spelling":  {"skip_verify": true},
		"the shortest one":       {"insecure": true},
	} {
		t.Run(name, func(t *testing.T) {
			said := warningsFrom(t, "production", &parser.Configuration{
				Connectors: []*connector.Config{
					connectorWith("payments", "http", "", map[string]interface{}{"tls": tls}),
				},
				Auth: &auth.Config{},
			})

			if !strings.Contains(said, "payments") {
				t.Errorf("the connector is not named:\n%s", said)
			}
			if !strings.Contains(said, "verification") {
				t.Errorf("nothing was said about verification being off:\n%s", said)
			}
			// The reason has to be there. "Insecure" on its own reads as a
			// style note; what it means is that the connection is
			// unauthenticated.
			if !strings.Contains(said, "unauthenticated") {
				t.Errorf("the warning does not say what it costs:\n%s", said)
			}
		})
	}
}

func TestVerificationLeftOnSaysNothing(t *testing.T) {
	for name, tls := range map[string]map[string]interface{}{
		"a tls block that verifies":    {"ca_cert": "/etc/ssl/ca.pem"},
		"the setting written as false": {"insecure_skip_verify": false},
		"no tls block at all":          nil,
	} {
		t.Run(name, func(t *testing.T) {
			props := map[string]interface{}{}
			if tls != nil {
				props["tls"] = tls
			}
			said := warningsFrom(t, "production", &parser.Configuration{
				Connectors: []*connector.Config{connectorWith("payments", "http", "", props)},
				Auth:       &auth.Config{},
			})

			if strings.Contains(said, "verification") {
				t.Errorf("a connector that verifies produced a warning:\n%s", said)
			}
		})
	}
}

func TestOnALaptopNoneOfThisIsSaid(t *testing.T) {
	// Every one of these is the ordinary way to run locally, and a warning per
	// start is how people learn to stop reading them.
	for _, environment := range []string{"development", "dev", "test", ""} {
		t.Run(environment, func(t *testing.T) {
			said := warningsFrom(t, environment, &parser.Configuration{
				Connectors: []*connector.Config{
					connectorWith("db", "database", "sqlite", nil),
					connectorWith("payments", "http", "", map[string]interface{}{
						"tls": map[string]interface{}{"insecure_skip_verify": true},
					}),
				},
			})
			if said != "" {
				t.Errorf("a local run was warned at:\n%s", said)
			}
		})
	}
}

func TestStagingIsWarnedAtToo(t *testing.T) {
	// Not about sqlite, which is a production concern, but about the ones that
	// are as expensive in staging — that is where a certificate check gets
	// turned off and then promoted.
	said := warningsFrom(t, "staging", &parser.Configuration{
		Connectors: []*connector.Config{
			connectorWith("db", "database", "sqlite", nil),
			connectorWith("payments", "http", "", map[string]interface{}{
				"tls": map[string]interface{}{"insecure_skip_verify": true},
			}),
		},
		Auth: &auth.Config{},
	})

	if strings.Contains(said, "SQLite") {
		t.Errorf("staging was warned about sqlite:\n%s", said)
	}
	if !strings.Contains(said, "verification") {
		t.Errorf("staging was not warned about certificate verification:\n%s", said)
	}
}
