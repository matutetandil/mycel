package sse

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// Addressing one connected client.
//
// send_to_room takes the room from the destination's target, and send_to_user
// read only filters — which a `to` block does not set. So the way the
// documentation shows it, `target = "input.user_id"`, was refused every time,
// and the example demonstrating per-user delivery answered 500.

// connectAs opens a real SSE connection identified as this user and returns a
// reader over the stream.
func connectAs(t *testing.T, c *Connector, userID string) *bufio.Reader {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(c.handleSSE))
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "?user_id=" + userID)
	if err != nil {
		t.Fatalf("connecting as %s: %v", userID, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	// Wait for the connection to be registered before anything is sent to it.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.RLock()
		n := len(c.clients)
		c.mu.RUnlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return bufio.NewReader(resp.Body)
}

func readEvent(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "data:") {
				done <- strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				return
			}
		}
	}()
	select {
	case got := <-done:
		return got
	case <-time.After(3 * time.Second):
		t.Fatal("nothing arrived")
		return ""
	}
}

func TestSendToUserTakesTheTargetTheDocumentationShows(t *testing.T) {
	c := New("events", &Config{Path: "/events"}, nil)
	stream := connectAs(t, c, "42")

	_, err := c.Write(context.Background(), &connector.Data{
		Operation: "send_to_user",
		Target:    "42",
		Payload:   map[string]interface{}{"message": "Your order shipped!"},
	})
	if err != nil {
		t.Fatalf("a send addressed the way the documentation shows was refused: %v", err)
	}

	if got := readEvent(t, stream); !strings.Contains(got, "Your order shipped!") {
		t.Errorf("the client received %q", got)
	}
}

func TestSendToUserStillAcceptsFilters(t *testing.T) {
	c := New("events", &Config{Path: "/events"}, nil)
	stream := connectAs(t, c, "42")

	_, err := c.Write(context.Background(), &connector.Data{
		Operation: "send_to_user",
		Filters:   map[string]interface{}{"user_id": "42"},
		Payload:   map[string]interface{}{"message": "still works"},
	})
	if err != nil {
		t.Fatalf("filters were refused: %v", err)
	}
	if got := readEvent(t, stream); !strings.Contains(got, "still works") {
		t.Errorf("the client received %q", got)
	}
}

func TestSendToUserWithNoAddresseeSaysWhatToWrite(t *testing.T) {
	c := New("events", &Config{Path: "/events"}, nil)

	_, err := c.Write(context.Background(), &connector.Data{Operation: "send_to_user"})
	if err == nil {
		t.Fatal("a send with no addressee was accepted")
	}
	if !strings.Contains(err.Error(), "target") {
		t.Errorf("the error does not say what to write: %v", err)
	}
}
