package flow

import (
	"reflect"
	"testing"
)

// A number read back from the cache is the number that was stored. Decoding
// JSON into an interface{} made every number a float64, so an integer past
// 2^53 — a snowflake id, a 64-bit hash, cents in a large ledger — came back
// with its low digits rounded away. Seen live: a flow answered
// 4244637855813302974 and the cached copy of the same entry answered
// 4244637855813303000.
func TestACachedIntegerKeepsItsDigits(t *testing.T) {
	stored := map[string]interface{}{
		"r":     int64(4244637855813302974),
		"price": 10.5,
		"n":     3,
		"neg":   int64(-9007199254740993),
		"big":   uint64(18446744073709551615),
		"rows": []interface{}{
			map[string]interface{}{"id": int64(9007199254740993), "ratio": 0.25},
		},
	}
	data, err := EncodeCacheValue(stored, nil)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCacheValue(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.(map[string]interface{})

	if got["r"] != int64(4244637855813302974) {
		t.Errorf("r came back as %#v", got["r"])
	}
	if got["neg"] != int64(-9007199254740993) {
		t.Errorf("neg came back as %#v", got["neg"])
	}
	// Past int64 there is no exact integer type in the value tree; the
	// float is the honest answer, not a wrapped-around int.
	if got["big"] != float64(18446744073709551615) {
		t.Errorf("big came back as %#v", got["big"])
	}
	if got["price"] != 10.5 || got["rows"].([]interface{})[0].(map[string]interface{})["ratio"] != 0.25 {
		t.Errorf("a fraction was altered: %#v", got)
	}
	if got["n"] != int64(3) {
		t.Errorf("a small integer came back as %#v", got["n"])
	}
	if row := got["rows"].([]interface{})[0].(map[string]interface{}); row["id"] != int64(9007199254740993) {
		t.Errorf("a nested integer came back as %#v", row["id"])
	}

	// The wire format has not moved: an entry written before this fix, or by
	// a service that is not Mycel, reads the same way.
	legacy, err := DecodeCacheValue([]byte(`{"id":9007199254740993,"price":10.5,"tags":["a"]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"id": int64(9007199254740993), "price": 10.5, "tags": []interface{}{"a"}}
	if !reflect.DeepEqual(legacy, want) {
		t.Errorf("legacy entry = %#v", legacy)
	}
}
