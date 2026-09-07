package parser

import (
	"strings"
	"testing"
)

// `key_from` on a cache block is a CEL expression yielding the key, for the
// requests a `${...}` template of scalars cannot identify.
func TestACacheBlockTakesKeyFrom(t *testing.T) {
	cfg := mustParse(t, `
flow "gallery" {
  from {
    connector = "api"
    operation = "GET /gallery"
  }
  cache {
    storage  = "redis"
    ttl      = "1h"
    key_from = <<-CEL
      'gallery:' + (input.store ?? 'default') + ':' +
      hash_sha256(join(as_list(input.filter).map(f, f.attribute_code + '=' + f.value), '|'))
    CEL
  }
  to {
    connector = "db"
    target    = "items"
  }
}
`)
	c := cfg.Flows[0].Cache
	if c == nil || !strings.Contains(c.KeyFrom, "hash_sha256(") || c.Key != "" {
		t.Errorf("cache = %+v", c)
	}
}

func TestKeyAndKeyFromTogetherAreRefused(t *testing.T) {
	_, err := tryParse(t, `
flow "gallery" {
  from {
    connector = "api"
    operation = "GET /gallery"
  }
  cache {
    storage  = "redis"
    key      = "gallery:${input.store}"
    key_from = "'gallery:' + input.store"
  }
  to {
    connector = "db"
    target    = "items"
  }
}
`)
	if err == nil {
		t.Fatal("a cache block with both key and key_from parsed")
	}
	for _, want := range []string{"key", "key_from"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

func TestAnAspectsCacheTakesKeyFromInsteadOfKey(t *testing.T) {
	cfg := mustParse(t, `
aspect "cached_lookup" {
  when = "around"
  on   = ["get_*"]
  cache {
    storage  = "redis"
    ttl      = "5m"
    key_from = "input._flow + ':' + hash_sha256(join(as_list(input.ids), ','))"
  }
}
`)
	c := cfg.Aspects[0].Cache
	if c == nil || !strings.Contains(c.KeyFrom, "hash_sha256(") || c.Key != "" {
		t.Errorf("aspect cache = %+v", c)
	}

	// One of the two still has to be there.
	_, err := tryParse(t, `
aspect "cached_lookup" {
  when = "around"
  on   = ["get_*"]
  cache {
    storage = "redis"
    ttl     = "5m"
  }
}
`)
	if err == nil || !strings.Contains(err.Error(), "key") {
		t.Errorf("an aspect cache with neither key nor key_from: %v", err)
	}
}
