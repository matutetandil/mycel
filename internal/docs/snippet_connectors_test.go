package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/pkg/connectors"
	"github.com/matutetandil/mycel/v3/pkg/schema"
)

// Every attribute a documented connector block writes has to be one the
// connector reads.
//
// The page-table test beside this one compares the option tables. Nothing
// compared the configuration blocks the pages actually show, and a connector
// sweeps what it does not know into a bag nothing reads — so the mistake is
// silent. `user = env("RABBITMQ_USER")` on a queue connector, in three pages:
// the attribute is `username`, and the one written was ignored. It worked for
// anybody whose broker still had the default guest account and failed to
// authenticate for everybody else, from a page they copied.
func TestEveryAttributeADocumentedConnectorBlockWritesIsRead(t *testing.T) {
	reg := schema.NewRegistry()
	connectors.RegisterAll(reg)

	var (
		block     = regexp.MustCompile("(?s)```hcl\\n(.*?)\\n```")
		connector = regexp.MustCompile(`(?s)connector\s+"[^"]+"\s*\{(.*?)\n\}`)
		attribute = regexp.MustCompile(`(?m)^\s{2}([a-z_]+)\s*=`)
		typeOf    = regexp.MustCompile(`(?m)^\s*type\s*=\s*"([a-z]+)"`)
		driverOf  = regexp.MustCompile(`(?m)^\s*driver\s*=\s*"([a-z0-9_]+)"`)
	)

	for _, root := range []string{"../../docs", "../../README.md"} {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			if strings.Contains(path, "/archive/") {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}

			for _, fence := range block.FindAllStringSubmatch(string(body), -1) {
				for _, decl := range connector.FindAllStringSubmatch(fence[1], -1) {
					kind := typeOf.FindStringSubmatch(decl[1])
					if kind == nil {
						continue
					}
					driver := ""
					if m := driverOf.FindStringSubmatch(decl[1]); m != nil {
						driver = m[1]
					}

					// A block that wraps profiles declares the wrapper's
					// attributes beside the profiles' own; the type read here
					// belongs to a profile, so the two sets are not comparable.
					if strings.Contains(decl[1], "profile \"") {
						continue
					}

					accepted := acceptedByAnyDriver(reg, kind[1], driver)
					if accepted == nil {
						continue
					}

					var unknown []string
					for _, attr := range attribute.FindAllStringSubmatch(decl[1], -1) {
						name := attr[1]
						if accepted[name] || documentedElsewhere[name] != "" {
							continue
						}
						unknown = append(unknown, name)
					}
					sort.Strings(unknown)
					for _, name := range unique(unknown) {
						t.Errorf("%s: a %s connector is written with %q, which it does not read",
							path, kind[1], name)
					}
				}
			}
			return nil
		})
	}
}

// acceptedByAnyDriver is what a connector of this type reads. With no driver
// written, a page is describing the type in general, so any driver's
// attributes count — otherwise a queue example without `driver = "rabbitmq"`
// is judged against whichever driver happens to be registered first.
func acceptedByAnyDriver(reg *schema.Registry, kind, driver string) map[string]bool {
	if driver != "" {
		provider := reg.Lookup(kind, driver)
		if provider == nil {
			return nil
		}
		return acceptedBy(provider)
	}

	accepted := map[string]bool{}
	found := false
	for _, registration := range reg.AllRegistrations() {
		if registration.Type != kind {
			continue
		}
		found = true
		provider := reg.Lookup(registration.Type, registration.Driver)
		if provider == nil {
			continue
		}
		for name := range acceptedBy(provider) {
			accepted[name] = true
		}
	}
	if !found {
		return nil
	}
	return accepted
}

func unique(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
