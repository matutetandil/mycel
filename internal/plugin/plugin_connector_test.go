package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
)

// A plugin can be a connector: somebody else's compiled code answering reads
// and writes for a system Mycel has never heard of. It is the whole point of
// the plugin system and the thing the next version of the host API is being
// built for, and the path from a .wasm file to a flow reading rows out of it
// had no tests.
//
// The module below is hand-encoded so there is no toolchain to install. Each
// exported function answers with a fixed document, which is enough to check
// the protocol: what the host sends, what it does with what comes back, and
// what happens when the plugin says no.

// leb128 encodes a signed integer the way i32.const takes it — the encoding
// that makes 64 read as -64 if you write the byte directly.
func leb128(value int32) []byte {
	var out []byte
	for {
		b := byte(value & 0x7f)
		value >>= 7
		signBit := b & 0x40
		if (value == 0 && signBit == 0) || (value == -1 && signBit != 0) {
			return append(out, b)
		}
		out = append(out, b|0x80)
	}
}

// pluginModule builds a connector plugin whose functions answer with the given
// documents, keyed by the name they are exported under.
func pluginModule(answers map[string]string) []byte {
	names := make([]string, 0, len(answers))
	for name := range answers {
		names = append(names, name)
	}
	// Stable order, so the module is the same bytes every time it is built.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}

	var w []byte
	w = append(w, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)

	section := func(id byte, body []byte) {
		w = append(w, id)
		w = append(w, leb128(int32(len(body)))...)
		w = append(w, body...)
	}

	// alloc, then one answering function type shared by everything else.
	section(0x01, []byte{
		0x02,
		0x60, 0x01, 0x7f, 0x01, 0x7f,
		0x60, 0x02, 0x7f, 0x7f, 0x02, 0x7f, 0x7f,
	})

	functions := []byte{byte(1 + len(names)), 0x00}
	for range names {
		functions = append(functions, 0x01)
	}
	section(0x03, functions)
	section(0x05, []byte{0x01, 0x00, 0x01})

	exports := []byte{byte(2 + len(names))}
	addExport := func(name string, kind, index byte) {
		exports = append(exports, byte(len(name)))
		exports = append(exports, name...)
		exports = append(exports, kind, index)
	}
	addExport("memory", 0x02, 0x00)
	addExport("alloc", 0x00, 0x00)
	for i, name := range names {
		addExport(name, 0x00, byte(i+1))
	}
	section(0x07, exports)

	// Each answer sits at its own offset, and the allocator hands out memory
	// well past all of them.
	offsets := make(map[string]int32, len(names))
	offset := int32(0)
	for _, name := range names {
		offsets[name] = offset
		offset += int32(len(answers[name])) + 1
	}

	code := []byte{byte(1 + len(names))}
	addBody := func(body []byte) {
		entry := append([]byte{0x00}, body...)
		code = append(code, leb128(int32(len(entry)))...)
		code = append(code, entry...)
	}
	alloc := []byte{0x41}
	alloc = append(alloc, leb128(4096)...)
	addBody(append(alloc, 0x0b))
	for _, name := range names {
		body := []byte{0x41}
		body = append(body, leb128(offsets[name])...)
		body = append(body, 0x41)
		body = append(body, leb128(int32(len(answers[name])))...)
		addBody(append(body, 0x0b))
	}
	section(0x0a, code)

	data := []byte{byte(len(names))}
	for _, name := range names {
		data = append(data, 0x00, 0x41)
		data = append(data, leb128(offsets[name])...)
		data = append(data, 0x0b)
		data = append(data, leb128(int32(len(answers[name])))...)
		data = append(data, answers[name]...)
	}
	section(0x0b, data)

	return w
}

