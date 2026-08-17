package grpc

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

func create(t *testing.T, driver string, props map[string]interface{}) (connector.Connector, error) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewFactory(logger).Create(context.Background(), &connector.Config{
		Name: "svc", Type: "grpc", Driver: driver, Properties: props,
	})
}

func server(t *testing.T, props map[string]interface{}) *ServerConnector {
	t.Helper()
	conn, err := create(t, "server", props)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return conn.(*ServerConnector)
}

func client(t *testing.T, props map[string]interface{}) *ClientConnector {
	t.Helper()
	conn, err := create(t, "client", props)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return conn.(*ClientConnector)
}

func TestWithNoDriverTheConnectorIsAServer(t *testing.T) {
	conn, err := create(t, "", map[string]interface{}{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, ok := conn.(*ServerConnector); !ok {
		t.Errorf("connector = %T, want a server", conn)
	}
}

func TestADriverNobodyImplementsIsRefused(t *testing.T) {
	if _, err := create(t, "proxy", map[string]interface{}{}); err == nil {
		t.Error("a connector was built for a driver that does not exist")
	}
}

func TestASwitchWrittenAsAWordIsStillASwitch(t *testing.T) {
	// env() hands back strings, so reflection = env("GRPC_REFLECTION", "false")
	// arrives spelt out. Read as anything but a boolean it fell back to the
	// default — which is on — and a service kept publishing its entire schema
	// to anyone who asked, with the file saying it was turned off.
	if server(t, map[string]interface{}{"reflection": "false"}).config.Reflection {
		t.Error("reflection stayed on although the configuration turned it off")
	}
	if !server(t, map[string]interface{}{"reflection": "true"}).config.Reflection {
		t.Error("reflection = \"true\" did not turn it on")
	}
	if !server(t, map[string]interface{}{}).config.Reflection {
		t.Error("reflection is on by default, and was not")
	}
}

func TestAClientToldToSecureItselfDoesNot(t *testing.T) {
	// The same defect where it costs the most: insecure defaults to true, so a
	// spelt-out false left the client talking plaintext to a service the
	// operator believed it was reaching over TLS.
	if client(t, map[string]interface{}{"insecure": "false"}).config.Insecure {
		t.Error("the client stayed on plaintext although the configuration said otherwise")
	}
}

func TestTheServerListensWhereItWasTold(t *testing.T) {
	conn := server(t, map[string]interface{}{"host": "127.0.0.1", "port": "50999"})
	if conn.config.Port != 50999 || conn.config.Host != "127.0.0.1" {
		t.Errorf("address = %s:%d", conn.config.Host, conn.config.Port)
	}

	byDefault := server(t, map[string]interface{}{}).config
	if byDefault.Port != 50051 || byDefault.Host != "0.0.0.0" {
		t.Errorf("defaults = %s:%d", byDefault.Host, byDefault.Port)
	}
	if byDefault.MaxRecv != 4 || byDefault.MaxSend != 4 {
		t.Errorf("message size defaults = %d/%d MB", byDefault.MaxRecv, byDefault.MaxSend)
	}
}

func TestProtoFilesReachTheConnector(t *testing.T) {
	conn := server(t, map[string]interface{}{
		"proto_path":  "/protos",
		"proto_files": []interface{}{"user.proto", "order.proto", 42},
	})
	if len(conn.config.ProtoFiles) != 2 {
		t.Errorf("proto files = %v, want the two that are file names", conn.config.ProtoFiles)
	}
	if conn.config.ProtoPath != "/protos" {
		t.Errorf("proto path = %q", conn.config.ProtoPath)
	}
}

func TestServerAuthIsReadHoweverItIsCapitalised(t *testing.T) {
	// Read strictly, the settings underneath went unparsed and every call was
	// refused with "unknown auth type" while the file looked right.
	conn := server(t, map[string]interface{}{
		"auth": map[string]interface{}{
			"type": "JWT", "secret": "s3cret",
			"public": []interface{}{"grpc.health.v1.Health/Check"},
		},
	})
	if conn.config.Auth.Type != "jwt" {
		t.Errorf("auth type = %q", conn.config.Auth.Type)
	}
	if conn.config.Auth.JWT == nil || conn.config.Auth.JWT.Secret != "s3cret" {
		t.Fatalf("the JWT settings were not read: %+v", conn.config.Auth.JWT)
	}
	if len(conn.config.Auth.Public) != 1 {
		t.Errorf("public methods = %v", conn.config.Auth.Public)
	}
}

func TestAnAuthTypeTheServerCannotHonourIsRefused(t *testing.T) {
	_, err := create(t, "server", map[string]interface{}{
		"auth": map[string]interface{}{"type": "jtw", "secret": "s"},
	})
	if err == nil {
		t.Fatal("a server was built that would refuse every call it received")
	}
	if !strings.Contains(err.Error(), "jtw") {
		t.Errorf("error = %q, want the word that was written", err)
	}
}

func TestWritingATLSBlockTakesTheClientOffPlaintext(t *testing.T) {
	conn := client(t, map[string]interface{}{
		"target": "svc.internal:443",
		"tls":    map[string]interface{}{"ca_file": "/etc/ssl/ca.pem"},
	})
	if conn.config.TLS == nil {
		t.Fatal("the TLS block was written and nothing came of it")
	}
	if conn.config.Insecure {
		t.Error("the client stayed on plaintext although TLS was configured")
	}
}

func TestClientTimeoutsAreReadAsDurations(t *testing.T) {
	conn := client(t, map[string]interface{}{
		"target": "svc:50051", "timeout": "5s", "connect_timeout": "2s",
		"retry_count": "5", "retry_backoff": "250ms",
	})
	if conn.config.Timeout != 5*time.Second || conn.config.ConnectTimeout != 2*time.Second {
		t.Errorf("timeouts = %v/%v", conn.config.Timeout, conn.config.ConnectTimeout)
	}
	if conn.config.RetryCount != 5 || conn.config.RetryBackoff != 250*time.Millisecond {
		t.Errorf("retry = %d every %v", conn.config.RetryCount, conn.config.RetryBackoff)
	}
}

func TestKeepAliveIsOnlySetWhenAskedFor(t *testing.T) {
	// A keep-alive too eager for the server's policy gets the connection
	// dropped, so it stays absent unless the block is written.
	if client(t, map[string]interface{}{"target": "svc:50051"}).config.KeepAlive != nil {
		t.Error("keep-alive was configured although nothing asked for it")
	}

	conn := client(t, map[string]interface{}{
		"target":     "svc:50051",
		"keep_alive": map[string]interface{}{"time": "20s", "timeout": "3s"},
	})
	if conn.config.KeepAlive == nil || conn.config.KeepAlive.Time != 20*time.Second {
		t.Errorf("keep-alive = %+v", conn.config.KeepAlive)
	}
}

func TestLoadBalancingCanBeWrittenEitherWay(t *testing.T) {
	// The short form is a policy name, which is what most configurations need.
	short := client(t, map[string]interface{}{
		"target": "svc:50051", "load_balancing": "round_robin",
	})
	if short.config.LoadBalancing == nil || short.config.LoadBalancing.Policy != "round_robin" {
		t.Fatalf("load balancing = %+v", short.config.LoadBalancing)
	}

	long := client(t, map[string]interface{}{
		"target": "svc:50051",
		"load_balancing": map[string]interface{}{
			"policy":       "round_robin",
			"targets":      []interface{}{"a:50051", "b:50051"},
			"health_check": true,
		},
	})
	if long.config.LoadBalancing == nil || len(long.config.LoadBalancing.Targets) != 2 {
		t.Fatalf("load balancing = %+v", long.config.LoadBalancing)
	}
	if !long.config.LoadBalancing.HealthCheck {
		t.Error("health checking was asked for and not configured")
	}

	// A single target is written as one word rather than a list of one.
	single := client(t, map[string]interface{}{
		"target":         "svc:50051",
		"load_balancing": map[string]interface{}{"targets": "only:50051"},
	})
	if len(single.config.LoadBalancing.Targets) != 1 {
		t.Errorf("targets = %v", single.config.LoadBalancing.Targets)
	}
}

func TestTheFactoryAnswersForItsOwnType(t *testing.T) {
	f := NewFactory(nil)
	if f.Type() != "grpc" || !f.Supports("grpc", "") || f.Supports("http", "") {
		t.Errorf("Type = %q Supports(grpc) = %v Supports(http) = %v",
			f.Type(), f.Supports("grpc", ""), f.Supports("http", ""))
	}
}
