package flow

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Cache entry encoding.
//
// A flow's cache entries used to be json.Marshal on the way out and
// json.Unmarshal on the way in, with nothing to say otherwise. That is fine
// while Mycel owns the namespace and no one else reads it. It stops being fine
// during a migration, which is when a cache is most likely to be shared: the
// service being replaced is still up, still reading and writing the same keys,
// and it encodes them its own way.
//
// What happened then was not incompatibility but mutual destruction. Mycel read
// a key the other service wrote, could not decode it, treated that as a miss,
// did the work, and then wrote plain JSON over the key. The other service read
// it next and its own decode threw. They took turns destroying each other's
// entries, and the only visible symptom was a cache that never seemed to hit.
//
// So the encoding is declared. `encoding = ["json", "base64", "gzip"]` is
// applied left to right on the way out and reversed on the way in, which is
// how a service storing gzip(base64(JSON.stringify(v))) writes it.

// cacheCodecKind separates the one codec that turns a value into bytes from
// the ones that transform bytes into other bytes. A chain needs exactly one of
// the first, at the start.
type cacheCodecKind int

const (
	codecValue cacheCodecKind = iota
	codecBytes
)

var cacheCodecKinds = map[string]cacheCodecKind{
	"json":   codecValue,
	"base64": codecBytes,
	"gzip":   codecBytes,
}

// DefaultCacheEncoding is what a cache block that does not say gets, and is
// what every cache did before the attribute existed.
var DefaultCacheEncoding = []string{"json"}

// CacheCodecNames lists the accepted codecs, sorted, for error messages and
// for the schema.
func CacheCodecNames() []string {
	return []string{"base64", "gzip", "json"}
}

// ValidateCacheEncoding reports whether a chain can be applied at all. Checked
// when the configuration is read, so a chain that could never work is refused
// at deploy rather than on the first cache write.
func ValidateCacheEncoding(encoding []string) error {
	if len(encoding) == 0 {
		return nil
	}
	for i, name := range encoding {
		kind, ok := cacheCodecKinds[name]
		if !ok {
			return fmt.Errorf("unknown cache encoding %q: expected one of %s",
				name, strings.Join(CacheCodecNames(), ", "))
		}
		if i == 0 && kind != codecValue {
			return fmt.Errorf("cache encoding must start with a codec that turns a value into bytes (\"json\"), got %q", name)
		}
		if i > 0 && kind == codecValue {
			return fmt.Errorf("cache encoding has %q after the first position, where only byte transforms belong (%s)",
				name, "base64, gzip")
		}
	}
	return nil
}

// EncodeCacheValue applies the chain left to right.
func EncodeCacheValue(value interface{}, encoding []string) ([]byte, error) {
	if len(encoding) == 0 {
		encoding = DefaultCacheEncoding
	}

	var data []byte
	for i, name := range encoding {
		var err error
		switch name {
		case "json":
			data, err = json.Marshal(value)
		case "base64":
			data = encodeBase64(data)
		case "gzip":
			data, err = encodeGzip(data)
		default:
			err = fmt.Errorf("unknown cache encoding %q", name)
		}
		if err != nil {
			return nil, fmt.Errorf("cache encoding step %d (%s): %w", i+1, name, err)
		}
	}
	return data, nil
}

// DecodeCacheValue applies the chain in reverse.
func DecodeCacheValue(data []byte, encoding []string) (interface{}, error) {
	if len(encoding) == 0 {
		encoding = DefaultCacheEncoding
	}

	var value interface{}
	for i := len(encoding) - 1; i >= 0; i-- {
		name := encoding[i]
		var err error
		switch name {
		case "gzip":
			data, err = decodeGzip(data)
		case "base64":
			data, err = decodeBase64(data)
		case "json":
			err = json.Unmarshal(data, &value)
		default:
			err = fmt.Errorf("unknown cache encoding %q", name)
		}
		if err != nil {
			return nil, fmt.Errorf("cache decoding step %d (%s): %w", i+1, name, err)
		}
	}
	return value, nil
}

// encodeBase64 uses the standard alphabet with padding, which is what
// Buffer.toString("base64") and Python's b64encode produce.
func encodeBase64(data []byte) []byte {
	out := make([]byte, base64.StdEncoding.EncodedLen(len(data)))
	base64.StdEncoding.Encode(out, data)
	return out
}

func decodeBase64(data []byte) ([]byte, error) {
	// Tolerant on the way in and strict on the way out: a producer may or may
	// not pad, and may wrap lines. Refusing an entry over whitespace would
	// look exactly like the corruption this feature exists to avoid.
	trimmed := bytes.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', ' ', '\t':
			return -1
		}
		return r
	}, data)

	enc := base64.StdEncoding
	if bytes.ContainsAny(trimmed, "-_") {
		enc = base64.URLEncoding
	}
	if len(trimmed)%4 != 0 {
		enc = enc.WithPadding(base64.NoPadding)
	}

	out := make([]byte, enc.DecodedLen(len(trimmed)))
	n, err := enc.Decode(out, trimmed)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

func encodeGzip(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeGzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	// Bounded by what the cache already holds; a cache entry is not attacker
	// input in the way a request body is, and the reader stops at the stream's
	// own end either way.
	out, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return out, nil
}
