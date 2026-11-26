package debug

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// The debug server's transport after its move to coder/websocket, checked with
// a gorilla client as the peer. An IDE extension runs in a webview whose origin
// is not the admin server's host, and a request has no natural size limit;
// the new library refuses the first and caps the second at 32KB by default.
func TestDebugTransportAcceptsAnyOriginAndLargeRequests(t *testing.T) {
	_, ts := startTestServer(t)
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/debug"
	conn, resp, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": []string{"vscode-webview://abc"}})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("cross-origin handshake refused (HTTP %d): %v", status, err)
	}
	defer conn.Close()

	name := strings.Repeat("x", 100*1024)
	sendRequest(t, conn, 1, "debug.attach", &AttachParams{ClientName: name})
	if r := readResponse(t, conn); r.Error != nil {
		t.Fatalf("a 100KB attach was answered with an error: %+v", r.Error)
	}
}
