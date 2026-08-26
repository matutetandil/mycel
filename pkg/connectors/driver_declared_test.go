package connectors

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// A connector whose behaviour turns on `driver` has to say so in its schema.
//
// Five did not. A GraphQL connector is a server or a client depending on that
// one attribute, and the schema — which `mycel add` generates from, which the
// editor completes from, and which the documentation calls the single place a
// connector says what it takes — did not mention it. So the generator wrote a
// connector with no driver, the editor offered no completion for it, and the
// only way to learn the attribute existed was to read the factory.
//
// The factories are the authority here, so this reads them: a factory that
// branches on cfg.Driver is a connector whose schema owes an entry.
func TestEveryConnectorThatBranchesOnDriverDeclaresIt(t *testing.T) {
	registry := schema.NewRegistry()
	RegisterAll(registry)

	declares := map[string]bool{}
	for _, reg := range registry.AllRegistrations() {
		provider := registry.Lookup(reg.Type, reg.Driver)
		if provider == nil {
			continue
		}
		for _, attr := range provider.ConnectorSchema().Attrs {
			if attr.Name == "driver" {
				declares[reg.Type] = true
			}
		}
	}

	branches := regexp.MustCompile(`\b(cfg|config)\.Driver\b`)

	var missing []string
	root := "../../internal/connector"
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read connectors: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		connType := entry.Name()
		if declares[connType] {
			continue
		}

		found := false
		_ = filepath.Walk(filepath.Join(root, connType), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			if branches.Match(body) {
				found = true
			}
			return nil
		})
		if found {
			missing = append(missing, connType)
		}
	}

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("these connectors decide what to build from `driver` and do not declare it: %s",
			strings.Join(missing, ", "))
	}
}
