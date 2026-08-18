package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

// Where an address is.
//
// Nothing here existed: the impossible_travel block declared a geoip source
// with a database file or an API and neither was ever read, which is why the
// whole detection could not have worked whatever else was written.

// Location is where a sign-in came from.
type Location struct {
	Latitude  float64
	Longitude float64
	// Label is what a person reads — a city and country when the source knows
	// them, empty when it does not.
	Label string
}

// GeoIPLookup turns an address into a place.
type GeoIPLookup interface {
	Locate(ctx context.Context, ip string) (*Location, error)
	Close() error
}

// ErrLocationUnknown is what a lookup returns for an address it cannot place —
// a private network, or one the source has no record of. It is not a failure:
// most of the addresses a service sees in a test are unplaceable.
var ErrLocationUnknown = fmt.Errorf("this address cannot be placed")

// --- A MaxMind database file ------------------------------------------------

// MMDBLookup reads a MaxMind City database from disk.
//
// The file is not shipped and cannot be: GeoLite2 is MaxMind's, downloaded
// under their licence with an account of your own. What this does is read one
// that is already there.
type MMDBLookup struct {
	reader *maxminddb.Reader
}

// NewMMDBLookup opens a MaxMind database.
func NewMMDBLookup(path string) (*MMDBLookup, error) {
	reader, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open the geoip database %q: %w", path, err)
	}
	return &MMDBLookup{reader: reader}, nil
}

func (l *MMDBLookup) Locate(ctx context.Context, ip string) (*Location, error) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nil, ErrLocationUnknown
	}

	var record struct {
		Location struct {
			Latitude  float64 `maxminddb:"latitude"`
			Longitude float64 `maxminddb:"longitude"`
		} `maxminddb:"location"`
		City struct {
			Names map[string]string `maxminddb:"names"`
		} `maxminddb:"city"`
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}

	if err := l.reader.Lookup(addr).Decode(&record); err != nil {
		return nil, ErrLocationUnknown
	}
	// A record with no coordinates is a record about a country, which is not
	// enough to measure a distance with.
	if record.Location.Latitude == 0 && record.Location.Longitude == 0 {
		return nil, ErrLocationUnknown
	}

	label := record.City.Names["en"]
	if record.Country.ISOCode != "" {
		if label != "" {
			label += ", "
		}
		label += record.Country.ISOCode
	}

	return &Location{
		Latitude:  record.Location.Latitude,
		Longitude: record.Location.Longitude,
		Label:     label,
	}, nil
}

func (l *MMDBLookup) Close() error {
	if l.reader == nil {
		return nil
	}
	return l.reader.Close()
}

// --- An HTTP service --------------------------------------------------------

// APILookup asks an HTTP service where an address is.
//
// The URL is a template with {ip} in it, so this works with whichever provider
// a deployment already pays for rather than one chosen here. The answer is read
// leniently, because every provider names the fields differently: latitude,
// lat and location.lat are all understood.
type APILookup struct {
	url    string
	client *http.Client
}

// NewAPILookup creates a lookup against an HTTP service.
func NewAPILookup(templateURL string) (*APILookup, error) {
	if !strings.Contains(templateURL, "{ip}") {
		return nil, fmt.Errorf("the geoip api url has no {ip} in it, so every lookup would ask about the same address")
	}
	return &APILookup{
		url: templateURL,
		// A sign-in waits for this, so it cannot wait long. Somebody signing in
		// should not be held up by a geolocation service having a bad day.
		client: &http.Client{Timeout: 3 * time.Second},
	}, nil
}

func (l *APILookup) Locate(ctx context.Context, ip string) (*Location, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.ReplaceAll(l.url, "{ip}", url.PathEscape(ip)), nil)
	if err != nil {
		return nil, err
	}

	response, err := l.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("the geoip service could not be reached: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the geoip service answered %d", response.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("the geoip service did not answer with JSON: %w", err)
	}

	lat, latOK := readCoordinate(body, "latitude", "lat")
	lon, lonOK := readCoordinate(body, "longitude", "lon", "lng")
	if !latOK || !lonOK {
		return nil, ErrLocationUnknown
	}

	return &Location{Latitude: lat, Longitude: lon, Label: readLabel(body)}, nil
}

func (l *APILookup) Close() error { return nil }

// readCoordinate finds a number under any of the names providers use for it,
// including one level down under "location".
func readCoordinate(body map[string]interface{}, names ...string) (float64, bool) {
	for _, name := range names {
		if value, held := body[name]; held {
			if number, ok := asFloat(value); ok {
				return number, true
			}
		}
	}
	if nested, ok := body["location"].(map[string]interface{}); ok {
		for _, name := range names {
			if value, held := nested[name]; held {
				if number, ok := asFloat(value); ok {
					return number, true
				}
			}
		}
	}
	return 0, false
}

func asFloat(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(v, "%g", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func readLabel(body map[string]interface{}) string {
	var parts []string
	for _, name := range []string{"city", "region", "country_code", "country"} {
		if value, ok := body[name].(string); ok && value != "" {
			parts = append(parts, value)
			if len(parts) == 2 {
				break
			}
		}
	}
	return strings.Join(parts, ", ")
}

// --- Caching ----------------------------------------------------------------

// cachedLookup remembers where an address was, because the same person signing
// in twice should not cost two lookups — and against a paid API it would cost
// two of something a service is billed for.
type cachedLookup struct {
	inner GeoIPLookup
	ttl   time.Duration

	mu      sync.Mutex
	entries map[string]cachedLocation
}

type cachedLocation struct {
	location *Location
	err      error
	at       time.Time
}

func newCachedLookup(inner GeoIPLookup, ttl time.Duration) *cachedLookup {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &cachedLookup{inner: inner, ttl: ttl, entries: make(map[string]cachedLocation)}
}

func (c *cachedLookup) Locate(ctx context.Context, ip string) (*Location, error) {
	c.mu.Lock()
	entry, held := c.entries[ip]
	c.mu.Unlock()
	if held && time.Since(entry.at) < c.ttl {
		return entry.location, entry.err
	}

	location, err := c.inner.Locate(ctx, ip)
	// A service that is down is not cached for an hour; an address that cannot
	// be placed is, because it will not become placeable.
	if err == nil || err == ErrLocationUnknown {
		c.mu.Lock()
		if len(c.entries) > 10000 {
			// A bound, so that a service facing the internet cannot be made to
			// hold an entry per address anybody chooses to send.
			c.entries = make(map[string]cachedLocation)
		}
		c.entries[ip] = cachedLocation{location: location, err: err, at: time.Now()}
		c.mu.Unlock()
	}
	return location, err
}

func (c *cachedLookup) Close() error { return c.inner.Close() }

// --- Distance ---------------------------------------------------------------

// kilometresBetween is the great-circle distance between two points.
//
// The straight line over the ground, which is the only distance worth
// comparing against a speed: anything somebody could actually travel is longer,
// so a speed computed this way is the slowest they could have been going, and
// calling that impossible is a claim that holds.
func kilometresBetween(a, b *Location) float64 {
	const earthRadiusKM = 6371.0

	lat1 := a.Latitude * math.Pi / 180
	lat2 := b.Latitude * math.Pi / 180
	deltaLat := (b.Latitude - a.Latitude) * math.Pi / 180
	deltaLon := (b.Longitude - a.Longitude) * math.Pi / 180

	h := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	return 2 * earthRadiusKM * math.Asin(math.Sqrt(h))
}

// isPrivateAddress reports whether an address is one there is no point looking
// up: a local network, a loopback, or something that is not an address at all.
func isPrivateAddress(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return true
	}
	return parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified()
}
