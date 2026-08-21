package runtime

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

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
