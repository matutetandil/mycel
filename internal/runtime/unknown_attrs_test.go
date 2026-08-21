package runtime

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/parser"
	"github.com/matutetandil/mycel/v2/pkg/connectors"
	"github.com/matutetandil/mycel/v2/pkg/schema"
)

// Settings a connector was given and does not read.
//
// The parser keeps one list of connector attributes for all twenty-odd
// connectors, so every connector accepts every connector's words. Each parses,
// each is stored, and none is looked at — the service starts with the default
// the setting was written to replace.
//
// Three were in this repository's own examples when this was written: a
// pool_size on a database whose pool is a block, a url on a REST server that
// only listens, and a path on SQLite, which reads database.

func unreadWarningsFor(t *testing.T, cfgs ...*connector.Config) string {
	t.Helper()

	logs := &bytes.Buffer{}
	r := &Runtime{
		config: &parser.Configuration{Connectors: cfgs},
		logger: slog.New(slog.NewTextHandler(logs, nil)),
	}
	r.warnAboutUnreadAttributes(schema.NewRegistryWith(connectors.RegisterAll))
	return logs.String()
}

func TestASettingNothingReadsIsSaidOutLoud(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg       *connector.Config
		attribute string
	}{
		"a pool size where the pool is a block": {
			&connector.Config{Name: "db", Type: "database", Driver: "postgres",
				Properties: map[string]interface{}{"host": "pg", "database": "app", "pool_size": 20}},
			"pool_size",
		},
		"a url on a server that only listens": {
			&connector.Config{Name: "downstream", Type: "rest",
				Properties: map[string]interface{}{"port": 8080, "url": "http://localhost:9000"}},
			"url",
		},
		"a path on a database that reads database": {
			&connector.Config{Name: "db", Type: "database", Driver: "sqlite",
				Properties: map[string]interface{}{"path": "./catalog.db"}},
			"path",
		},
	} {
		t.Run(name, func(t *testing.T) {
			said := unreadWarningsFor(t, tc.cfg)
			if !strings.Contains(said, tc.attribute) {
				t.Errorf("nothing was said about %q:\n%s", tc.attribute, said)
			}
			if !strings.Contains(said, tc.cfg.Name) {
				t.Errorf("the connector is not named:\n%s", said)
			}
			// The reason matters: "unknown attribute" reads as a typo, and
			// what actually happens is a default standing in silently.
			if !strings.Contains(said, "default") {
				t.Errorf("the warning does not say what happens instead:\n%s", said)
			}
		})
	}
}

func TestASettingTheConnectorReadsIsNotWarnedAbout(t *testing.T) {
	for name, cfg := range map[string]*connector.Config{
		"a database with what it takes": {
			Name: "db", Type: "database", Driver: "postgres",
			Properties: map[string]interface{}{
				"host": "pg", "port": 5432, "database": "app", "user": "u", "password": "p",
			},
		},
		"a rest server": {
			Name: "api", Type: "rest",
			Properties: map[string]interface{}{"port": 8080},
		},
		"a grpc client with its keep-alive and timeout": {
			Name: "svc", Type: "grpc",
			Properties: map[string]interface{}{
				"target":  "svc:50051",
				"timeout": "30s",
				"keep_alive": map[string]interface{}{
					"time": "30s", "timeout": "10s",
				},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if said := unreadWarningsFor(t, cfg); said != "" {
				t.Errorf("a connector given what it reads was warned about:\n%s", said)
			}
		})
	}
}

func TestTheParsersOwnMarksAreNotSettings(t *testing.T) {
	// Names beginning with an underscore are what the parser leaves behind,
	// not anything somebody wrote.
	said := unreadWarningsFor(t, &connector.Config{
		Name: "db", Type: "database", Driver: "postgres",
		Properties: map[string]interface{}{"host": "pg", "database": "app", "_profiles": []interface{}{}},
	})
	if said != "" {
		t.Errorf("a parser mark was reported as a setting:\n%s", said)
	}
}

func TestAConnectorTypeNothingDescribesIsLeftAlone(t *testing.T) {
	// A plugin brings its own connector, and its schema is not in this
	// registry. Warning about every one of its settings would be noise.
	said := unreadWarningsFor(t, &connector.Config{
		Name: "sf", Type: "salesforce",
		Properties: map[string]interface{}{"instance_url": "https://x", "api_key": "k"},
	})
	if said != "" {
		t.Errorf("a connector this does not describe was warned about:\n%s", said)
	}
}
