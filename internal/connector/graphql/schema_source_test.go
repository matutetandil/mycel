package graphql

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// Where a GraphQL server's schema comes from.
//
// auto_generate was stored in the config and read by nothing: the source was
// decided entirely by whether a path was given. So a server configured with no
// path and auto_generate = false — which says, in two ways, that there is no
// schema — started happily and answered every query with an empty one, which
// reads as a service that is up and knows nothing.

func graphqlConfig(schema map[string]interface{}) *connector.Config {
	return &connector.Config{
		Name: "api",
		Type: "graphql",
		Properties: map[string]interface{}{
			"mode":   "server",
			"path":   "/graphql",
			"schema": schema,
		},
	}
}

func TestWhereTheSchemaComesFrom(t *testing.T) {
	for name, tc := range map[string]struct {
		schema   map[string]interface{}
		accepted bool
	}{
		"a file": {
			map[string]interface{}{"path": "schema.graphql"}, true,
		},
		"the type blocks": {
			map[string]interface{}{"auto_generate": true}, true,
		},
		"nothing said, which is the type blocks": {
			map[string]interface{}{}, true,
		},
		"neither, which is nothing at all": {
			map[string]interface{}{"auto_generate": false}, false,
		},
		"both, which is a contradiction": {
			map[string]interface{}{"path": "schema.graphql", "auto_generate": true}, false,
		},
		"a file, and explicitly not generated": {
			map[string]interface{}{"path": "schema.graphql", "auto_generate": false}, true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewFactory(slog.Default()).Create(context.Background(), graphqlConfig(tc.schema))
			if tc.accepted && err != nil {
				t.Errorf("refused: %v", err)
			}
			if !tc.accepted && err == nil {
				t.Error("accepted a schema block that names no source")
			}
		})
	}
}

func TestTheRefusalSaysWhatIsMissing(t *testing.T) {
	_, err := NewFactory(slog.Default()).Create(context.Background(), graphqlConfig(map[string]interface{}{"auto_generate": false}))
	if err == nil {
		t.Fatal("accepted")
	}
	if !strings.Contains(err.Error(), "auto_generate") || !strings.Contains(err.Error(), "path") {
		t.Errorf("the error names neither setting: %v", err)
	}
}
