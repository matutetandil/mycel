package rest

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
)

// A server that cannot take its port says so, at startup.
//
// ListenAndServe was called inside the goroutine, so a port already in use was
// an error logged from a background thread while startup carried on: the
// banner said "listening on :3000", the service said "Ready", the health
// endpoint said healthy, and nothing was listening. A deployment looked fine
// and answered nothing — which is worse than not starting, because nothing
// asks why.
func TestARestServerThatCannotTakeItsPortFailsToStart(t *testing.T) {
	// Somebody else has it, on every interface — which is what the connector
	// asks for, and what another service on the same host holds.
	held, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	_, portText, _ := net.SplitHostPort(held.Addr().String())
	port, _ := strconv.Atoi(portText)

	c := New("api", port, nil, slog.Default())
	startErr := c.Start(context.Background())
	if startErr == nil {
		_ = c.Close(context.Background())
		t.Fatal("the connector started on a port it does not have")
	}
	if !strings.Contains(startErr.Error(), "api") || !strings.Contains(startErr.Error(), portText) {
		t.Errorf("the refusal reads %q; it should name the connector and the port", startErr)
	}
}

// And one whose port is free starts and can be shut down again.
func TestARestServerTakesAFreePort(t *testing.T) {
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portText, _ := net.SplitHostPort(free.Addr().String())
	port, _ := strconv.Atoi(portText)
	free.Close()

	c := New("api", port, nil, slog.Default())
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Errorf("close: %v", err)
	}
}
