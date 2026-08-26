package examples

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// The Kafka example, followed all the way round.
//
// Its README's two commands are a publish and a read, and between them sits a
// consumer that runs on its own schedule — so a harness that runs each command
// once and moves on can watch the publish succeed and the read come back empty
// without anything being wrong. What has to be asserted is that the order
// posted over HTTP comes back out of the topic and lands in the database.
func TestTheKafkaExampleGoesAllTheWayRound(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	brokers := os.Getenv("MYCEL_TEST_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("set MYCEL_TEST_KAFKA_BROKERS to run this against a real broker")
	}

	// A group of this run's own. The example's default is a fixed name, and a
	// second consumer in the same group takes the partition and the message
	// with it — which is exactly what a developer running the example beside
	// this test would do.
	group := fmt.Sprintf("mycel-order-writers-test-%d", time.Now().UnixNano())

	svc := start(t, "kafka",
		"KAFKA_BROKERS="+address(t, "MYCEL_TEST_KAFKA_BROKERS"),
		"KAFKA_GROUP="+group)
	port := svc.ports[3000]
	if port == 0 {
		t.Fatal("the example's REST port was not moved; nothing to talk to")
	}
	base := fmt.Sprintf("http://localhost:%d", port)

	// A distinct id per run: the topic keeps what earlier runs published, and
	// a consumer group reading from the earliest offset will see all of it.
	id := fmt.Sprintf("A-%d", time.Now().UnixNano())

	status, answer := svc.run(t, fmt.Sprintf(
		`curl -X POST %s/orders -H 'Content-Type: application/json' -d '{"id":"%s","customer":"Acme Inc","total":240.00}'`,
		base, id))
	if status != 200 {
		t.Fatalf("publishing answered %d: %s", status, answer)
	}
	var published map[string]interface{}
	if err := json.Unmarshal([]byte(answer), &published); err != nil {
		t.Fatalf("the publish answer was not JSON: %v: %s", err, answer)
	}
	if published["status"] != "published" {
		t.Errorf("publishing answered %s", answer)
	}

	// The consumer writes on its own; this is the wait the README describes as
	// "a moment later".
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		listStatus, listed := svc.run(t, fmt.Sprintf(`curl %s/orders`, base))
		if listStatus != 200 {
			t.Fatalf("listing answered %d: %s", listStatus, listed)
		}
		var rows []map[string]interface{}
		if err := json.Unmarshal([]byte(listed), &rows); err != nil {
			t.Fatalf("the listing was not JSON: %v: %s", err, listed)
		}
		for _, row := range rows {
			if row["id"] == id {
				if row["customer"] != "Acme Inc" {
					t.Errorf("the row the consumer wrote is not the order published: %v", row)
				}
				if row["written"] == nil || row["written"] == "" {
					t.Errorf("the consumer's transform did not run: %v", row)
				}
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("the order published as %s never came back out of the topic; the service log says:\n%s", id, svc.tail())
}
