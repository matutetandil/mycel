package runtime

import (
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/flow"
)

// A cache TTL written in days.
//
// getCacheTTL read it with the standard library's parser, which has no day
// unit, and discarded the error — so `ttl = "30d"` meant no TTL at all and the
// entry lived for however long the connector defaults to. The examples in this
// repository write TTLs in days.
func TestACacheTTLInDaysIsHonoured(t *testing.T) {
	for name, tc := range map[string]struct {
		ttl  string
		want time.Duration
	}{
		"days":    {"30d", 30 * 24 * time.Hour},
		"weeks":   {"2w", 14 * 24 * time.Hour},
		"hours":   {"24h", 24 * time.Hour},
		"minutes": {"5m", 5 * time.Minute},
	} {
		t.Run(name, func(t *testing.T) {
			h := &FlowHandler{Config: &flow.Config{
				Name:  "cached",
				Cache: &flow.CacheConfig{Storage: "memcache", TTL: tc.ttl},
			}}
			if got := h.getCacheTTL(); got != tc.want {
				t.Errorf("ttl %q -> %v, want %v", tc.ttl, got, tc.want)
			}
		})
	}
}