func pluginConnector(t *testing.T, answers map[string]string) *WASMConnector {
	t.Helper()
	path := filepath.Join(t.TempDir(), "connector.wasm")
	if err := os.WriteFile(path, pluginModule(answers), 0o644); err != nil {
		t.Fatalf("writing the plugin: %v", err)
	}

	conn, err := NewWASMConnector("store", "acme-store", path,
		map[string]interface{}{"base_url": "https://acme.example.com"})
	if err != nil {
		t.Fatalf("NewWASMConnector: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func TestAFlowReadsRowsOutOfAPlugin(t *testing.T) {
	conn := pluginConnector(t, map[string]string{
		"read": `{"data":[{"id":1,"name":"Ada"},{"id":2,"name":"Grace"}]}`,
	})
	ctx := context.Background()

	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	result, err := conn.Read(ctx, connector.Query{Target: "customers"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(result.Rows) != 2 || result.Rows[0]["name"] != "Ada" {
		t.Errorf("rows = %v", result.Rows)
	}
}

func TestAFlowWritesThroughAPlugin(t *testing.T) {
	conn := pluginConnector(t, map[string]string{
		"write": `{"affected":1,"last_id":"gen-1"}`,
	})
	ctx := context.Background()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	result, err := conn.Write(ctx, &connector.Data{
		Target: "customers", Operation: "INSERT",
		Payload: map[string]interface{}{"name": "Ada"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result.Affected != 1 {
		t.Errorf("affected = %d", result.Affected)
	}
}

func TestAPluginThatSaysNoFailsTheFlow(t *testing.T) {
	// A plugin reporting a problem must not come back as an empty result set,
	// which a flow would treat as "nothing matched".
	conn := pluginConnector(t, map[string]string{
		"read": `{"error":"the store is not reachable"}`,
	})
	ctx := context.Background()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	_, err := conn.Read(ctx, connector.Query{Target: "customers"})
	if err == nil {
		t.Fatal("a plugin reporting a failure produced an empty result instead")
	}
	if !strings.Contains(err.Error(), "not reachable") {
		t.Errorf("error = %q, want what the plugin said", err)
	}
}

func TestAPluginIsToldItsConfigurationWhenItStarts(t *testing.T) {
	// init is how a plugin receives what the connector block configured, and a
	// plugin that refuses that configuration must stop the connector rather
	// than run against settings it rejected.
	conn := pluginConnector(t, map[string]string{
		"init": `{"error":"base_url is not one I can reach"}`,
		"read": `{"data":[]}`,
	})

	if err := conn.Connect(context.Background()); err == nil {
		t.Fatal("a plugin that refused its configuration was connected anyway")
	}
}

func TestNothingReachesAPluginThatIsNotConnected(t *testing.T) {
	// Reading before Connect would call into a module that is not there.
	conn := pluginConnector(t, map[string]string{"read": `{"data":[]}`})

	if _, err := conn.Read(context.Background(), connector.Query{Target: "x"}); err == nil {
		t.Error("a read reached a plugin that was never connected")
	}
	if _, err := conn.Write(context.Background(), &connector.Data{Target: "x"}); err == nil {
		t.Error("a write reached a plugin that was never connected")
	}
	if _, err := conn.Call(context.Background(), "anything", nil); err == nil {
		t.Error("a call reached a plugin that was never connected")
	}
}

func TestAFunctionThePluginDoesNotOfferIsReported(t *testing.T) {
	// A plugin that only reads is a perfectly good plugin; a flow writing to
	// it has to be told, by name, rather than getting an empty result.
	conn := pluginConnector(t, map[string]string{"read": `{"data":[]}`})
	ctx := context.Background()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	_, err := conn.Write(ctx, &connector.Data{Target: "x", Operation: "INSERT"})
	if err == nil {
		t.Fatal("writing to a plugin with no write function succeeded")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("error = %q, want the function named", err)
	}
}

func TestConnectingTwiceLoadsThePluginOnce(t *testing.T) {
	conn := pluginConnector(t, map[string]string{"read": `{"data":[]}`})
	ctx := context.Background()

	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := conn.Connect(ctx); err != nil {
		t.Errorf("connecting an already connected plugin failed: %v", err)
	}
}

func TestAPluginIsUnusableOnceItIsClosed(t *testing.T) {
	conn := pluginConnector(t, map[string]string{"read": `{"data":[]}`})
	ctx := context.Background()
	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := conn.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := conn.Read(ctx, connector.Query{Target: "x"}); err == nil {
		t.Error("a closed plugin answered a read")
	}
	// And closing again is not a failure: shutdown paths overlap.
	if err := conn.Close(ctx); err != nil {
		t.Errorf("closing twice reported a failure: %v", err)
	}
}

func TestAPluginThatIsNotThereIsReportedWhenItIsConnected(t *testing.T) {
	conn, err := NewWASMConnector("store", "acme-store",
		filepath.Join(t.TempDir(), "absent.wasm"), nil)
	if err != nil {
		t.Fatalf("NewWASMConnector: %v", err)
	}
	if err := conn.Connect(context.Background()); err == nil {
		t.Error("a plugin whose file does not exist was connected")
	}
}

func TestTheConnectorAnswersForItsOwnName(t *testing.T) {
	conn := pluginConnector(t, map[string]string{"read": `{"data":[]}`})
	if conn.Name() != "store" || conn.Type() != "acme-store" {
		t.Errorf("name = %q type = %q", conn.Name(), conn.Type())
	}
}

func TestAClosedPluginGivesItsModuleBack(t *testing.T) {
	// The module is loaded into a runtime shared by the whole process, under a
	// name derived from this connector. A reload that drops the connector
	// should not leave its compiled module there for the life of the process.
	conn := pluginConnector(t, map[string]string{"read": `{"data":[]}`})
	ctx := context.Background()

	if err := conn.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	runtime, err := getWASMRuntime()
	if err != nil {
		t.Fatalf("getWASMRuntime: %v", err)
	}
	if _, loaded := runtime.GetModule("plugin_acme-store_store"); !loaded {
		t.Fatal("the module was not loaded under the name the connector uses")
	}

	if err := conn.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, loaded := runtime.GetModule("plugin_acme-store_store"); loaded {
		t.Error("the module stayed loaded after its connector was closed")
	}

	// And the connector can be brought back up.
	if err := conn.Connect(ctx); err != nil {
		t.Errorf("a closed connector could not be reconnected: %v", err)
	}
}
