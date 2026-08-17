package mysql

import (
	"context"
	"database/sql"
	"net"
	"os"
	"testing"
	"time"
)

// Read replicas, the MySQL side. The same feature as the Postgres connector
// and the same quiet failure: reads all landing on the primary looks exactly
// like it working.

func primary() *Connector {
	return New("orders_db", "localhost", 3306, "orders", "mycel", "mycel", "utf8mb4")
}

func TestAReplicaGetsTheDefaultsItNeeds(t *testing.T) {
	c := primary()
	c.AddReplica(ReplicaConfig{Host: "replica-1"})

	if len(c.replicas) != 1 {
		t.Fatalf("%d replicas", len(c.replicas))
	}
	if c.replicas[0].Port != 3306 {
		t.Errorf("port = %d, want the usual one for MySQL", c.replicas[0].Port)
	}
	if c.replicas[0].Weight != 1 {
		t.Errorf("weight = %d", c.replicas[0].Weight)
	}
	if !c.useReplicas {
		t.Error("a configured replica is not used")
	}
}

func TestReadsFallBackToThePrimary(t *testing.T) {
	c := primary()
	c.db = &sql.DB{}

	if c.getReplicaDB() != c.db {
		t.Error("a service with no replicas read from somewhere else")
	}

	c.AddReplica(ReplicaConfig{Host: "replica-1"})
	if c.getReplicaDB() != c.db {
		t.Error("a replica that never connected was read from")
	}

	c.SetUseReplicas(false)
	if c.getReplicaDB() != c.db {
		t.Error("routing was turned off and reads still went elsewhere")
	}
}

func TestReadsAreSpreadAcrossTheReplicas(t *testing.T) {
	c := primary()
	c.useReplicas = true
	first, second := &sql.DB{}, &sql.DB{}
	c.replicaDBs = []*sql.DB{first, second}

	seen := map[*sql.DB]int{}
	for i := 0; i < 10; i++ {
		seen[c.getReplicaDB()]++
	}
	if seen[first] != 5 || seen[second] != 5 {
		t.Errorf("reads = %v, want them spread evenly", seen)
	}
}

func TestAReplicaIsReadFromTheConfiguration(t *testing.T) {
	replica := parseReplicaConfig(map[string]interface{}{
		"host": "replica-1", "port": 3307, "weight": 3, "max_connections": 20,
	})
	if replica.Host != "replica-1" || replica.Port != 3307 {
		t.Errorf("replica = %+v", replica)
	}
	if replica.Weight != 3 || replica.MaxConns != 20 {
		t.Errorf("replica = %+v", replica)
	}

	// env() hands back strings.
	fromEnv := parseReplicaConfig(map[string]interface{}{"host": "replica-1", "port": "3307"})
	if fromEnv.Port != 3307 {
		t.Errorf("port = %d, want the one written as a string", fromEnv.Port)
	}

	bare := parseReplicaConfig(map[string]interface{}{"host": "replica-1"})
	if bare.Port != 3306 || bare.Weight != 1 {
		t.Errorf("replica = %+v, want the usual defaults", bare)
	}
}

// --- Against a real server ---------------------------------------------------

func liveMySQL(t *testing.T) (string, int) {
	t.Helper()
	if os.Getenv("MYCEL_TEST_MYSQL_DSN") == "" && !reachable("127.0.0.1:33306") {
		t.Skip("no MySQL reachable at 127.0.0.1:33306 (the integration stack publishes it)")
	}
	return "127.0.0.1", 33306
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
	host, port := liveMySQL(t)

	c := New("orders_db", host, port, "mycel_test", "mycel", "mycel", "utf8mb4")
	c.AddReplica(ReplicaConfig{Host: host, Port: port})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close(ctx)

	if len(c.replicaDBs) != 1 {
		t.Fatalf("%d replicas connected, want the one configured", len(c.replicaDBs))
	}
	if c.getReplicaDB() == c.db {
		t.Error("the read was routed to the primary although a replica is connected")
	}
	if err := c.Health(ctx); err != nil {
		t.Errorf("Health: %v", err)
	}
	// The handle a flow's transaction runs on is the primary, whatever the
	// read routing says.
	if c.DB() == nil {
		t.Error("the connector exposes no database handle, so no transaction can run")
	}
}

func TestAReplicaThatCannotBeReachedDoesNotStopTheService(t *testing.T) {
	host, port := liveMySQL(t)

	c := New("orders_db", host, port, "mycel_test", "mycel", "mycel", "utf8mb4")
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
