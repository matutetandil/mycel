package parser

import (
	"strings"
	"testing"
)

// parseResolved parses through the directory walk, which is what a service
// does: ParseFile alone does not fold reusable references, and the whole point
// of these tests is what a reference resolves to.
func parseResolved(t *testing.T, doc string) (*Configuration, error) {
	t.Helper()
	return tryParseFiles(t, map[string]string{"service.mycel": doc})
}

// The one reusable block whose reference could not be written.
//
// Ten blocks were made nameable and referenceable, and the documentation calls
// that the recommended way to write them. Cache was the only one that also kept
// `storage` as a required argument, so `cache { use = "cache.short" }` — the
// whole point of naming one — was refused with "the argument storage is
// required, but no definition was found", which names neither `use` nor the
// named block it was pointing at.
//
// The evidence it had never worked: the cache example in this repository
// declares three named caches and references none of them, writing storage out
// again in every flow.

const cacheProbePreamble = `connector "api" {
  type = "rest"
  port = 8080
}
connector "memcache" {
  type   = "cache"
  driver = "memory"
}
cache "short" {
  storage = "memcache"
  ttl     = "5m"
}
`

func TestANamedCacheCanBeReferenced(t *testing.T) {
	cfg, err := parseResolved(t, cacheProbePreamble+`flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }
  cache {
    use = "cache.short"
  }
}`)
	if err != nil {
		t.Fatalf("a flow referencing a named cache was refused: %v", err)
	}

	c := cfg.Flows[0].Cache
	if c == nil {
		t.Fatal("no cache block came back")
	}
	// The reference is kept by name and resolved when the flow runs, which is
	// where the named caches live; what matters here is that it survived
	// parsing at all.
	if c.Use != "short" {
		t.Errorf("use = %q, want the named cache", c.Use)
	}
	// And the named cache itself is there to be resolved against.
	if len(cfg.NamedCaches) != 1 || cfg.NamedCaches[0].Storage != "memcache" {
		t.Errorf("named caches = %+v", cfg.NamedCaches)
	}
}

func TestAReferenceCanStillCarryAnOverride(t *testing.T) {
	// The other half of what naming a block is for: take the base and change
	// one thing.
	cfg, err := parseResolved(t, cacheProbePreamble+`flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }
  cache {
    use = "cache.short"
    ttl = "1h"
  }
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	c := cfg.Flows[0].Cache
	if c.TTL != "1h" {
		t.Errorf("ttl = %q, want the override", c.TTL)
	}
	if c.Use != "short" {
		t.Errorf("use = %q, want the reference kept alongside the override", c.Use)
	}
}

func TestACacheBlockThatNamesNoStorageIsRefused(t *testing.T) {
	// Dropping the required argument must not let through a cache that caches
	// into nowhere.
	_, err := parseResolved(t, cacheProbePreamble+`flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }
  cache {
    ttl = "5m"
  }
}`)
	if err == nil {
		t.Fatal("a cache block naming neither a storage nor a named cache was accepted")
	}
	// And the message has to offer both ways out, since either is valid.
	if !strings.Contains(err.Error(), "storage") || !strings.Contains(err.Error(), "use") {
		t.Errorf("the error names only one of the two spellings: %v", err)
	}
}

func TestWritingStorageInlineStillWorks(t *testing.T) {
	// Every flow that already exists.
	cfg, err := parseResolved(t, cacheProbePreamble+`flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }
  cache {
    storage = "memcache"
    ttl     = "30s"
  }
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Flows[0].Cache.Storage != "memcache" {
		t.Errorf("storage = %q", cfg.Flows[0].Cache.Storage)
	}
}

// The prefixed spelling of a validator reference.
//
// The registry keys validators by their bare name, and the validators example
// in this repository documents the prefixed form — `validator = "validator.email"`
// — so writing what the example shows resolved to nothing. Silently: a
// reference that does not resolve leaves the field unvalidated rather than
// failing.
func TestAValidatorReferenceReadsEitherSpelling(t *testing.T) {
	for name, written := range map[string]string{
		"the bare name":     "corporate_email",
		"the prefixed name": "validator.corporate_email",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := tryParse(t, `validator "corporate_email" {
  type    = "regex"
  pattern = "^[a-z]+@example.com$"
}
type "user" {
  email = string({ validator = "`+written+`" })
}`)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(cfg.Types) != 1 || len(cfg.Types[0].Fields) != 1 {
				t.Fatalf("types = %+v", cfg.Types)
			}
			// Both have to arrive as the name the registry holds, or the
			// lookup misses and the field goes unvalidated.
			if got := cfg.Types[0].Fields[0].ValidatorRef; got != "corporate_email" {
				t.Errorf("validator ref = %q, want the registry's key", got)
			}
		})
	}
}
