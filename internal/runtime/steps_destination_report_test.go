package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v3/internal/connector"
	"github.com/matutetandil/mycel/v3/internal/flow"
)

// A read flow with steps answers out of its steps: the destination is neither
// read nor written, and nothing said so.
//
// The use-cases guide had a recipe built on the opposite belief — read the
// user, then a step fetches the weather for their city — which answered "no
// such key" for every field it was meant to fill, because the read never
// happened. The `to` block was simply ignored.
func TestAReadFlowWithStepsSaysItsDestinationIsUnused(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, nil))

	reportIgnoredDestinations(logger, []*flow.Config{
		{
			Name:  "get_user_with_weather",
			From:  &flow.FromConfig{ConnectorParams: map[string]interface{}{"operation": "GET /users/:id"}},
			To:    &flow.ToConfig{Connector: "db"},
			Steps: []*flow.StepConfig{{Name: "weather"}},
		},
	}, nil)

	said := out.String()
	if !strings.Contains(said, "get_user_with_weather") {
		t.Errorf("the warning does not name the flow: %s", said)
	}
	if !strings.Contains(said, "enrich") {
		t.Errorf("the warning does not point at what to use instead: %s", said)
	}
}

// The flows it must stay quiet about: a write, which does use its destination,
// and a steps flow that never named one.
func TestOnlyTheFlowsThatIgnoreADestinationAreReported(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, nil))

	reportIgnoredDestinations(logger, []*flow.Config{
		{
			Name:  "create_order",
			From:  &flow.FromConfig{ConnectorParams: map[string]interface{}{"operation": "POST /orders"}},
			To:    &flow.ToConfig{Connector: "db"},
			Steps: []*flow.StepConfig{{Name: "customer"}},
		},
		{
			Name:  "gather",
			From:  &flow.FromConfig{ConnectorParams: map[string]interface{}{"operation": "GET /report"}},
			Steps: []*flow.StepConfig{{Name: "a"}},
		},
		{
			Name: "plain_read",
			From: &flow.FromConfig{ConnectorParams: map[string]interface{}{"operation": "GET /items"}},
			To:   &flow.ToConfig{Connector: "db"},
		},
	}, nil)

	if said := out.String(); said != "" {
		t.Errorf("warned about a flow that uses its destination:\n%s", said)
	}
}

// The report decided "read" from the source operation alone, while the flow
// itself is dispatched by the full reading of its intent. Anything that intent
// turns into a write — a queue consumer, a remote subscription, a transaction —
// was reported as ignoring a destination it writes to on every message. The
// most common consumer shape, look something up in a step and write, was told
// at every startup that its destination was unused.
type typedSource struct {
	recordingWriter
	typ string
}

func (s *typedSource) Type() string { return s.typ }

// readWriteStore is a destination that can be read and written, like a
// database: the kind the report is about.
type readWriteStore struct{ recordingWriter }

func (*readWriteStore) Read(context.Context, connector.Query) (*connector.Result, error) {
	return &connector.Result{}, nil
}

func TestTheReportUsesTheFlowsOwnReadingOfItsIntent(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, nil))

	registry := connector.NewRegistry()
	registry.Replace("orders_queue", &typedSource{recordingWriter: recordingWriter{name: "orders_queue"}, typ: "mq"})
	registry.Replace("products_api", &typedSource{recordingWriter: recordingWriter{name: "products_api"}, typ: "graphql"})
	registry.Replace("db", &readWriteStore{recordingWriter{name: "db"}})

	reportIgnoredDestinations(logger, []*flow.Config{
		{
			Name:  "consume_orders",
			From:  &flow.FromConfig{Connector: "orders_queue", ConnectorParams: map[string]interface{}{"operation": "orders.created"}},
			To:    &flow.ToConfig{Connector: "db"},
			Steps: []*flow.StepConfig{{Name: "customer"}},
		},
		{
			Name:  "track_prices",
			From:  &flow.FromConfig{Connector: "products_api", ConnectorParams: map[string]interface{}{"operation": "Subscription.priceChanged"}},
			To:    &flow.ToConfig{Connector: "db"},
			Steps: []*flow.StepConfig{{Name: "product"}},
		},
		{
			Name:  "settle",
			From:  &flow.FromConfig{ConnectorParams: map[string]interface{}{"operation": "GET /settle"}},
			To:    &flow.ToConfig{Connector: "db", Transaction: &flow.TransactionConfig{}},
			Steps: []*flow.StepConfig{{Name: "balance"}},
		},
	}, registry)

	if said := out.String(); said != "" {
		t.Errorf("warned about flows that write to their destination:\n%s", said)
	}
}
