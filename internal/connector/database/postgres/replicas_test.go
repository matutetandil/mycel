package postgres

import (
	"context"
	"database/sql"
	"net"
	"os"
	"testing"
	"time"
)

// Read replicas: reads go to a follower, writes to the primary. A service
// configures this to take load off the primary, and every way of getting it
// wrong is quiet — reads all landing on the primary looks exactly like it
// working, until somebody looks at the load.

func primary() *Connector {
	return New("orders_db", "localhost", 5432, "orders", "mycel", "mycel", "disable")
}

func TestAReplicaGetsTheDefaultsItNeeds(t *testing.T) {
	// A replica block that names only a host has to be usable: the port is the
	// usual one and the weight is one share.
	c := primary()
	c.AddReplica(ReplicaConfig{Host: "replica-1"})

	if len(c.replicas) != 1 {
		t.Fatalf("%d replicas", len(c.replicas))
	}
	if c.replicas[0].Port != 5432 {
		t.Errorf("port = %d, want the usual one", c.replicas[0].Port)
	}
	if c.replicas[0].Weight != 1 {
		t.Errorf("weight = %d, want one share", c.replicas[0].Weight)
	}
	// Adding one is what turns replica routing on: configuring a replica and
	// then not using it would be the worst of both.
	if !c.useReplicas {
		t.Error("a configured replica is not used")
	}
}

func TestReadsFallBackToThePrimaryWhenNoReplicaAnswered(t *testing.T) {
	// A replica that could not be reached must not stop reads — it is there to
	// share load, not to be a second point of failure.
	c := primary()
	c.db = &sql.DB{} // stands in for the primary connection

	if c.getReplicaDB() != c.db {
		t.Error("a service with no replicas configured read from somewhere else")
	}

	c.AddReplica(ReplicaConfig{Host: "replica-1"})
	if c.getReplicaDB() != c.db {
		t.Error("a replica that never connected was read from")
	}

	// And turning routing off sends everything to the primary, which is how
	// somebody takes a replica out of service without editing the list.
	c.SetUseReplicas(false)
	if c.getReplicaDB() != c.db {
		t.Error("routing was turned off and reads still went elsewhere")
	}
}

func TestReadsAreSpreadAcrossTheReplicas(t *testing.T) {
	// One replica taking every read is the same as having one replica.
	c := primary()
	c.useReplicas = true
	first, second := &sql.DB{}, &sql.DB{}
	c.replicaDBs = []*sql.DB{first, second}

	seen := map[*sql.DB]int{}
	for i := 0; i < 10; i++ {
		seen[c.getReplicaDB()]++
	}

	if seen[first] == 0 || seen[second] == 0 {
		t.Errorf("reads went to %v, want both replicas", seen)
	}
	if seen[first] != 5 || seen[second] != 5 {
		t.Errorf("reads = %v, want them spread evenly", seen)
	}
}

func TestThePoolIsWhatTheConfigurationAsksFor(t *testing.T) {
	// The pool decides how many connections a service holds open, which is
	// what a database's connection limit is spent on.
	c := primary()
	c.SetPoolConfig(50, 10, 30*time.Minute)

	if c.maxOpenConns != 50 || c.maxIdleConns != 10 {
		t.Errorf("pool = %d/%d", c.maxOpenConns, c.maxIdleConns)
	}
	if c.connMaxLifetime != 30*time.Minute {
		t.Errorf("lifetime = %v", c.connMaxLifetime)
	}

	// Nothing said leaves what was there, so a partial setting does not blank
	// the rest.
	c.SetPoolConfig(0, 0, 0)
	if c.maxOpenConns != 50 || c.maxIdleConns != 10 {
		t.Errorf("pool = %d/%d, want the previous values kept", c.maxOpenConns, c.maxIdleConns)
	}
}

func TestAReplicaIsReadFromTheConfiguration(t *testing.T) {
	replica := parseReplicaConfig(map[string]interface{}{
		"host":            "replica-1",
		"port":            5433,
		"weight":          3,
		"max_connections": 20,
	})

	if replica.Host != "replica-1" || replica.Port != 5433 {
		t.Errorf("replica = %+v", replica)
	}
	if replica.Weight != 3 || replica.MaxConns != 20 {
		t.Errorf("replica = %+v", replica)
	}

	// env() hands back strings, so a port written that way has to survive.
	fromEnv := parseReplicaConfig(map[string]interface{}{
		"host": "replica-1", "port": "5433",
	})
	if fromEnv.Port != 5433 {
		t.Errorf("port = %d, want the one written as a string", fromEnv.Port)
	}

	// And one that names only a host.
	bare := parseReplicaConfig(map[string]interface{}{"host": "replica-1"})
	if bare.Port != 5432 || bare.Weight != 1 {
		t.Errorf("replica = %+v, want the usual defaults", bare)
	}
}

// --- Against a real server ---------------------------------------------------

func liveDSN(t *testing.T) (host string, port int) {
	t.Helper()
	if os.Getenv("MYCEL_TEST_POSTGRES_DSN") == "" && !reachable("127.0.0.1:55432") {
		t.Skip("no PostgreSQL reachable at 127.0.0.1:55432 (the integration stack publishes it)")
	}
	return "127.0.0.1", 55432
}

func reachable(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func TestAReadGoesToTheReplicaAndStillAnswers(t *testing.T) {
	// The whole point, against a real server: a replica is connected, a read
	// is routed to it, and the answer is the same as the primary's. Pointing
	// the "replica" at the same server is the honest shape of this test —
	// what is under test is the routing, not replication itself.
	host, port := liveDSN(t)

	c := New("orders_db", host, port, "mycel_test", "mycel", "mycel", "disable")
	c.AddReplica(ReplicaConfig{Host: host, Port: port})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close(ctx)

	// The replica connected, or reads would quietly fall back to the primary
	// and the configuration would be doing nothing.
	if len(c.replicaDBs) != 1 {
		t.Fatalf("%d replicas connected, want the one configured", len(c.replicaDBs))
	}
	if c.getReplicaDB() == c.db {
		t.Error("the read was routed to the primary although a replica is connected")
	}

	if err := c.Health(ctx); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestAReplicaThatCannotBeReachedDoesNotStopTheService(t *testing.T) {
	// A follower being down is not an outage: reads fall back to the primary.
	host, port := liveDSN(t)

	c := New("orders_db", host, port, "mycel_test", "mycel", "mycel", "disable")
	c.AddReplica(ReplicaConfig{Host: "127.0.0.1", Port: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("a replica that is down stopped the service: %v", err)
	}
	defer c.Close(ctx)

	if len(c.replicaDBs) != 0 {
		t.Errorf("%d replicas connected, want none", len(c.replicaDBs))
	}
	if c.getReplicaDB() != c.db {
		t.Error("reads did not fall back to the primary")
	}
}
