package mock

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// A connector that serves is left alone.
//
// Wrapping replaces what a connector answers when it is asked something, and
// one that serves is not asked — it receives. Wrapping it anyway cost it the
// ability to serve: the wrapper offers the ordinary connector surface and
// nothing else, so the runtime's check for something it can start stopped
// matching and the HTTP server was never started. The banner still said
// "listening", because that line is printed from the configuration, so a
// service with mocking on came up looking healthy and refused every request.

type serving struct{ name string }

func (s *serving) Name() string                  { return s.name }
func (s *serving) Type() string                  { return "rest" }
func (s *serving) Connect(context.Context) error { return nil }
func (s *serving) Close(context.Context) error   { return nil }
func (s *serving) Health(context.Context) error  { return nil }
func (s *serving) Start(context.Context) error   { return nil }

type answering struct{ name string }

func (a *answering) Name() string                  { return a.name }
func (a *answering) Type() string                  { return "http" }
func (a *answering) Connect(context.Context) error { return nil }
func (a *answering) Close(context.Context) error   { return nil }
func (a *answering) Health(context.Context) error  { return nil }

func managerFor(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(&Config{Enabled: true, Path: t.TempDir()})
	m.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return m
}

func TestAConnectorThatServesIsNotWrapped(t *testing.T) {
	m := managerFor(t)

	original := &serving{name: "api"}
	wrapped, err := m.WrapConnector("api", original)
	if err != nil {
		t.Fatalf("WrapConnector: %v", err)
	}

	if wrapped != connector.Connector(original) {
		t.Error("a connector that serves was wrapped, and the wrapper cannot serve")
	}
	if _, canStart := wrapped.(interface{ Start(context.Context) error }); !canStart {
		t.Error("what came back cannot be started, so the runtime would never start it")
	}
}

func TestAConnectorThatAnswersIsWrapped(t *testing.T) {
	m := managerFor(t)

	original := &answering{name: "pricing"}
	wrapped, err := m.WrapConnector("pricing", original)
	if err != nil {
		t.Fatalf("WrapConnector: %v", err)
	}

	if wrapped == connector.Connector(original) {
		t.Error("a connector that answers was left unwrapped, so its calls cannot be mocked")
	}
}
