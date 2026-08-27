package http

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// The existing capture/captureServer helpers in method_resolution_test.go
// record what actually went over the wire, which is the only thing worth
// asserting here: the bug this guards against was invisible in every result
// the connector returned.

func newCapturing(t *testing.T) (*Connector, *capture) {
	t.Helper()
	got := &capture{}
	srv := captureServer(got)
	t.Cleanup(srv.Close)
	c := New("api", srv.URL, 0, nil, nil, 1)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return c, got
}

// sentQuery parses the query string off the recorded URL.
func sentQuery(t *testing.T, got *capture) url.Values {
	t.Helper()
	u, err := url.Parse(got.url)
	if err != nil {
		t.Fatalf("parsing recorded url %q: %v", got.url, err)
	}
	return u.Query()
}

// The message a flow writing to HTTP carries in Filters. Nested, because that
// is what made the old rendering meaningless as well as oversized.
func inboundMessage() map[string]interface{} {
	return map[string]interface{}{
		"body": map[string]interface{}{
			"metadata": map[string]interface{}{
				"headers": map[string]interface{}{"operation": "create", "elementType": "widget"},
			},
			"payload": map[string]interface{}{"sku": "ABC-1", "name": "Widget"},
		},
	}
}

// The reported bug: for a verb that carries a body, the whole message went on
// the request line as well, and once it passed the front-end proxy's
// request-line limit the request came back 414 before reaching the
// application.
func TestWrite_BodyCarryingVerbsPutNothingOnTheURL(t *testing.T) {
	for _, method := range []string{"POST", "PUT", "PATCH", "QUERY"} {
		t.Run(method, func(t *testing.T) {
			c, got := newCapturing(t)

			_, err := c.Write(context.Background(), &connector.Data{
				Target:    "/some/endpoint",
				Operation: method,
				Payload:   map[string]interface{}{"sku": "ABC-1"},
				Filters:   inboundMessage(),
			})
			if err != nil {
				t.Fatalf("Write: %v", err)
			}

			if got.url != "/some/endpoint" {
				t.Errorf("the message reached the request line: %s", got.url)
			}
			if got.body != `{"sku":"ABC-1"}` {
				t.Errorf("body = %q, want the payload", got.body)
			}
		})
	}
}

// A query string in the target survives, since it is what the flow wrote.
func TestWrite_QueryInTheTargetIsKept(t *testing.T) {
	c, got := newCapturing(t)

	_, err := c.Write(context.Background(), &connector.Data{
		Target:    "/endpoint?mode=sync",
		Operation: "POST",
		Payload:   map[string]interface{}{"sku": "ABC-1"},
		Filters:   inboundMessage(),
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got.url != "/endpoint?mode=sync" {
		t.Errorf("uri = %s, want the target's own query and nothing else", got.url)
	}
}

// Verbs with no body still address the request through the query string —
// that is the only place their data can go, and removing it would break a
// delete-by-id.
func TestWrite_BodylessVerbsStillQuery(t *testing.T) {
	for _, method := range []string{"GET", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			c, got := newCapturing(t)

			_, err := c.Write(context.Background(), &connector.Data{
				Target:    "/widgets",
				Operation: method,
				Filters:   map[string]interface{}{"id": "ABC-1", "force": true},
			})
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			if sentQuery(t, got).Get("id") != "ABC-1" || sentQuery(t, got).Get("force") != "true" {
				t.Errorf("query = %v, want the identifiers", sentQuery(t, got))
			}
		})
	}
}

