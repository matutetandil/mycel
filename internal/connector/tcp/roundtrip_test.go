package tcp

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// A TCP server and a TCP client, talking to each other.
//
// Both sides are in this package and neither had been asked to do its job
// here: Connect, Health, Read and Write on the client were at zero, and the
// server's accept path was only reached by the integration suite, which runs
// the binary. The two halves are the same protocol read from opposite ends —
// a framing mistake shows up as a hang, and a hang is what a test is for.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func serverAndClient(t *testing.T, protocol string) (*ServerConnector, *ClientConnector) {
	t.Helper()

	port := freePort(t)
	server, err := NewServer("api", "127.0.0.1", port, protocol)
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	if err := server.Connect(context.Background()); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(func() { _ = server.Close(context.Background()) })

	// The listener is up before the client dials, but the accept loop is a
	// goroutine.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	client, err := NewClient("upstream", "127.0.0.1", port, protocol)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })

	return server, client
}

func TestARequestGoesOverTheWireAndTheAnswerComesBack(t *testing.T) {
	server, client := serverAndClient(t, "json")
	ctx := context.Background()

	if err := server.Health(ctx); err != nil {
		t.Errorf("server health: %v", err)
	}
	if err := client.Health(ctx); err != nil {
		t.Errorf("client health: %v", err)
	}

	server.RegisterRoute("get_user", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{
			"id":    input["id"],
			"name":  "Ada",
			"asked": true,
		}, nil
	})

	answer, err := client.Write(ctx, &connector.Data{
		Target:  "get_user",
		Payload: map[string]interface{}{"id": "u-1"},
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(answer.Rows) != 1 {
		t.Fatalf("%d rows came back", len(answer.Rows))
	}

	row := answer.Rows[0]
	if row["name"] != "Ada" {
		t.Errorf("name = %#v", row["name"])
	}
	// What was sent reached the other side unchanged.
	if row["id"] != "u-1" {
		t.Errorf("the id the handler saw came back as %#v", row["id"])
	}
}

// A read is the same round trip, asked the other way.
func TestAReadIsTheSameRoundTrip(t *testing.T) {
	server, client := serverAndClient(t, "json")
	ctx := context.Background()

	server.RegisterRoute("list_users", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return []interface{}{
			map[string]interface{}{"id": 1},
			map[string]interface{}{"id": 2},
		}, nil
	})

	result, err := client.Read(ctx, connector.Query{Target: "list_users"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(result.Rows) == 0 {
		t.Fatal("nothing came back from a read")
	}
}

// An operation the server does not serve is answered, not left hanging.
func TestAnOperationNobodyServesIsAnswered(t *testing.T) {
	_, client := serverAndClient(t, "json")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := client.Write(ctx, &connector.Data{
		Target:  "nothing_here",
		Payload: map[string]interface{}{},
	})
	if err == nil {
		t.Error("an operation nothing serves was reported as a success")
	}
	if ctx.Err() != nil {
		t.Error("the client waited for the deadline rather than being told")
	}
}

// A closed client refuses rather than taking the process down.
//
// Close drains the pool and closes its channel, and a receive on a closed
// channel hands back a nil connection immediately — which was then used. So
// anything reaching this connector after it had been closed panicked with a
// nil dereference, and a panic takes the whole service with it. The moment
// that happens is a flow still in flight during a hot reload or a shutdown.
func TestAClosedClientRefusesRatherThanPanicking(t *testing.T) {
	server, client := serverAndClient(t, "json")
	ctx := context.Background()

	server.RegisterRoute("ping", func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	})

	// Warm the pool, so there is a connection in it when it is closed.
	if _, err := client.Read(ctx, connector.Query{Target: "ping"}); err != nil {
		t.Fatalf("read: %v", err)
	}

	if err := client.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Closing twice is what shutdown does when something already has.
	if err := client.Close(ctx); err != nil {
		t.Fatalf("second close: %v", err)
	}

	if err := client.Health(ctx); err == nil {
		t.Error("a closed client reported itself healthy")
	}

	_, err := client.Read(ctx, connector.Query{Target: "ping"})
	if err == nil {
		t.Fatal("a closed client answered a read")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("the refusal reads %q; it should say the client is closed", err)
	}

	if _, err := client.Write(ctx, &connector.Data{
		Target: "ping", Payload: map[string]interface{}{},
	}); err == nil {
		t.Error("a closed client accepted a write")
	}
}
