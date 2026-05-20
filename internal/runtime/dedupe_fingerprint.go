package runtime

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
)

// Type tags for the fingerprint encoding. Each tag claims a region of the
// stream so two values of different shapes can never produce equal bytes —
// e.g. the string "a,b" cannot collide with the array ["a","b"], and the
// integer 5 cannot collide with the string "5".
const (
	fpTagNil    byte = 'n'
	fpTagBool   byte = 'b'
	fpTagInt    byte = 'i'
	fpTagFloat  byte = 'f'
	fpTagString byte = 's'
	fpTagBytes  byte = 'B'
	fpTagMap    byte = 'm'
	fpTagArray  byte = 'a'
)

// Fingerprint encodes a projection into a deterministic byte string suitable
// for byte-for-byte equality comparison against a previously stored
// fingerprint. The encoding is length-prefixed and type-tagged so different
// shapes can never produce equal bytes.
//
// Determinism rules:
//
//   - Map keys are sorted alphabetically before serialization. Two maps with
//     the same content but different insertion order produce identical bytes.
//
//   - Array elements are sorted by their own serialized representation before
//     serialization. The encoder treats arrays as order-insensitive sets,
//     appropriate for the common "list of attribute values" projection but
//     lossy if order is semantically meaningful. Callers needing
//     order-preserving fingerprints should reshape arrays in transform
//     (e.g. join with a delimiter into a string).
//
//   - Numeric values that are whole numbers are encoded as integers
//     regardless of their input form (float64(5.0) and int64(5) produce
//     identical bytes). CEL evaluates all numeric literals to float64, so
//     this normalization is what makes "input.qty == output.qty" round-trip
//     cleanly through the projection.
//
// Returns the encoded byte string. An error is returned only for unsupported
// types in the projection — the function is otherwise total.
//
// Future optimization: hash the result with SHA-256 to bound storage size
// per key. v1 keeps the full bytes so that any divergence is auditable and
// there is zero risk of hash collision.
func Fingerprint(projection map[string]interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeValue(&buf, projection); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeValue writes a single value with its type tag.
func encodeValue(buf *bytes.Buffer, v interface{}) error {
	if v == nil {
		buf.WriteByte(fpTagNil)
		return nil
	}
	switch x := v.(type) {
	case bool:
		buf.WriteByte(fpTagBool)
		if x {
			buf.WriteByte(1)
		} else {
			buf.WriteByte(0)
		}
		return nil
	case int:
		return encodeInt(buf, int64(x))
	case int32:
		return encodeInt(buf, int64(x))
	case int64:
		return encodeInt(buf, x)
	case uint:
		return encodeInt(buf, int64(x))
	case uint32:
		return encodeInt(buf, int64(x))
	case uint64:
		return encodeInt(buf, int64(x))
	case float32:
		return encodeFloat(buf, float64(x))
	case float64:
		// Whole-number floats round-trip as integers so that CEL's float64
		// representation of integer literals matches Go int64 sources.
		if x == float64(int64(x)) {
			return encodeInt(buf, int64(x))
		}
		return encodeFloat(buf, x)
	case string:
		return encodeString(buf, x)
	case []byte:
		buf.WriteByte(fpTagBytes)
		writeLen(buf, len(x))
		buf.Write(x)
		return nil
	case map[string]interface{}:
		return encodeMap(buf, x)
	case []interface{}:
		return encodeArray(buf, x)
	default:
		return fmt.Errorf("fingerprint: unsupported type %T (projection values must be primitives, []byte, map[string]interface{}, or []interface{})", v)
	}
}

func encodeInt(buf *bytes.Buffer, n int64) error {
	buf.WriteByte(fpTagInt)
	s := strconv.FormatInt(n, 10)
	writeLen(buf, len(s))
	buf.WriteString(s)
	return nil
}

func encodeFloat(buf *bytes.Buffer, f float64) error {
	buf.WriteByte(fpTagFloat)
	// 'g' with precision -1 produces the shortest representation that
	// round-trips. Same float64 input → same bytes across machines and Go
	// versions because the format is fully deterministic.
	s := strconv.FormatFloat(f, 'g', -1, 64)
	writeLen(buf, len(s))
	buf.WriteString(s)
	return nil
}

func encodeString(buf *bytes.Buffer, s string) error {
	buf.WriteByte(fpTagString)
	writeLen(buf, len(s))
	buf.WriteString(s)
	return nil
}

func encodeMap(buf *bytes.Buffer, m map[string]interface{}) error {
	buf.WriteByte(fpTagMap)
	writeLen(buf, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		// Map keys are length-prefixed strings without a leading tag since
		// they are always strings — the type-tag is implied by position.
		writeLen(buf, len(k))
		buf.WriteString(k)
		if err := encodeValue(buf, m[k]); err != nil {
			return fmt.Errorf("fingerprint: encoding key %q: %w", k, err)
		}
	}
	return nil
}

func encodeArray(buf *bytes.Buffer, a []interface{}) error {
	buf.WriteByte(fpTagArray)
	writeLen(buf, len(a))
	// Encode each element into its own buffer, then sort by the encoded
	// bytes. This makes the result independent of input order.
	encoded := make([][]byte, len(a))
	for i, item := range a {
		var sub bytes.Buffer
		if err := encodeValue(&sub, item); err != nil {
			return fmt.Errorf("fingerprint: encoding array element %d: %w", i, err)
		}
		encoded[i] = sub.Bytes()
	}
	sort.Slice(encoded, func(i, j int) bool {
		return bytes.Compare(encoded[i], encoded[j]) < 0
	})
	for _, e := range encoded {
		buf.Write(e)
	}
	return nil
}

// writeLen writes a fixed-width little-endian 4-byte length prefix. Bounded
// to 2^32 bytes per value, which is more than enough for any realistic
// projection.
func writeLen(buf *bytes.Buffer, n int) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(n))
	buf.Write(b[:])
}
