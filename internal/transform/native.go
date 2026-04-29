package transform

import (
	"fmt"
	"reflect"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// CELValueToNative recursively converts a value produced by CEL evaluation
// into native Go types that downstream encoders (json, msgpack, SQL drivers)
// can serialize without intermediate wrappers.
//
// Without this conversion, CEL-internal maps may surface to json.Marshal as
// `map[ref.Val]ref.Val`, which it rejects as an unsupported type. Lists may
// surface as `[]ref.Val` with the same problem. The shallow `.Value()`
// conversion that CEL provides is not enough — it leaves child elements as
// ref.Val. This walker recurses through every level.
//
// Output mapping:
//   - types.Null / nil               → nil
//   - types.String                   → string
//   - types.Int / Uint / Double      → int64 / uint64 / float64
//   - types.Bool                     → bool
//   - types.Bytes                    → []byte
//   - types.Timestamp / Duration     → time.Time / time.Duration (json marshals these natively)
//   - traits.Mapper                  → map[string]interface{}
//   - traits.Lister                  → []interface{}
//
// Any other ref.Val falls back to its `.Value()` representation. Map keys
// that are not already strings are stringified via fmt.Sprint so the result
// is JSON-encodable.
func CELValueToNative(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	if rv, ok := v.(ref.Val); ok {
		return refValToNative(rv)
	}
	return goValueToNative(v)
}

func refValToNative(val ref.Val) interface{} {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case types.Null:
		return nil
	case traits.Mapper:
		return mapperToNative(v)
	case traits.Lister:
		return listerToNative(v)
	}
	// Scalars (string, int, uint, double, bool, bytes, timestamp, duration)
	// all return native Go types from .Value(). Pass through goValueToNative
	// so any nested CEL leftovers also get unwrapped (defensive).
	return goValueToNative(val.Value())
}

func mapperToNative(m traits.Mapper) map[string]interface{} {
	out := make(map[string]interface{})
	it := m.Iterator()
	for it.HasNext() == types.True {
		k := it.Next()
		out[keyToString(k)] = refValToNative(m.Get(k))
	}
	return out
}

func listerToNative(l traits.Lister) []interface{} {
	sizeVal, err := l.Size().ConvertToNative(reflect.TypeOf(int64(0)))
	if err != nil {
		return nil
	}
	size, _ := sizeVal.(int64)
	out := make([]interface{}, size)
	for i := int64(0); i < size; i++ {
		out[i] = refValToNative(l.Get(types.Int(i)))
	}
	return out
}

// goValueToNative walks a Go value and converts any ref.Val leaves and any
// non-string-keyed maps it encounters. Required because some CEL helpers
// inside this package call shallow .Value() and stash ref.Val children into
// otherwise native maps.
func goValueToNative(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	if rv, ok := v.(ref.Val); ok {
		return refValToNative(rv)
	}
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			out[k] = goValueToNative(val)
		}
		return out
	case map[ref.Val]ref.Val:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			out[keyToString(k)] = refValToNative(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = goValueToNative(val)
		}
		return out
	case []ref.Val:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = refValToNative(val)
		}
		return out
	}
	// For maps with non-string keys we cannot reliably stringify without
	// reflection. Use reflection only when we hit one — keeps the fast path
	// allocation-free.
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		out := make(map[string]interface{}, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[fmt.Sprint(iter.Key().Interface())] = goValueToNative(iter.Value().Interface())
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]interface{}, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = goValueToNative(rv.Index(i).Interface())
		}
		return out
	}
	return v
}

func keyToString(k ref.Val) string {
	if s, ok := k.(types.String); ok {
		return string(s)
	}
	if k == nil {
		return ""
	}
	return fmt.Sprint(k.Value())
}
