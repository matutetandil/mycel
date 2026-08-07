package runtime

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/flow"
	"github.com/matutetandil/mycel/v2/internal/parser"
)

// dispatchReport runs the report over a config and returns what was logged.
func dispatchReport(t *testing.T, connectors []*connector.Config, flows []*flow.Config) string {
	t.Helper()

	var buf bytes.Buffer
	rt := &Runtime{
		config:         &parser.Configuration{Connectors: connectors, Flows: flows},
		schemaRegistry: NewSchemaRegistry(),
	}
	rt.reportDispatch(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), rt.schemaRegistry)
	return buf.String()
}

func mqConnector(name string) *connector.Config {
	return &connector.Config{Name: name, Type: "mq", Driver: "rabbitmq"}
}

func mqFlow(name, conn, operation string) *flow.Config {
	params := map[string]interface{}{}
	if operation != "" {
		params["operation"] = operation
	}
	return &flow.Config{
		Name: name,
		From: &flow.FromConfig{Connector: conn, ConnectorParams: params},
	}
}

// The shape that bit the project twice: every flow on the connector declares a
// narrowing pattern, so a delivery matching none is dropped with nothing to
// catch it.
func TestReportDispatch_WarnsWhenEveryFlowIsNarrowed(t *testing.T) {
	out := dispatchReport(t,
		[]*connector.Config{mqConnector("rabbit")},
		[]*flow.Config{
			mqFlow("item_create", "rabbit", "all.in.magento.q"),
			mqFlow("item_update", "rabbit", "all.in.magento.q"),
		},
	)

	if !strings.Contains(out, "will be DROPPED") {
		t.Errorf("expected a drop warning, got:\n%s", out)
	}
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("the drop notice must be a warning, got:\n%s", out)
	}
	// Both flows named, and the pattern spelled out.
	for _, want := range []string{"item_create", "item_update", "all.in.magento.q"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// A catch-all always has a handler, so nothing can be dropped at lookup and
// there is nothing to warn about.
func TestReportDispatch_CatchAllIsSilent(t *testing.T) {
	out := dispatchReport(t,
		[]*connector.Config{mqConnector("rabbit")},
		[]*flow.Config{mqFlow("asset_upsert", "rabbit", "")},
	)

	if strings.Contains(out, "DROPPED") {
		t.Errorf("a catch-all flow must not warn about drops:\n%s", out)
	}
	if !strings.Contains(out, "accepts every message") {
		t.Errorf("expected the catch-all to be stated explicitly:\n%s", out)
	}
}

// One catch-all among narrowed flows is enough: every delivery still reaches a
// handler.
func TestReportDispatch_CatchAllAlongsideNarrowedIsSilent(t *testing.T) {
	out := dispatchReport(t,
		[]*connector.Config{mqConnector("rabbit")},
		[]*flow.Config{
			mqFlow("specific", "rabbit", "orders.created"),
			mqFlow("everything_else", "rabbit", "#"),
		},
	)

	if strings.Contains(out, "DROPPED") {
		t.Errorf("a catch-all sibling means nothing is dropped at lookup:\n%s", out)
	}
}

// On a REST source `operation` addresses an endpoint rather than narrowing a
// subscription, so there is nothing to report.
func TestReportDispatch_IgnoresRequestStyleSources(t *testing.T) {
	out := dispatchReport(t,
		[]*connector.Config{{Name: "api", Type: "rest"}},
		[]*flow.Config{mqFlow("get_users", "api", "GET /users")},
	)

	if strings.TrimSpace(out) != "" {
		t.Errorf("a rest source should produce no dispatch report:\n%s", out)
	}
}

// Each connector is judged on its own flows: a safe one must not silence a
// risky one.
func TestReportDispatch_PerConnector(t *testing.T) {
	out := dispatchReport(t,
		[]*connector.Config{mqConnector("risky"), mqConnector("safe")},
		[]*flow.Config{
			mqFlow("narrowed", "risky", "all.in.magento.q"),
			mqFlow("catch_all", "safe", ""),
		},
	)

	if !strings.Contains(out, "DROPPED") {
		t.Errorf("the narrowed connector should still warn:\n%s", out)
	}
	if strings.Count(out, "DROPPED") != 1 {
		t.Errorf("only the narrowed connector should warn, got %d:\n%s", strings.Count(out, "DROPPED"), out)
	}
}

// The regression this guards: an earlier version reported "no flow reads from
// this source" for any connector whose schema allows it to be a source. A
// database used only as a write target, and an MQ connector with only a
// publisher block, both matched — so every real consumer logged errors about
// connectors that were working exactly as configured. Whether a connector will
// consume is only knowable where it starts consuming; the drivers report it.
func TestReportDispatch_NoOrphanWarningForNonSourceConnectors(t *testing.T) {
	out := dispatchReport(t,
		[]*connector.Config{
			mqConnector("rabbit"),                                   // consumed
			mqConnector("rabbit_returns"),                           // publisher only
			{Name: "magento_db", Type: "database", Driver: "mysql"}, // write target
		},
		[]*flow.Config{mqFlow("item_update", "rabbit", "")},
	)

	for _, unwanted := range []string{"magento_db", "rabbit_returns", "no flow reads"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("connectors that are not configured as sources must not be reported (%q):\n%s", unwanted, out)
		}
	}
	if !strings.Contains(out, "accepts every message") {
		t.Errorf("the consumed connector should still be reported:\n%s", out)
	}
}
