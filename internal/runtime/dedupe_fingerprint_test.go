package runtime

import (
	"bytes"
	"testing"
)

// fpEqual asserts two Fingerprint calls produce identical bytes.
func fpEqual(t *testing.T, name string, a, b map[string]interface{}) {
	t.Helper()
	fa, err := Fingerprint(a)
	if err != nil {
		t.Fatalf("[%s] encode a: %v", name, err)
	}
	fb, err := Fingerprint(b)
	if err != nil {
		t.Fatalf("[%s] encode b: %v", name, err)
	}
	if !bytes.Equal(fa, fb) {
		t.Errorf("[%s] expected equal fingerprints\n  a=%x\n  b=%x", name, fa, fb)
	}
}

// fpDiffer asserts two Fingerprint calls produce different bytes.
func fpDiffer(t *testing.T, name string, a, b map[string]interface{}) {
	t.Helper()
	fa, err := Fingerprint(a)
	if err != nil {
		t.Fatalf("[%s] encode a: %v", name, err)
	}
	fb, err := Fingerprint(b)
	if err != nil {
		t.Fatalf("[%s] encode b: %v", name, err)
	}
	if bytes.Equal(fa, fb) {
		t.Errorf("[%s] expected DIFFERENT fingerprints but both produced %x", name, fa)
	}
}

func TestFingerprint_Deterministic(t *testing.T) {
	// Same input twice → identical bytes. Encoding is a pure function with
	// no randomness, no map-iteration-order dependence, no time, no IDs.
	p := map[string]interface{}{
		"name":  "Widget",
		"price": 42,
		"tags":  []interface{}{"a", "b", "c"},
	}
	for i := 0; i < 5; i++ {
		fpEqual(t, "deterministic-run", p, p)
	}
}

func TestFingerprint_MapKeyOrderIndependent(t *testing.T) {
	// Go map iteration is randomized; the encoder must sort keys so the
	// output is independent of insertion order.
	a := map[string]interface{}{"name": "A", "qty": 1, "color": "red"}
	b := map[string]interface{}{"color": "red", "qty": 1, "name": "A"}
	fpEqual(t, "map-key-order", a, b)
}

func TestFingerprint_ArrayOrderIndependent(t *testing.T) {
	// Per the spec, arrays are sorted by their encoded representation
	// before serialization. Two projections with the same set of array
	// elements in different order produce identical bytes.
	a := map[string]interface{}{
		"tags": []interface{}{"fr", "uk", "us"},
	}
	b := map[string]interface{}{
		"tags": []interface{}{"us", "fr", "uk"},
	}
	fpEqual(t, "array-order", a, b)
}

func TestFingerprint_TypeTagPreventsCollision(t *testing.T) {
	// The whole point of type-tagging + length-prefixing: structurally
	// different shapes that could otherwise serialize to the same bytes
	// must produce DIFFERENT fingerprints.

	// "a,b" as a string vs ["a","b"] as an array
	fpDiffer(t, "string-vs-array", map[string]interface{}{
		"v": "a,b",
	}, map[string]interface{}{
		"v": []interface{}{"a", "b"},
	})

	// "5" as a string vs 5 as a number
	fpDiffer(t, "string-vs-int", map[string]interface{}{
		"v": "5",
	}, map[string]interface{}{
		"v": 5,
	})

	// "ab" as a single string vs "a"+"b" as two-key map
	fpDiffer(t, "concat-vs-split", map[string]interface{}{
		"v": "ab",
	}, map[string]interface{}{
		"a": "",
		"b": "",
	})

	// nil vs empty string
	fpDiffer(t, "nil-vs-empty", map[string]interface{}{
		"v": nil,
	}, map[string]interface{}{
		"v": "",
	})

	// nil vs missing field — the missing field changes the map count, so
	// they must differ.
	fpDiffer(t, "nil-vs-missing", map[string]interface{}{
		"v": nil,
	}, map[string]interface{}{})
}

func TestFingerprint_NestedMapsAndArrays(t *testing.T) {
	// Real Mercury shape: top-level map with nested maps (per-storeview
	// price map) and arrays (extra_images, dynamic_attributes). The
	// encoder recurses, and key ordering at every level is normalized.
	a := map[string]interface{}{
		"name": "Widget",
		"prices": map[string]interface{}{
			"us": map[string]interface{}{"ListPrice": "10", "TradePrice": "9"},
			"uk": map[string]interface{}{"ListPrice": "12", "TradePrice": "11"},
		},
		"extra_images": []interface{}{"a.jpg", "b.jpg", "c.jpg"},
	}
	b := map[string]interface{}{
		"extra_images": []interface{}{"c.jpg", "a.jpg", "b.jpg"},
		"prices": map[string]interface{}{
			"uk": map[string]interface{}{"TradePrice": "11", "ListPrice": "12"},
			"us": map[string]interface{}{"TradePrice": "9", "ListPrice": "10"},
		},
		"name": "Widget",
	}
	fpEqual(t, "nested-shuffled", a, b)
}

func TestFingerprint_NumberNormalization(t *testing.T) {
	// CEL evaluates numeric literals to float64 even when the value is a
	// whole number. The encoder coerces whole-number floats to int so that
	// e.g. input.qty (an int from JSON) and a CEL projection that arrives
	// as float64 still match.
	fpEqual(t, "int-vs-whole-float",
		map[string]interface{}{"qty": 5},
		map[string]interface{}{"qty": float64(5.0)},
	)

	// Non-integer floats are NOT collapsed — different values must
	// produce different bytes.
	fpDiffer(t, "different-floats",
		map[string]interface{}{"v": 1.5},
		map[string]interface{}{"v": 1.6},
	)

	// 0 across int and float forms collapses.
	fpEqual(t, "zero-forms",
		map[string]interface{}{"v": 0},
		map[string]interface{}{"v": float64(0)},
	)
}