// A query string carries scalars. A nested value used to be rendered with %v,
// producing Go's own map[a:map[b:c]] — not a format any receiver can decode,
// so it was not merely redundant but meaningless.
func TestWrite_NestedQueryValuesAreJSON(t *testing.T) {
	c, got := newCapturing(t)

	_, err := c.Write(context.Background(), &connector.Data{
		Target:    "/widgets",
		Operation: "DELETE",
		Filters: map[string]interface{}{
			"scope": map[string]interface{}{"region": "us", "tier": "gold"},
			"tags":  []interface{}{"a", "b"},
		},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if strings.Contains(got.url, "map%5B") || strings.Contains(got.url, "map[") {
		t.Fatalf("a nested value was rendered as Go syntax: %s", got.url)
	}

	var scope map[string]string
	if err := json.Unmarshal([]byte(sentQuery(t, got).Get("scope")), &scope); err != nil {
		t.Errorf("scope is not decodable: %q (%v)", sentQuery(t, got).Get("scope"), err)
	} else if scope["region"] != "us" || scope["tier"] != "gold" {
		t.Errorf("scope = %v", scope)
	}

	var tags []string
	if err := json.Unmarshal([]byte(sentQuery(t, got).Get("tags")), &tags); err != nil {
		t.Errorf("tags is not decodable: %q (%v)", sentQuery(t, got).Get("tags"), err)
	} else if len(tags) != 2 {
		t.Errorf("tags = %v", tags)
	}
}

// A nil has no representation in a query string; it used to be sent as the
// four characters "<nil>", which a receiver reads as a value.
func TestWrite_NilFilterIsOmitted(t *testing.T) {
	c, got := newCapturing(t)

	_, err := c.Write(context.Background(), &connector.Data{
		Target:    "/widgets",
		Operation: "DELETE",
		Filters:   map[string]interface{}{"id": "ABC-1", "parent": nil},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, present := sentQuery(t, got)["parent"]; present {
		t.Errorf("a nil filter was sent as %q", sentQuery(t, got).Get("parent"))
	}
	if sentQuery(t, got).Get("id") != "ABC-1" {
		t.Errorf("id = %q", sentQuery(t, got).Get("id"))
	}
}

// The same filters must always produce the same URL. Built by ranging a map,
// the query string came out in a different order per request, which defeats
// any cache keyed on it and makes two identical requests look different in a
// log.
func TestWrite_QueryOrderIsStable(t *testing.T) {
	filters := map[string]interface{}{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6}

	var first string
	for i := 0; i < 12; i++ {
		c, got := newCapturing(t)
		if _, err := c.Write(context.Background(), &connector.Data{
			Target: "/widgets", Operation: "GET", Filters: filters,
		}); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if i == 0 {
			first = got.url
			continue
		}
		if got.url != first {
			t.Fatalf("the same filters produced two URLs:\n  %s\n  %s", first, got.url)
		}
	}
}

// Call() had the method switch already; this pins it to the shared one and to
// the same query encoding, so the two cannot drift apart again.
func TestCall_SplitsBodyAndQueryLikeWrite(t *testing.T) {
	t.Run("body-carrying verb", func(t *testing.T) {
		c, got := newCapturing(t)

		if _, err := c.Call(context.Background(), "POST /reserve",
			map[string]interface{}{"sku": "ABC-1", "qty": 2}); err != nil {
			t.Fatalf("Call: %v", err)
		}
		if got.url != "/reserve" {
			t.Errorf("uri = %s, want no query string", got.url)
		}
		var body map[string]interface{}
		if err := json.Unmarshal([]byte(got.body), &body); err != nil {
			t.Fatalf("body %q: %v", got.body, err)
		}
		if body["sku"] != "ABC-1" {
			t.Errorf("body = %v", body)
		}
	})

	t.Run("bodyless verb encodes nested params as JSON", func(t *testing.T) {
		c, got := newCapturing(t)

		if _, err := c.Call(context.Background(), "GET /search",
			map[string]interface{}{"scope": map[string]interface{}{"region": "us"}}); err != nil {
			t.Fatalf("Call: %v", err)
		}
		if strings.Contains(got.url, "map%5B") {
			t.Fatalf("nested param rendered as Go syntax: %s", got.url)
		}
		if sentQuery(t, got).Get("scope") != `{"region":"us"}` {
			t.Errorf("scope = %q", sentQuery(t, got).Get("scope"))
		}
	})
}
