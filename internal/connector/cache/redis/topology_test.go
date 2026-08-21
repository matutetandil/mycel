package redis

import (
	"context"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/matutetandil/mycel/v3/internal/connector/cache/types"
)

// Connecting to a Redis that is more than one machine.
//
// A cache in production is usually a cluster or a set behind Sentinel, and
// which of the three shapes a connector takes is decided from configuration
// that nothing exercised. The failures are quiet ones: a cluster configured
// with no nodes, a Sentinel with no master name, or pool limits that were read
// from the file and never reached the client — a service that behaves until
// there is load, and then holds far more connections than the cache allows.

func clusterConnector(t *testing.T, cfg *types.ClusterConfig, pool types.PoolConfig) *Connector {
	t.Helper()
	return New("cache", &types.Config{Mode: "cluster", Cluster: cfg, Pool: pool})
}

func TestConnectingToACluster(t *testing.T) {
	c := clusterConnector(t, &types.ClusterConfig{
		Nodes:          []string{"10.0.0.1:6379", "10.0.0.2:6379", "10.0.0.3:6379"},
		Password:       "secret",
		MaxRedirects:   5,
		RouteByLatency: true,
		ReadOnly:       true,
	}, types.PoolConfig{
		MaxConnections: 50,
		MinIdle:        5,
		MaxIdleTime:    2 * time.Minute,
		ConnectTimeout: 3 * time.Second,
	})

	// Building the client does not dial: what is being checked is that the
	// configuration reached it, not that a cluster is running.
	if err := c.connectCluster(context.Background()); err != nil {
		t.Fatalf("connectCluster: %v", err)
	}

	client, ok := c.client.(*goredis.ClusterClient)
	if !ok {
		t.Fatalf("client = %T, want a cluster client", c.client)
	}
	opts := client.Options()

	if len(opts.Addrs) != 3 {
		t.Errorf("addresses = %v, want all three nodes", opts.Addrs)
	}
	if opts.Password != "secret" {
		t.Error("the password did not reach the client")
	}
	// A redirect is how a cluster tells a client the key lives elsewhere;
	// too few and a resharding cluster starts failing commands.
	if opts.MaxRedirects != 5 {
		t.Errorf("max redirects = %d", opts.MaxRedirects)
	}
	if !opts.RouteByLatency || !opts.ReadOnly {
		t.Errorf("routing settings were dropped: %+v", opts)
	}
	// Pool limits are the ones that matter under load: a cache with a
	// connection limit and a client that ignores its own is an outage waiting
	// for traffic.
	if opts.PoolSize != 50 || opts.MinIdleConns != 5 {
		t.Errorf("pool = %d/%d, want 50/5", opts.PoolSize, opts.MinIdleConns)
	}
	if opts.ConnMaxIdleTime != 2*time.Minute || opts.DialTimeout != 3*time.Second {
		t.Errorf("timeouts = %v/%v", opts.ConnMaxIdleTime, opts.DialTimeout)
	}

	if err := c.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestAClusterWithNoNodes(t *testing.T) {
	// Nothing to connect to, and the message has to say so — a client built
	// with an empty address list fails later with something about a slot map.
	for name, cfg := range map[string]*types.ClusterConfig{
		"no cluster block at all": nil,
		"a block with no nodes":   {Nodes: nil},
	} {
		t.Run(name, func(t *testing.T) {
			c := clusterConnector(t, cfg, types.PoolConfig{})
			err := c.connectCluster(context.Background())
			if err == nil {
				t.Fatal("a cluster with nowhere to connect was accepted")
			}
			if !strings.Contains(err.Error(), "nodes") {
				t.Errorf("the error does not say what is missing: %v", err)
			}
		})
	}
}

func TestConnectingThroughSentinel(t *testing.T) {
	// Sentinel is what makes a cache survive losing its master: the client
	// asks the sentinels who the master is now, rather than being told an
	// address that stops being right during a failover.
	c := New("cache", &types.Config{
		Mode: "sentinel",
		Sentinel: &types.SentinelConfig{
			MasterName:     "mymaster",
			Nodes:          []string{"10.0.0.1:26379", "10.0.0.2:26379"},
			Password:       "sentinel-secret",
			MasterPassword: "master-secret",
			DB:             3,
			RouteByLatency: true,
		},
		Pool: types.PoolConfig{MaxConnections: 20, MinIdle: 2},
	})

	if err := c.connectSentinel(context.Background()); err != nil {
		t.Fatalf("connectSentinel: %v", err)
	}
	if c.client == nil {
		t.Fatal("no client was built")
	}

	// Spreading reads over the replicas needs the client that knows about all
	// of them, not the one that talks to whoever is master. Asking the plain
	// failover client for it does not fail — it panics inside the driver while
	// the service is starting, so a documented setting took the process down
	// with a stack trace rather than a line saying what was wrong.
	if _, ok := c.client.(*goredis.ClusterClient); !ok {
		t.Fatalf("client = %T, want the one that can read from replicas", c.client)
	}

	if err := c.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestSentinelWithoutRoutingIsTheOrdinaryFailoverClient(t *testing.T) {
	// The common shape: one master, found through the sentinels, with reads
	// going to it like everything else.
	c := New("cache", &types.Config{
		Mode: "sentinel",
		Sentinel: &types.SentinelConfig{
			MasterName: "mymaster",
			Nodes:      []string{"10.0.0.1:26379"},
			DB:         3,
		},
		Pool: types.PoolConfig{MaxConnections: 20, MinIdle: 2, MaxIdleTime: time.Minute, ConnectTimeout: time.Second},
	})

	if err := c.connectSentinel(context.Background()); err != nil {
		t.Fatalf("connectSentinel: %v", err)
	}
	client, ok := c.client.(*goredis.Client)
	if !ok {
		t.Fatalf("client = %T, want a failover client", c.client)
	}
	opts := client.Options()
	if opts.DB != 3 {
		t.Errorf("database = %d, want the configured one", opts.DB)
	}
	if opts.PoolSize != 20 || opts.MinIdleConns != 2 {
		t.Errorf("pool = %d/%d, want 20/2", opts.PoolSize, opts.MinIdleConns)
	}
	if opts.ConnMaxIdleTime != time.Minute || opts.DialTimeout != time.Second {
		t.Errorf("timeouts = %v/%v", opts.ConnMaxIdleTime, opts.DialTimeout)
	}
}

func TestSentinelWithoutEnoughToFindTheMaster(t *testing.T) {
	for name, tc := range map[string]struct {
		config *types.SentinelConfig
		says   string
	}{
		"no sentinel block": {nil, "master"},
		"no master name":    {&types.SentinelConfig{Nodes: []string{"10.0.0.1:26379"}}, "master"},
		"no sentinel nodes": {&types.SentinelConfig{MasterName: "mymaster"}, "nodes"},
	} {
		t.Run(name, func(t *testing.T) {
			c := New("cache", &types.Config{Mode: "sentinel", Sentinel: tc.config})
			err := c.connectSentinel(context.Background())
			if err == nil {
				t.Fatal("a sentinel configuration that cannot find a master was accepted")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the error does not say what is missing: %v", err)
			}
		})
	}
}

func TestTheShapeIsChosenFromTheConfiguration(t *testing.T) {
	// Connect dispatches on the mode, and a mode nobody recognises has to
	// behave as the ordinary single server rather than as nothing.
	for mode, want := range map[string]string{
		"":           "standalone",
		"standalone": "standalone",
		"cluster":    "cluster",
		"sentinel":   "sentinel",
	} {
		c := New("cache", &types.Config{Mode: mode})
		if c.mode != want {
			t.Errorf("mode %q became %q, want %q", mode, c.mode, want)
		}
	}

	// A misconfigured cluster fails at start-up rather than at the first
	// request, which is the difference between a deployment that does not go
	// out and one that goes out broken.
	c := New("cache", &types.Config{Mode: "cluster"})
	if err := c.Connect(context.Background()); err == nil {
		t.Error("a cluster connector with no nodes started")
	}
}
