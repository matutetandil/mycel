package mongodb

import (
	"strings"
	"testing"
)

// The parser accepted every one of these and nothing read them, so a replica
// set name or an authentication database could be written and silently had no
// effect.

func TestConnectionOptionsReachTheURI(t *testing.T) {
	got := appendMongoOptions("mongodb://db:27017", map[string]interface{}{
		"auth_source":  "admin",
		"replica_set":  "rs0",
		"read_concern": "majority",
		"direct":       true,
	})
	for _, want := range []string{
		"authSource=admin", "replicaSet=rs0",
		"readConcernLevel=majority", "directConnection=true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing %q", got, want)
		}
	}
	if strings.Count(got, "?") != 1 {
		t.Errorf("%q should carry exactly one query separator", got)
	}
}

func TestAuthDbIsTheShorterSpelling(t *testing.T) {
	got := appendMongoOptions("mongodb://db:27017", map[string]interface{}{"auth_db": "admin"})
	if !strings.Contains(got, "authSource=admin") {
		t.Errorf("auth_db did not reach the URI: %q", got)
	}

	// Written together, the longer one wins rather than the map order deciding.
	got = appendMongoOptions("mongodb://db:27017", map[string]interface{}{
		"auth_source": "primary", "auth_db": "secondary",
	})
	if !strings.Contains(got, "authSource=primary") {
		t.Errorf("auth_source should win: %q", got)
	}
}

func TestNoOptionsLeavesTheURIAlone(t *testing.T) {
	const uri = "mongodb://db:27017"
	if got := appendMongoOptions(uri, map[string]interface{}{}); got != uri {
		t.Errorf("got %q, want it untouched", got)
	}
	// A false boolean is not a setting.
	if got := appendMongoOptions(uri, map[string]interface{}{"direct": false}); got != uri {
		t.Errorf("got %q, want it untouched", got)
	}
}

func TestOptionsAppendToAnExistingQueryString(t *testing.T) {
	got := appendMongoOptions("mongodb://db:27017/?tls=true", map[string]interface{}{"replica_set": "rs0"})
	if !strings.Contains(got, "tls=true") || !strings.Contains(got, "replicaSet=rs0") {
		t.Errorf("got %q, want both", got)
	}
	if strings.Count(got, "?") != 1 {
		t.Errorf("%q should carry exactly one query separator", got)
	}
}
