package runtime

import (
	"context"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
	"github.com/matutetandil/mycel/v2/internal/metrics"
)

// The connector metrics were defined from the start and never recorded, so
// mycel_connector_operations_total and mycel_connector_latency_seconds were
// documented but permanently absent from /metrics.
//
// They are recorded through these wrappers rather than at each call site
// because the flow pipeline reads and writes from a dozen places (the main
// read/write path, per-destination writes, steps, enrich), and threading
// timing through each one by hand is how call sites get missed.
//
// The operation label is deliberately coarse — read, write, call — and never
// the query or target. A SQL statement or a per-entity target as a Prometheus
// label is unbounded cardinality; what you actually want from this metric is
// "which connector is slow or failing", and the flow metrics already carry the
// per-flow breakdown.

// meteredRead runs a connector read and records its outcome and latency.
func meteredRead(ctx context.Context, r connector.Reader, q connector.Query) (*connector.Result, error) {
	start := time.Now()
	res, err := r.Read(ctx, q)
	recordConnectorOp(r, "read", start, err)
	return res, err
}

// meteredWrite runs a connector write and records its outcome and latency.
func meteredWrite(ctx context.Context, w connector.Writer, data *connector.Data) (*connector.Result, error) {
	start := time.Now()
	res, err := w.Write(ctx, data)
	recordConnectorOp(w, "write", start, err)
	return res, err
}

// meteredCall runs a connector call and records its outcome and latency.
func meteredCall(ctx context.Context, c Caller, operation string, params map[string]interface{}) (interface{}, error) {
	start := time.Now()
	res, err := c.Call(ctx, operation, params)
	recordConnectorOp(c, "call", start, err)
	return res, err
}

// recordConnectorOp records one connector operation. The value arrives as a
// Reader/Writer/Caller, which are narrow interfaces, so the identity comes off
// an optional assertion: a connector that does not report its own name is
// skipped rather than recorded under an empty label.
func recordConnectorOp(v interface{}, operation string, start time.Time, err error) {
	named, ok := v.(interface {
		Name() string
		Type() string
	})
	if !ok {
		return
	}

	status := "success"
	if err != nil {
		status = "error"
	}
	metrics.Default().RecordConnectorOperation(named.Name(), named.Type(), operation, status, time.Since(start))
}
