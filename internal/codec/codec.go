// Package codec provides multi-format encoding/decoding for Mycel connectors.
// It supports JSON and XML out of the box, with a registry for extensibility.
package codec

import (
	"strings"
	"sync"
)

// Codec defines the interface for encoding and decoding data.
type Codec interface {
	// Encode serializes a value to bytes.
	Encode(v interface{}) ([]byte, error)

	// Decode deserializes bytes into a map.
	Decode(data []byte) (map[string]interface{}, error)

	// ContentType returns the MIME type for this codec (e.g., "application/json").
	ContentType() string

	// Name returns the codec identifier (e.g., "json", "xml").
	Name() string
}

var (
	mu       sync.RWMutex
	registry = map[string]Codec{}
)

func init() {
	Register("json", &JSONCodec{})
	Register("xml", &XMLCodec{RootElement: "root"})
}

// Register adds a codec to the global registry.
func Register(name string, c Codec) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = c
}

// Get returns a codec by name. Returns the JSON codec if name is empty or unknown.
func Get(name string) Codec {
	mu.RLock()
	defer mu.RUnlock()

	if name == "" {
		name = "json"
	}
	if c, ok := registry[name]; ok {
		return c
	}
	return registry["json"]
}

// DetectFromContentType returns the appropriate codec based on a Content-Type header value.
// Falls back to JSON if the content type is unrecognized.
func DetectFromContentType(ct string) Codec {
	ct = strings.ToLower(strings.TrimSpace(ct))
	// Strip parameters (e.g., "; charset=utf-8")
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}

	switch ct {
	case "application/xml", "text/xml":
		return Get("xml")
	case "application/json", "text/json":
		return Get("json")
	default:
		if strings.HasSuffix(ct, "+xml") {
			return Get("xml")
		}
		if strings.HasSuffix(ct, "+json") {
			return Get("json")
		}
		return Get("json")
	}
}
