package flow

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// nodeGzipBase64JSON was produced by a real Node process, not by the encoder
// under test:
//
//	const json = JSON.stringify(value)
//	const b64  = Buffer.from(json).toString('base64')
//	zlib.gzipSync(Buffer.from(b64))
//
// Frozen here so the interop this feature exists for is checked on every run,
// on a machine with no Node on it. Comparing our gzip bytes to theirs would
// prove nothing — compressors differ — so what is asserted is that we can read
// what they wrote.
const nodeGzipBase64JSON = "1f8b080000000000001315c7d10a82301400d05f5203a3071f4271dc9b6d2cd3e55e37a3eb7482a2b6bf8fcedbe9036e9d8a4718666a93d1e9a48d40dcae3315c5cecec592f05db386aa1c37fde2915692845fc99c1ebb991a1284b10978a9723ccc340eba86f43eb84d3cdd0a9e2fb686145c99cb107d810eb2acf42640aa027efeef08ed5b66d90f2decc89988000000"

func interopValue() interface{} {
	var v interface{}
	_ = json.Unmarshal([]byte(`{"name":"Widget • ünïcode","nested":{"store":"us"},"price":29.99,"sku":"ABC-1","tags":["a","b"]}`), &v)
	return v
}

// The case from the report: a NestJS service storing
// gzip(base64(JSON.stringify(value))) in the same Redis namespace.
func TestDecodeCacheValue_ReadsWhatAnotherServiceWrote(t *testing.T) {
	data, err := hex.DecodeString(nodeGzipBase64JSON)
	if err != nil {
		t.Fatalf("golden: %v", err)
	}

	got, err := DecodeCacheValue(data, []string{"json", "base64", "gzip"})
	if err != nil {
		t.Fatalf("decoding what the other service wrote: %v", err)
	}
	if want := interopValue(); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
}

// And the same bytes are unreadable under the default encoding — which is what
// made this corruption rather than incompatibility: the decode failed, the
// failure was read as a miss, and plain JSON was written over the key.
func TestDecodeCacheValue_DefaultEncodingCannotReadThem(t *testing.T) {
	data, _ := hex.DecodeString(nodeGzipBase64JSON)
	if _, err := DecodeCacheValue(data, nil); err == nil {
		t.Fatal("the default encoding must not silently accept gzip bytes")
	}
}

func TestCacheValue_RoundTrip(t *testing.T) {
	value := interopValue()
	for _, chain := range [][]string{
		nil,
		{"json"},
		{"json", "base64"},
		{"json", "gzip"},
		{"json", "base64", "gzip"},
		{"json", "gzip", "base64"},
	} {
		name := "default"
		if chain != nil {
			name = strings.Join(chain, "+")
		}
		t.Run(name, func(t *testing.T) {
			data, err := EncodeCacheValue(value, chain)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			got, err := DecodeCacheValue(data, chain)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !reflect.DeepEqual(got, value) {
				t.Errorf("got %#v, want %#v", got, value)
			}
		})
	}
}

// An absent encoding has to stay byte-for-byte what every cache did before the
// attribute existed, or upgrading rewrites everyone's entries.
func TestCacheValue_DefaultIsPlainJSON(t *testing.T) {
	value := map[string]interface{}{"sku": "ABC-1", "n": 2}
	want, _ := json.Marshal(value)

	for _, chain := range [][]string{nil, {}, {"json"}} {
		got, err := EncodeCacheValue(value, chain)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if string(got) != string(want) {
			t.Errorf("chain %v produced %s, want %s", chain, got, want)
		}
	}
}

func TestValidateCacheEncoding(t *testing.T) {
	for _, tc := range []struct {
		name    string
		chain   []string
		wantErr string
	}{
		{"absent", nil, ""},
		{"json", []string{"json"}, ""},
		{"the reported shape", []string{"json", "base64", "gzip"}, ""},
		{"unknown codec", []string{"json", "snappy"}, "unknown cache encoding"},
		{"does not start with a value codec", []string{"gzip", "json"}, "must start with"},
		{"two value codecs", []string{"json", "json"}, "after the first position"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCacheEncoding(tc.chain)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// Base64 on the way in is tolerant of what real producers emit — padding or
// not, wrapped or not. Refusing an entry over a newline would look exactly
// like the corruption this feature exists to avoid.
func TestDecodeCacheValue_TolerantBase64(t *testing.T) {
	for _, tc := range []struct{ name, b64 string }{
		{"padded", "eyJhIjoxfQ=="},
		{"unpadded", "eyJhIjoxfQ"},
		{"wrapped", "eyJhIjox\nfQ=="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeCacheValue([]byte(tc.b64), []string{"json", "base64"})
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			m, ok := got.(map[string]interface{})
			if !ok || m["a"] != float64(1) {
				t.Errorf("got %#v", got)
			}
		})
	}
}

// The live check, when there is a Node to run it against: the other service
// has to be able to read what Mycel writes, which the frozen golden above
// cannot prove on its own.
func TestCacheEncoding_NodeReadsWhatMycelWrites(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	script := filepath.Join(t.TempDir(), "decode.js")
	src := `const zlib = require('zlib');
const gz = Buffer.from(process.argv[2], 'hex');
const b64 = zlib.gunzipSync(gz).toString();
process.stdout.write(Buffer.from(b64, 'base64').toString());`
	if err := os.WriteFile(script, []byte(src), 0o600); err != nil {
		t.Fatalf("writing script: %v", err)
	}

	value := interopValue()
	encoded, err := EncodeCacheValue(value, []string{"json", "base64", "gzip"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	out, err := exec.Command("node", script, hex.EncodeToString(encoded)).Output()
	if err != nil {
		t.Fatalf("node could not read what Mycel wrote: %v", err)
	}
	var got interface{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("node produced %q: %v", out, err)
	}
	if !reflect.DeepEqual(got, value) {
		t.Errorf("node read %#v, want %#v", got, value)
	}
}
