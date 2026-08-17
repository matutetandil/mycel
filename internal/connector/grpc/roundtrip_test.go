package grpc

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Both halves of the gRPC connector are dynamic: the server builds its services
// from .proto files at startup and the client calls them by name with no
// generated code anywhere. That means the two can be put in the same process
// and made to talk to each other over a real socket — no external service, no
// container — which is the only way to find out whether a call actually
// travels, since a mock would answer in place of the connector and never run a
// line of it.

const userProto = `
syntax = "proto3";

package testing;

service UserService {
  rpc GetUser (GetUserRequest) returns (User);
  rpc CreateUser (CreateUserRequest) returns (User);
}

message GetUserRequest {
  int32 id = 1;
}

message CreateUserRequest {
  string name = 1;
  string email = 2;
  bool active = 3;
}

message User {
  int32 id = 1;
  string name = 2;
  string email = 3;
  bool active = 4;
}
`

func protoDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "user.proto"), []byte(userProto), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return dir
}

// freePort asks the operating system for one, so tests can run beside each
// other and beside whatever else is on this machine.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

// serving starts a Mycel gRPC server on a free port with one handler per
// method, and returns the address to dial.
func serving(t *testing.T, dir string, handlers map[string]HandlerFunc) string {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	port := freePort(t)

	server := NewServerConnector("api", &ServerConfig{
		Host: "127.0.0.1", Port: port,
		ProtoPath: dir, ProtoFiles: []string{"user.proto"},
	}, logger)

	if err := server.Connect(context.Background()); err != nil {
		t.Fatalf("the server could not load its protos: %v", err)
	}
	for operation, handler := range handlers {
		server.RegisterRoute(operation, handler)
	}
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = server.Close(context.Background()) })

	address := net.JoinHostPort("127.0.0.1", itoa(port))
	waitForListener(t, address)
	return address
}

func dialing(t *testing.T, dir, address string) *ClientConnector {
	t.Helper()
	client := NewClientConnector("api", &ClientConfig{
		Target: address, Insecure: true,
		ProtoPath: dir, ProtoFiles: []string{"user.proto"},
		Timeout: 5 * time.Second, ConnectTimeout: 5 * time.Second,
	})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	return client
}

func TestACallTravelsFromTheClientToTheServerAndBack(t *testing.T) {
	dir := protoDir(t)

	var received map[string]interface{}
	var mu sync.Mutex
	address := serving(t, dir, map[string]HandlerFunc{
		"UserService/GetUser": func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			mu.Lock()
			received = input
			mu.Unlock()
			return map[string]interface{}{
				"id": 7, "name": "Ada", "email": "ada@example.com", "active": true,
			}, nil
		},
	})

	client := dialing(t, dir, address)
	answer, err := client.Call(context.Background(), "UserService/GetUser",
		map[string]interface{}{"id": 7})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	// What the handler was given, over the wire and back through protobuf.
	mu.Lock()
	got := received
	mu.Unlock()
	if got == nil {
		t.Fatal("the server never saw the call")
	}
	if id := toInt(got["id"]); id != 7 {
		t.Errorf("the server was given id = %v (%T), want 7", got["id"], got["id"])
	}

	fields, ok := answer.(map[string]interface{})
	if !ok {
		t.Fatalf("the answer came back as %T", answer)
	}
	if fields["name"] != "Ada" || fields["email"] != "ada@example.com" {
		t.Errorf("answer = %v", fields)
	}
	if fields["active"] != true {
		t.Errorf("a boolean did not survive the round trip: %v", fields["active"])
	}
}

func TestEveryFieldOfARequestArrives(t *testing.T) {
	dir := protoDir(t)

	var received map[string]interface{}
	var mu sync.Mutex
	address := serving(t, dir, map[string]HandlerFunc{
		"UserService/CreateUser": func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			mu.Lock()
			received = input
			mu.Unlock()
			return map[string]interface{}{"id": 1, "name": input["name"]}, nil
		},
	})

	client := dialing(t, dir, address)
	if _, err := client.Call(context.Background(), "UserService/CreateUser", map[string]interface{}{
		"name": "Grace", "email": "grace@example.com", "active": true,
	}); err != nil {
		t.Fatalf("Call: %v", err)
	}

	mu.Lock()
	got := received
	mu.Unlock()
	for field, want := range map[string]interface{}{
		"name": "Grace", "email": "grace@example.com", "active": true,
	} {
		if got[field] != want {
			t.Errorf("the server was given %s = %v, want %v", field, got[field], want)
		}
	}
}

func TestAFailingHandlerReachesTheCallerAsAFailure(t *testing.T) {
	// A handler that fails must not come back as an empty answer: the flow
	// calling it decides between retrying and dead-lettering on this.
	dir := protoDir(t)
	address := serving(t, dir, map[string]HandlerFunc{
		"UserService/GetUser": func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			return nil, errFromHandler
		},
	})

	client := dialing(t, dir, address)
	_, err := client.Call(context.Background(), "UserService/GetUser", map[string]interface{}{"id": 1})
	if err == nil {
		t.Fatal("a failed call came back as a successful one")
	}
	if !strings.Contains(err.Error(), "the database is down") {
		t.Errorf("error = %q, want what the handler said", err)
	}
}

func TestAMethodNobodyServesIsReported(t *testing.T) {
	dir := protoDir(t)
	address := serving(t, dir, map[string]HandlerFunc{
		"UserService/GetUser": func(ctx context.Context, input map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"id": 1}, nil
		},
	})

	client := dialing(t, dir, address)
	if _, err := client.Call(context.Background(), "UserService/DeleteUser", map[string]interface{}{}); err == nil {
		t.Error("a method the service does not have was called successfully")
	}
}

func TestAServerThatIsNotThereIsReportedWhenItIsCalled(t *testing.T) {
	// gRPC connects lazily, so the failure surfaces on the call rather than on
	// Connect — the flow has to see it as a failure either way.
	dir := protoDir(t)
	client := NewClientConnector("api", &ClientConfig{
		Target: "127.0.0.1:1", Insecure: true,
		ProtoPath: dir, ProtoFiles: []string{"user.proto"},
		Timeout: 2 * time.Second, ConnectTimeout: time.Second,
	})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })

	if _, err := client.Call(context.Background(), "UserService/GetUser", map[string]interface{}{"id": 1}); err == nil {
		t.Error("a call to nothing at all succeeded")
	}
}

func TestAProtoFileThatIsNotThereIsReportedBeforeAnythingStarts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServerConnector("api", &ServerConfig{
		Host: "127.0.0.1", Port: freePort(t),
		ProtoPath: t.TempDir(), ProtoFiles: []string{"absent.proto"},
	}, logger)

	if err := server.Connect(context.Background()); err == nil {
		t.Error("a server started with a proto file that does not exist")
	}
}

func TestHealthIsWhatTheConnectionIsDoing(t *testing.T) {
	dir := protoDir(t)
	address := serving(t, dir, map[string]HandlerFunc{})
	client := dialing(t, dir, address)

	if err := client.Health(context.Background()); err != nil {
		t.Errorf("a connected client reported itself unhealthy: %v", err)
	}
}

// waitForListener waits for the server to be accepting connections, since
// Start returns before the socket is necessarily up.
func waitForListener(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the server never started listening on %s", address)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return -1
}

var errFromHandler = &handlerError{"the database is down"}

type handlerError struct{ message string }

func (e *handlerError) Error() string { return e.message }