func TestFingerprint_StringLengthPrefix(t *testing.T) {
	// "a"+"b" projection vs "ab" projection — without length prefixing on
	// strings, the concatenated bytes could collide. The prefix prevents
	// this regardless of how the encoder positions adjacent strings.
	fpDiffer(t, "string-boundary",
		map[string]interface{}{"x": "a", "y": "b"},
		map[string]interface{}{"x": "ab", "y": ""},
	)
}

func TestFingerprint_BoolsAndNulls(t *testing.T) {
	// true ≠ false
	fpDiffer(t, "true-vs-false",
		map[string]interface{}{"v": true},
		map[string]interface{}{"v": false},
	)
	// true ≠ "true" (type tag protects)
	fpDiffer(t, "bool-vs-string",
		map[string]interface{}{"v": true},
		map[string]interface{}{"v": "true"},
	)
	// nil ≠ false
	fpDiffer(t, "nil-vs-false",
		map[string]interface{}{"v": nil},
		map[string]interface{}{"v": false},
	)
}

func TestFingerprint_EmptyMapDeterministic(t *testing.T) {
	// Empty projection is valid (degenerate dedupe — all messages with
	// the same key are duplicates). Must be deterministic.
	a := map[string]interface{}{}
	b := map[string]interface{}{}
	fpEqual(t, "empty-empty", a, b)

	// And it must differ from a non-empty projection.
	fpDiffer(t, "empty-vs-one",
		map[string]interface{}{},
		map[string]interface{}{"v": nil},
	)
}

func TestFingerprint_UnsupportedTypeReturnsError(t *testing.T) {
	// Types we haven't taught the encoder must error explicitly rather
	// than silently producing garbage that could equal something else.
	type customStruct struct{ X int }
	_, err := Fingerprint(map[string]interface{}{
		"v": customStruct{X: 1},
	})
	if err == nil {
		t.Fatal("expected error for unsupported struct type")
	}
}

func TestFingerprint_RealWorldShape(t *testing.T) {
	// Approximation of the Mercury "products" projection: text fields,
	// nested price map per-storeview, arrays of images and attributes.
	// First snapshot is the baseline; second has IDENTICAL content with
	// shuffled keys at every level → equal fingerprints.
	baseline := map[string]interface{}{
		"sku":        "HLS213",
		"name":       "Widget",
		"parent_sku": "HLS",
		"prices": map[string]interface{}{
			"us":    map[string]interface{}{"ListPrice": "10.00", "TradePrice": "9.00"},
			"uk":    map[string]interface{}{"ListPrice": "12.00", "TradePrice": "11.00"},
			"fr_en": map[string]interface{}{"ListPrice": "11.00", "TradePrice": "10.00"},
		},
		"websites":     map[string]interface{}{"us": true, "uk": true, "fr": false},
		"extra_images": []interface{}{"a.jpg", "b.jpg", "c.jpg"},
		"dynamic_attributes": []interface{}{
			map[string]interface{}{"key": "color", "value": "red"},
			map[string]interface{}{"key": "size", "value": "M"},
		},
	}
	shuffled := map[string]interface{}{
		"dynamic_attributes": []interface{}{
			map[string]interface{}{"value": "M", "key": "size"},
			map[string]interface{}{"value": "red", "key": "color"},
		},
		"extra_images": []interface{}{"c.jpg", "a.jpg", "b.jpg"},
		"websites":     map[string]interface{}{"fr": false, "uk": true, "us": true},
		"prices": map[string]interface{}{
			"fr_en": map[string]interface{}{"TradePrice": "10.00", "ListPrice": "11.00"},
			"uk":    map[string]interface{}{"TradePrice": "11.00", "ListPrice": "12.00"},
			"us":    map[string]interface{}{"TradePrice": "9.00", "ListPrice": "10.00"},
		},
		"parent_sku": "HLS",
		"name":       "Widget",
		"sku":        "HLS213",
	}
	fpEqual(t, "real-world-shuffled", baseline, shuffled)

	// Now flip ONE field — a single price changed — and confirm the
	// fingerprint actually changes (no false negatives on real edits).
	changed := map[string]interface{}{
		"sku":        "HLS213",
		"name":       "Widget",
		"parent_sku": "HLS",
		"prices": map[string]interface{}{
			"us":    map[string]interface{}{"ListPrice": "10.50", "TradePrice": "9.00"}, // 10.00 → 10.50
			"uk":    map[string]interface{}{"ListPrice": "12.00", "TradePrice": "11.00"},
			"fr_en": map[string]interface{}{"ListPrice": "11.00", "TradePrice": "10.00"},
		},
		"websites":     map[string]interface{}{"us": true, "uk": true, "fr": false},
		"extra_images": []interface{}{"a.jpg", "b.jpg", "c.jpg"},
		"dynamic_attributes": []interface{}{
			map[string]interface{}{"key": "color", "value": "red"},
			map[string]interface{}{"key": "size", "value": "M"},
		},
	}
	fpDiffer(t, "real-world-changed-price", baseline, changed)
}
