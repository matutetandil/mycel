package runtime

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/matutetandil/mycel/v3/internal/connector/cache/memory"
	"github.com/matutetandil/mycel/v3/internal/connector/cache/types"
	"github.com/matutetandil/mycel/v3/internal/flow"
	"github.com/matutetandil/mycel/v3/internal/metrics"
)

// The same bytes a real Node process produced for
// gzip(base64(JSON.stringify(value))) — see internal/flow/cache_codec_test.go,
// which is where the golden is explained.
const foreignEntry = "1f8b080000000000001315c7d10a82301400d05f5203a3071f4271dc9b6d2cd3e55e37a3eb7482a2b6bf8fcedbe9036e9d8a4718666a93d1e9a48d40dcae3315c5cecec592f05db386aa1c37fde2915692845fc99c1ebb991a1284b10978a9723ccc340eba86f43eb84d3cdd0a9e2fb686145c99cb107d810eb2acf42640aa027efeef08ed5b66d90f2decc89988000000"

func newCacheHandler(t *testing.T, encoding []string) (*FlowHandler, *bytes.Buffer, func()) {
	t.Helper()

	memCache := memory.New("shared", &types.Config{Driver: "memory"})
	if err := memCache.Connect(context.Background()); err != nil {
		t.Fatalf("cache connect: %v", err)
	}

	logs := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

	h := &FlowHandler{
		Config: &flow.Config{
			Name:  "cached_read",
			Cache: &flow.CacheConfig{Storage: "shared", TTL: "5m", Encoding: encoding},
		},
		CacheConnector: memCache,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return h, logs, func() {
		slog.SetDefault(prev)
		_ = memCache.Close(context.Background())
	}
}

// counterValue reads one labelled counter straight off the registry, so the
// assertions are about the series an operator would actually see.
func counterValue(t *testing.T, vec *prometheus.CounterVec, label string) float64 {
	t.Helper()
	return testutil.ToFloat64(vec.WithLabelValues(label))
}

// The scenario from the report, end to end: an entry another service wrote is
// sitting in the namespace, and this flow declares the format it is in.
func TestCache_ReadsAnEntryWrittenByAnotherService(t *testing.T) {
	h, _, done := newCacheHandler(t, []string{"json", "base64", "gzip"})
	defer done()

	ctx := context.Background()
	data, _ := hex.DecodeString(foreignEntry)
	if err := h.CacheConnector.Set(ctx, "shared:key", data, 0); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	got, hit, err := h.checkCache(ctx, "shared:key")
	if err != nil {
		t.Fatalf("checkCache: %v", err)
	}
	if !hit {
		t.Fatal("the entry is there and the format is declared; it must be a hit")
	}
	m, ok := got.(map[string]interface{})
	if !ok || m["sku"] != "ABC-1" {
		t.Errorf("decoded %#v", got)
	}
}

// And what this flow writes goes back in the same format, so the other service
// can still read it. Before this, Mycel wrote plain JSON over the key and the
// two took turns destroying each other's entries.
func TestCache_WritesInTheDeclaredFormat(t *testing.T) {
	h, _, done := newCacheHandler(t, []string{"json", "base64", "gzip"})
	defer done()

	ctx := context.Background()
	value := map[string]interface{}{"sku": "ABC-1", "price": 29.99}
	if err := h.storeInCache(ctx, "shared:key", value); err != nil {
		t.Fatalf("storeInCache: %v", err)
	}

	stored, found, err := h.CacheConnector.Get(ctx, "shared:key")
	if err != nil || !found {
		t.Fatalf("Get: %v found=%v", err, found)
	}
	if json.Valid(stored) {
		t.Fatal("plain JSON was written over a namespace declared as gzip+base64+json")
	}
	// gzip's magic number, i.e. the format the other service expects.
	if len(stored) < 2 || stored[0] != 0x1f || stored[1] != 0x8b {
		t.Fatalf("stored bytes are not gzip: %x", stored[:min(8, len(stored))])
	}
	back, err := flow.DecodeCacheValue(stored, []string{"json", "base64", "gzip"})
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if back.(map[string]interface{})["sku"] != "ABC-1" {
		t.Errorf("round trip gave %#v", back)
	}
}

// An entry that cannot be decoded is not a hit, and does not pass in silence.
// Both halves used to be wrong: RecordCacheHit fired before the decode was
// attempted, and the error was returned to a call site that dropped it — so
// the hit rate was overstated in exactly the case where something was wrong,
// and nothing anywhere distinguished "the key was not there" from "the key was
// there and I could not read it".
func TestCache_UndecodableEntryIsNotAHitAndIsReported(t *testing.T) {
	h, logs, done := newCacheHandler(t, nil) // default JSON
	defer done()

	ctx := context.Background()
	data, _ := hex.DecodeString(foreignEntry) // gzip bytes, unreadable as JSON
	if err := h.CacheConnector.Set(ctx, "shared:key", data, 0); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	hitsBefore := counterValue(t, metrics.Default().CacheHits, "shared")
	errsBefore := counterValue(t, metrics.Default().CacheDecodeErrors, "shared")

	_, hit, err := h.checkCache(ctx, "shared:key")
	if hit {
		t.Error("an entry that could not be decoded was reported as a hit")
	}
	if err == nil {
		t.Error("the decode failure must reach the caller")
	}

	if got := counterValue(t, metrics.Default().CacheHits, "shared"); got != hitsBefore {
		t.Errorf("cache hits went from %v to %v; a request that does the work is not a hit", hitsBefore, got)
	}
	if got := counterValue(t, metrics.Default().CacheDecodeErrors, "shared"); got != errsBefore+1 {
		t.Errorf("decode errors went from %v to %v, want +1", errsBefore, got)
	}

	out := logs.String()
	if !strings.Contains(out, "could not be decoded") {
		t.Errorf("the failure was not logged:\n%s", out)
	}
	if !strings.Contains(out, "shared:key") {
		t.Errorf("the log does not name the key, which is what makes it actionable:\n%s", out)
	}
}

// A hit still counts as a hit.
func TestCache_ReadableEntryIsStillAHit(t *testing.T) {
	h, _, done := newCacheHandler(t, nil)
	defer done()

	ctx := context.Background()
	before := counterValue(t, metrics.Default().CacheHits, "shared")

	if err := h.storeInCache(ctx, "k", map[string]interface{}{"a": 1}); err != nil {
		t.Fatalf("store: %v", err)
	}
	_, hit, err := h.checkCache(ctx, "k")
	if err != nil || !hit {
		t.Fatalf("checkCache: err=%v hit=%v", err, hit)
	}
	if got := counterValue(t, metrics.Default().CacheHits, "shared"); got != before+1 {
		t.Errorf("hits went from %v to %v, want +1", before, got)
	}
}

// A named cache carries the format, so flows sharing that namespace share it
// without restating it — and a flow that declares its own still wins.
func TestCache_EncodingComesFromTheNamedCache(t *testing.T) {
	h, _, done := newCacheHandler(t, nil)
	defer done()

	h.Config.Cache.Use = "shared_ns"
	h.NamedCaches = map[string]*flow.NamedCacheConfig{
		"shared_ns": {Name: "shared_ns", Storage: "shared", Encoding: []string{"json", "base64", "gzip"}},
	}
	if got := h.cacheEncoding(); strings.Join(got, "+") != "json+base64+gzip" {
		t.Errorf("inherited encoding = %v", got)
	}

	h.Config.Cache.Encoding = []string{"json"}
	if got := h.cacheEncoding(); strings.Join(got, "+") != "json" {
		t.Errorf("a flow declaring its own must win, got %v", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
