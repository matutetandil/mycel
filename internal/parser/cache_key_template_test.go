package parser

import "testing"

// A cache key is a template, not an HCL expression.
//
// The runtime replaces ${input.id} in it when a request arrives — that is what
// interpolateKey is for, and what the guide shows. HCL sees the same ${...},
// tries to resolve `input` as a variable it does not have, and refuses the
// line: "Variables may not be used here".
//
// An aspect's cache key already fell back to the raw text when that happened.
// A flow's did not, so the same spelling worked in one block called cache and
// was refused in the other.

const interpolationPreamble = `connector "api" {
  type = "rest"
  port = 8080
}
connector "memcache" {
  type   = "cache"
  driver = "memory"
}
`

func TestACacheKeyMayInterpolateWhatArrives(t *testing.T) {
	cfg, err := tryParseFiles(t, map[string]string{"service.mycel": interpolationPreamble + `flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }
  cache {
    storage       = "memcache"
    ttl           = "5m"
    key           = "user:${input.id}"
    invalidate_on = ["user.updated:${input.id}"]
  }
}`})
	if err != nil {
		t.Fatalf("a cache key with interpolation in it was refused: %v", err)
	}

	c := cfg.Flows[0].Cache
	// The template has to arrive intact: the runtime does the replacing, so
	// anything that mangles it here produces a key nobody can look up.
	if c.Key != "user:${input.id}" {
		t.Errorf("key = %q, want the template as written", c.Key)
	}
	if len(c.InvalidateOn) != 1 || c.InvalidateOn[0] != "user.updated:${input.id}" {
		t.Errorf("invalidate_on = %v, want the template as written", c.InvalidateOn)
	}
}

func TestAKeyWithNoInterpolationIsUnchanged(t *testing.T) {
	// The ordinary case, and a CEL expression in quotes, which is the other
	// spelling a key can take.
	cfg, err := tryParseFiles(t, map[string]string{"service.mycel": interpolationPreamble + `flow "get_user" {
  from {
    connector = "api"
    operation = "GET /users/:id"
  }
  cache {
    storage = "memcache"
    key     = "'user:' + input.id"
  }
}`})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := cfg.Flows[0].Cache.Key; got != "'user:' + input.id" {
		t.Errorf("key = %q", got)
	}
}

func TestAnInvalidateBlockTakesTemplatesToo(t *testing.T) {
	cfg, err := tryParseFiles(t, map[string]string{"service.mycel": interpolationPreamble + `flow "update_user" {
  from {
    connector = "api"
    operation = "PUT /users/:id"
  }
  after {
    invalidate {
      storage  = "memcache"
      keys     = ["user:${input.id}"]
      patterns = ["users:*"]
    }
  }
}`})
	if err != nil {
		t.Fatalf("an invalidate block with interpolation was refused: %v", err)
	}

	inv := cfg.Flows[0].After.Invalidate
	if len(inv.Keys) != 1 || inv.Keys[0] != "user:${input.id}" {
		t.Errorf("keys = %v", inv.Keys)
	}
	if len(inv.Patterns) != 1 || inv.Patterns[0] != "users:*" {
		t.Errorf("patterns = %v", inv.Patterns)
	}
}

func TestAnAspectsCacheKeyDoesNotCarryItsQuotes(t *testing.T) {
	// Reading the source text brings the quotes with it, and an aspect's cache
	// key has been read that way since it was written — so every key it
	// produced had a pair of stray quote characters in it. Consistently, which
	// is why it worked and why nobody noticed: written and read under the same
	// wrong name.
	cfg, err := tryParseFiles(t, map[string]string{"service.mycel": interpolationPreamble + `aspect "cache_products" {
  on   = ["get_product*"]
  when = "before"
  cache {
    storage = "memcache"
    ttl     = "10m"
    key     = "products:${input.id}"
  }
}`})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	got := cfg.Aspects[0].Cache.Key
	if got != "products:${input.id}" {
		t.Errorf("key = %q, want it without the quotes the source carries", got)
	}
}

func TestAListOfTemplatesIsAListAndNotOneString(t *testing.T) {
	// Taking the source text of the whole expression gives one string holding
	// the brackets and every element inside it, which is not a list of
	// anything — so an invalidation of two keys invalidated neither.
	cfg, err := tryParseFiles(t, map[string]string{"service.mycel": interpolationPreamble + `flow "update_user" {
  from {
    connector = "api"
    operation = "PUT /users/:id"
  }
  after {
    invalidate {
      storage = "memcache"
      keys    = ["user:${input.id}", "users:all"]
    }
  }
}`})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	keys := cfg.Flows[0].After.Invalidate.Keys
	if len(keys) != 2 {
		t.Fatalf("keys = %v, want two", keys)
	}
	if keys[0] != "user:${input.id}" || keys[1] != "users:all" {
		t.Errorf("keys = %v", keys)
	}
}
