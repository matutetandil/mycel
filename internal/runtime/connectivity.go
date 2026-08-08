package runtime

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/matutetandil/mycel/v2/internal/connector"
)

// DefaultConnectivityTimeout bounds each individual connector check. Without
// it an unreachable host can hang for the driver's own connect timeout, which
// for some databases is minutes.
const DefaultConnectivityTimeout = 10 * time.Second

// ConnectivityResult is the outcome of checking one connector.
type ConnectivityResult struct {
	Name   string
	Type   string
	Driver string

	// Err is nil when the connector was built, connected and reported healthy.
	Err error

	// Inbound reports a connector that listens rather than dials, so there was
	// nothing to reach and no verdict to give.
	Inbound bool

	// TimedOut reports that the check hit the timeout rather than failing
	// outright, which usually means a firewall dropping packets rather than a
	// host actively refusing.
	TimedOut bool

	// Duration is how long the check took.
	Duration time.Duration
}

// OK reports whether the connector connected and is healthy. Listeners are OK
// by definition: there is nothing they could fail to reach.
func (r ConnectivityResult) OK() bool { return r.Err == nil }

// CheckConnectivity builds every configured connector, connects to it and runs
// its health check, returning one result per connector sorted by name.
//
// This is what `mycel check` is for, and it deliberately does the work itself
// rather than reusing the startup path. Creating the runtime proves only that
// the configuration parses; connectors are not even constructed until Start, so
// a config pointing at an unreachable database or a broker with wrong
// credentials looked perfectly healthy until the service actually started.
//
// Unlike startup, a failure here is recorded and the sweep continues: the point
// of the command is a complete picture of what is reachable, not the first
// thing that broke. Each connector gets its own timeout and they run
// concurrently, so one unreachable host does not stall the rest.
func (r *Runtime) CheckConnectivity(ctx context.Context, timeout time.Duration) []ConnectivityResult {
	if timeout <= 0 {
		timeout = DefaultConnectivityTimeout
	}

	configs := r.config.Connectors
	results := make([]ConnectivityResult, len(configs))

	var wg sync.WaitGroup
	for i, cfg := range configs {
		if cfg == nil {
			continue
		}
		wg.Add(1)
		go func(i int, cfg *connector.Config) {
			defer wg.Done()
			results[i] = r.checkOne(ctx, cfg, timeout)
		}(i, cfg)
	}
	wg.Wait()

	// Drop the placeholder entries left by nil configs.
	checked := results[:0]
	for _, res := range results {
		if res.Name != "" {
			checked = append(checked, res)
		}
	}

	sort.Slice(checked, func(i, j int) bool { return checked[i].Name < checked[j].Name })
	return checked
}

// checkOne builds a single connector, connects it and reports its health.
func (r *Runtime) checkOne(ctx context.Context, cfg *connector.Config, timeout time.Duration) ConnectivityResult {
	res := ConnectivityResult{Name: cfg.Name, Type: cfg.Type, Driver: cfg.Driver}

	cfg.Environment = r.environment

	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	defer func() { res.Duration = time.Since(start) }()

	// Registering runs the factory, which is where a bad driver, a malformed
	// URL or an env() that resolved to nothing surfaces. Append the missing-env
	// hint for the same reason startup does: the factory error alone
	// ("http connector requires base_url") does not name the cause.
	if err := r.connectors.Register(checkCtx, cfg); err != nil {
		res.Err = fmt.Errorf("%w%s", err, missingEnvHint(cfg))
		return res
	}

	conn, err := r.connectors.Get(cfg.Name)
	if err != nil {
		res.Err = err
		return res
	}

	// A listener has no endpoint to reach, and its health check only reports
	// whether it has been started — which check deliberately does not do. It
	// was still worth building, since that is where a bad port or a malformed
	// TLS config surfaces.
	if inbound, ok := conn.(connector.InboundOnly); ok && inbound.InboundOnly() {
		res.Inbound = true
		return res
	}

	res.Err = connectAndPing(checkCtx, conn)

	// A driver that ignores context cancellation reports its own error; treat
	// the deadline as authoritative either way. The distinction between
	// "refused" and "no answer at all" is what tells you whether you are
	// looking at a wrong port or a firewall.
	if checkCtx.Err() != nil {
		res.TimedOut = true
		if res.Err == nil {
			res.Err = checkCtx.Err()
		}
	}
	return res
}

// connectAndPing opens the connection and verifies it. Connect surfaces bad
// credentials and unreachable hosts; Health catches a connector that connects
// lazily and only notices on first use.
func connectAndPing(ctx context.Context, conn connector.Connector) error {
	if err := conn.Connect(ctx); err != nil {
		return err
	}
	return conn.Health(ctx)
}
