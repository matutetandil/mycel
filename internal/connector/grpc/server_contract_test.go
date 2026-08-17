package grpc

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// The parts of the connector the runtime talks to rather than a caller: what it
// calls itself, whether it is answering, what a flow must say to bind to it, and
// what happens to a streaming call when authentication is turned on.

func TestTheConnectorNamesItself(t *testing.T) {
	// The runtime looks connectors up by name and reports them by type, so an
	// empty one shows up as a flow bound to nothing in particular.
	server := NewServerConnector("orders_grpc", &ServerConfig{Port: 50051}, nil)
	if server.Name() != "orders_grpc" {
		t.Errorf("name = %q", server.Name())
	}
	if server.Type() != "grpc" {
		t.Errorf("type = %q", server.Type())
	}
	// A listening connector: the runtime must not try to write to it.
	if !server.InboundOnly() {
		t.Error("a gRPC server was offered as somewhere to write to")
	}

	client := NewClientConnector("orders_client", &ClientConfig{Target: "svc:50051"})
	if client.Name() != "orders_client" {
		t.Errorf("name = %q", client.Name())
	}
	if client.Type() != "grpc" {
		t.Errorf("type = %q", client.Type())
	}
}

func TestAServerThatHasNotStartedIsNotHealthy(t *testing.T) {
	// /health is what a load balancer reads to decide whether to send traffic
	// here, so answering before the port is open sends it into nothing.
	server := NewServerConnector("orders", &ServerConfig{Port: 50051}, nil)
	if err := server.Health(context.Background()); err == nil {
		t.Error("a server that has not started reported itself healthy")
	}
}

func TestAFlowMustSayWhichMethodItAnswers(t *testing.T) {
	// A gRPC server dispatches by method name; a flow that names none registers
	// a handler for the empty operation and is never called.
	server := NewServerConnector("orders", &ServerConfig{Port: 50051}, nil)

	err := server.ValidateSourceParams(map[string]interface{}{})
	if err == nil {
		t.Fatal("a flow naming no method was accepted")
	}
	if !strings.Contains(err.Error(), "operation") {
		t.Errorf("error = %q, want it to name what is missing", err)
	}

	if err := server.ValidateSourceParams(map[string]interface{}{"operation": "GetUser"}); err != nil {
		t.Errorf("a flow naming its method was refused: %v", err)
	}
}

// --- Streaming calls --------------------------------------------------------

// fakeStream is the server side of a streaming call, carrying only the context
// the interceptor cares about.
type fakeStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *fakeStream) Context() context.Context { return s.ctx }

func TestAStreamingCallIsAuthenticatedToo(t *testing.T) {
	// Authentication that covered only unary calls would leave every streaming
	// method open — and a subscription is exactly the sort of method somebody
	// would want to protect.
	interceptor := NewAuthInterceptor(&AuthConfig{
		Type:   "api_key",
		APIKey: &APIKeyConfig{Keys: []string{"k-123"}, Metadata: "api-key"},
	})
	handle := interceptor.StreamInterceptor()

	info := &grpc.StreamServerInfo{FullMethod: "/orders.Orders/Watch"}

	// Without a key.
	err := handle(nil, &fakeStream{ctx: context.Background()}, info,
		func(interface{}, grpc.ServerStream) error {
			t.Error("the stream was opened for a caller with no credentials")
			return nil
		})
	if err == nil {
		t.Error("an unauthenticated stream was accepted")
	}

	// With one — and the handler must see the identity, which means the stream
	// it is handed carries the authenticated context rather than the original.
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("api-key", "k-123"))
	var sawIdentity bool
	err = handle(nil, &fakeStream{ctx: ctx}, info, func(_ interface{}, stream grpc.ServerStream) error {
		sawIdentity = GetAuthContext(stream.Context()) != nil
		return nil
	})
	if err != nil {
		t.Fatalf("an authenticated stream was refused: %v", err)
	}
	if !sawIdentity {
		t.Error("the handler was given the original context, so it cannot tell who is calling")
	}
}

func TestAPublicMethodIsOpenOnBothKindsOfCall(t *testing.T) {
	// Health and reflection are the usual ones: a probe cannot carry a token,
	// and a service whose health check is authenticated looks down.
	interceptor := NewAuthInterceptor(&AuthConfig{
		Type:   "api_key",
		APIKey: &APIKeyConfig{Keys: []string{"k-123"}},
		Public: []string{"/grpc.health.v1.Health/Check", "/orders.Public/*"},
	})

	var reached bool
	_, err := interceptor.UnaryInterceptor()(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"},
		func(context.Context, interface{}) (interface{}, error) {
			reached = true
			return nil, nil
		})
	if err != nil || !reached {
		t.Errorf("a health check was turned away: err = %v", err)
	}

	// And the wildcard form, which is how a whole service is opened.
	reached = false
	err = interceptor.StreamInterceptor()(nil, &fakeStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: "/orders.Public/Announcements"},
		func(interface{}, grpc.ServerStream) error {
			reached = true
			return nil
		})
	if err != nil || !reached {
		t.Errorf("a method under a public prefix was turned away: err = %v", err)
	}

	// A method outside the public list is still closed.
	err = interceptor.StreamInterceptor()(nil, &fakeStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: "/orders.Orders/Watch"},
		func(interface{}, grpc.ServerStream) error {
			t.Error("a method nobody opened was opened")
			return nil
		})
	if err == nil {
		t.Error("a method outside the public list was accepted without credentials")
	}
}
