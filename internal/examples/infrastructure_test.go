package examples

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	amqp "github.com/rabbitmq/amqp091-go"
)

// The examples that want a broker or a database server, started against one.
//
// Following a README only reaches the examples that need nothing but Mycel.
// Most want something else, and they are where the flows worth exercising live:
// a queue consumer, a retry with a dead letter queue, a workflow that keeps its
// state in Postgres. Nothing had ever run them.
//
// The coordinates come from the same variables the integration runner already
// exports, translated into the ones each example reads — every one of these
// takes its host and credentials from env(), which is how a reader points an
// example at their own database.

// infrastructure describes what an example needs and how it asks for it.
type infrastructure struct {
	example string
	// needs is the address that has to answer before this is worth running.
	needs []string
	// env is what the example reads, built from the runner's variables.
	env func(t *testing.T) []string
	// then runs once the README's commands have been followed, for what a
	// status code cannot say: which services the flows actually called.
	then func(t *testing.T, calls []string)
}

func dsn(t *testing.T, name string) *url.URL {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("%s is not a URL: %v", name, err)
	}
	return parsed
}

// freshPostgres makes a database of this example's own and returns the
// coordinates of it.
//
// The server is shared and remembers: a migration applied by an earlier run is
// recorded as applied, so a changed schema — or the rows it seeds — is never
// run again, and the example is checked against whatever the last run left
// behind. A database per example is what makes the check mean the same thing
// every time.
func freshPostgres(t *testing.T, example string) *url.URL {
	t.Helper()

	u := dsn(t, "MYCEL_TEST_POSTGRES_DSN")
	if u == nil {
		return nil
	}

	name := "example_" + strings.NewReplacer("-", "_", ".", "_").Replace(example)

	admin, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatalf("connecting to Postgres: %v", err)
	}
	defer admin.Close()

	// Anything still connected to the old one would hold it open.
	_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, name)
	if _, err := admin.Exec(`DROP DATABASE IF EXISTS ` + name); err != nil {
		t.Fatalf("dropping %s: %v", name, err)
	}
	if _, err := admin.Exec(`CREATE DATABASE ` + name); err != nil {
		t.Fatalf("creating %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + name)
	})

	fresh := *u
	fresh.Path = "/" + name
	return &fresh
}

func postgresEnvFor(t *testing.T, example string) []string {
	t.Helper()
	u := freshPostgres(t, example)
	if u == nil {
		return nil
	}
	password, _ := u.User.Password()
	return []string{
		"DB_HOST=" + u.Hostname(),
		"DB_PORT=" + u.Port(),
		"DB_USER=" + u.User.Username(),
		"DB_PASSWORD=" + password,
		"DB_NAME=" + strings.TrimPrefix(u.Path, "/"),
		"POSTGRES_HOST=" + u.Hostname(),
		"POSTGRES_PORT=" + u.Port(),
		"POSTGRES_USER=" + u.User.Username(),
		"POSTGRES_PASSWORD=" + password,
		"POSTGRES_DB=" + strings.TrimPrefix(u.Path, "/"),
	}
}

func rabbitEnv(t *testing.T) []string {
	t.Helper()
	u := dsn(t, "MYCEL_TEST_RABBITMQ_URL")
	if u == nil {
		return nil
	}
	password, _ := u.User.Password()
	return []string{
		// The whole address as well as its parts: an example writes whichever
		// its connector reads, and one that asks for RABBITMQ_URL was starting
		// against the default localhost rather than against this broker.
		"RABBITMQ_URL=" + u.String(),
		"RABBITMQ_HOST=" + u.Hostname(),
		// Both spellings, because the examples do not agree on one.
		"RABBIT_HOST=" + u.Hostname(),
		"RABBIT_PORT=" + u.Port(),
		"RABBIT_USER=" + u.User.Username(),
		"RABBIT_PASS=" + password,
		"RABBITMQ_PORT=" + u.Port(),
		"RABBITMQ_USER=" + u.User.Username(),
		"RABBITMQ_PASS=" + password,
		"RABBITMQ_PASSWORD=" + password,
	}
}

// address returns the host and port a variable points at, whatever shape it is
// written in.
//
// A Postgres or AMQP address is a URL and a MySQL one is not:
// user:password@tcp(host:port)/database. Reading the second as a URL gives no
// host at all, so the check for "is anything answering there" always said no —
// and the read-replicas example skipped itself in a run where MySQL was up.
func address(t *testing.T, name string) string {
	t.Helper()

	raw := os.Getenv(name)
	if raw == "" {
		return ""
	}

	if open := strings.Index(raw, "tcp("); open >= 0 {
		rest := raw[open+len("tcp("):]
		if close := strings.Index(rest, ")"); close > 0 {
			return rest[:close]
		}
	}

	// A bare host:port, which is how an SFTP server is named. Checked before
	// the URL parse, which refuses one and stops the test.
	if strings.Count(raw, ":") == 1 && !strings.Contains(raw, "/") {
		return raw
	}

	if u := dsn(t, name); u != nil && u.Host != "" {
		return u.Host
	}
	return ""
}

// freshMySQL makes a database of this example's own, for the same reason the
// Postgres one does: the server remembers which migrations it has applied.
func freshMySQL(t *testing.T, example string) (host, port, user, password, name string) {
	t.Helper()

	dsn := os.Getenv("MYCEL_TEST_MYSQL_DSN")
	if dsn == "" {
		return "", "", "", "", ""
	}

	// user:password@tcp(host:port)/database?...
	at := strings.Index(dsn, "@tcp(")
	if at < 0 {
		t.Fatalf("MYCEL_TEST_MYSQL_DSN is not the shape this expects: %s", dsn)
	}
	credentials := dsn[:at]
	user, password, _ = strings.Cut(credentials, ":")
	rest := dsn[at+len("@tcp("):]
	address, _, _ := strings.Cut(rest, ")")
	host, port, _ = strings.Cut(address, ":")

	// The database written in the address, used as it is if a new one cannot
	// be made.
	existing := ""
	if slash := strings.Index(rest, ")/"); slash >= 0 {
		existing, _, _ = strings.Cut(rest[slash+2:], "?")
	}

	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("connecting to MySQL: %v", err)
	}

	// A database of this example's own, for the same reason the Postgres one
	// has: the server remembers which migrations it has applied. An account
	// that may not create one is not an error — the migrations say IF NOT
	// EXISTS, so the shared database serves, with whatever earlier runs left
	// in it.
	name = "example_" + strings.NewReplacer("-", "_", ".", "_").Replace(example)
	if _, err := admin.Exec("CREATE DATABASE IF NOT EXISTS " + name); err != nil {
		t.Logf("using %s: this account may not create a database (%v)", existing, err)
		_ = admin.Close()
		return host, port, user, password, existing
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name)
		_ = admin.Close()
	})

	return host, port, user, password, name
}

func redisEnv(t *testing.T) []string {
	t.Helper()
	url := os.Getenv("MYCEL_TEST_REDIS_URL")
	if url == "" {
		return nil
	}
	host, port, _ := strings.Cut(strings.TrimPrefix(url, "redis://"), ":")
	return []string{
		"REDIS_URL=" + url,
		"REDIS_HOST=" + host,
		"REDIS_PORT=" + port,
	}
}

func elasticsearchEnv(t *testing.T) []string {
	t.Helper()
	url := os.Getenv("MYCEL_TEST_ELASTICSEARCH_URL")
	if url == "" {
		return nil
	}
	return []string{"ELASTICSEARCH_URL=" + url}
}

func mongoEnv(t *testing.T) []string {
	t.Helper()
	uri := os.Getenv("MYCEL_TEST_MONGO_URI")
	if uri == "" {
		return nil
	}
	return []string{"MONGO_URI=" + uri}
}

func minioEnv(t *testing.T) []string {
	t.Helper()
	endpoint := os.Getenv("MYCEL_TEST_S3_ENDPOINT")
	if endpoint == "" {
		return nil
	}
	return []string{
		"MINIO_ENDPOINT=" + endpoint,
		"MINIO_BUCKET=" + envOr("MYCEL_TEST_S3_BUCKET", "test-bucket"),
		"MINIO_ACCESS_KEY=" + envOr("MYCEL_TEST_S3_ACCESS_KEY", "minioadmin"),
		"MINIO_SECRET_KEY=" + envOr("MYCEL_TEST_S3_SECRET_KEY", "minioadmin"),
	}
}

// downstreamEnv stands a service up in front of the examples that call one.
//
// Several examples write to an HTTP service they do not include — a payment
// API, an inventory API, a notification service. Their addresses used to be
// written into the configuration, which both left a reader unable to point the
// example at their own service and left this harness unable to run the flows
// at all: the interesting part of a saga is what it does when a step answers,
// and nothing was answering. The addresses now come from the environment, so
// what answers here is a server that accepts anything and says so.
//
// It records what it was sent, which is what makes an assertion about a
// compensating call possible rather than only an assertion about a status.
func downstreamEnv(t *testing.T, names ...string) []string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recordCall(t, r.Method+" "+r.URL.Path+" "+string(body))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Enough of an answer for whatever the example asks it: a step that
		// captures an id out of it, an enrichment that reads a price or a
		// stock level. A stand-in service, not a mock of any one of them.
		_, _ = w.Write([]byte(`{"status":"ok","id":"stub-1","reservation_id":"res-1",` +
			`"price":29.99,"currency":"USD","tax_rate":0.21,"available":42,"reserved":3,` +
			// The names the profiles example normalises from, since each
			// profile reads a different upstream's spelling of the same thing.
			`"entity_id":"123","sku":"WIDGET-1",` +
			`"prod_id":"123","product_code":"WIDGET-1","unit_price":"29.99","currency_code":"USD"}`))
	}))
	t.Cleanup(server.Close)

	env := make([]string, 0, len(names))
	for _, name := range names {
		env = append(env, name+"="+server.URL)
	}
	return env
}

// What the stub was asked for, per test, since the tests run one at a time but
// a package-level slice would still carry one example's calls into the next.
var (
	callsMu sync.Mutex
	calls   = map[string][]string{}
)

func recordCall(t *testing.T, call string) {
	callsMu.Lock()
	defer callsMu.Unlock()
	calls[t.Name()] = append(calls[t.Name()], call)
}

func callsFor(t *testing.T) []string {
	callsMu.Lock()
	defer callsMu.Unlock()
	return append([]string(nil), calls[t.Name()]...)
}

// publish puts a message on a queue, for the examples whose flows are started
// by one rather than by a request.
//
// A consumer example cannot be followed with curl, so none of them were being
// run at all — and they are where the primitives worth checking live: the
// deduplication, the sequence guard, the retry with its dead letter queue.
func publish(t *testing.T, queue string, message map[string]interface{}) {
	t.Helper()

	conn, err := amqp.Dial(os.Getenv("MYCEL_TEST_RABBITMQ_URL"))
	if err != nil {
		t.Fatalf("connecting to the broker: %v", err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("opening a channel: %v", err)
	}
	defer channel.Close()

	// Declared here as well as by the service: since 2.0.0 Mycel does not
	// create queues it does not own, so a publisher that assumes one exists
	// has nowhere to put the message.
	if _, err := channel.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		t.Fatalf("declaring %s: %v", queue, err)
	}

	body, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("encoding the message: %v", err)
	}
	err = channel.PublishWithContext(context.Background(), "", queue, false, false,
		amqp.Publishing{ContentType: "application/json", Body: body})
	if err != nil {
		t.Fatalf("publishing to %s: %v", queue, err)
	}
}

// resetQueue empties a queue before a test publishes to it.
//
// A message that failed on an earlier run is still there, being retried, and
// it is delivered before anything published now — so a run is judged on the
// leftovers of the one before it.
func resetQueue(t *testing.T, queue string) {
	t.Helper()

	conn, err := amqp.Dial(os.Getenv("MYCEL_TEST_RABBITMQ_URL"))
	if err != nil {
		t.Fatalf("connecting to the broker: %v", err)
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("opening a channel: %v", err)
	}
	defer channel.Close()

	if _, err := channel.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		t.Fatalf("declaring %s: %v", queue, err)
	}
	if _, err := channel.QueuePurge(queue, false); err != nil {
		t.Fatalf("purging %s: %v", queue, err)
	}
}

// waitForCalls gives a consumer time to do its work, since publishing returns
// as soon as the broker has the message and the flow runs after that.
func waitForCalls(t *testing.T, want int, within time.Duration) []string {
	t.Helper()

	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if made := callsFor(t); len(made) >= want {
			return made
		}
		time.Sleep(200 * time.Millisecond)
	}
	return callsFor(t)
}

func mqttEnv(t *testing.T) []string {
	t.Helper()
	broker := os.Getenv("MYCEL_TEST_MQTT_BROKER")
	if broker == "" {
		return nil
	}
	return []string{"MQTT_BROKER=" + broker}
}

func sftpEnv(t *testing.T) []string {
	t.Helper()
	addr := os.Getenv("MYCEL_TEST_SFTP_ADDR")
	if addr == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("MYCEL_TEST_SFTP_ADDR: %v", err)
	}
	return []string{
		"SFTP_HOST=" + host,
		"SFTP_PORT=" + port,
		"SFTP_USER=" + envOr("MYCEL_TEST_SFTP_USER", "testuser"),
		"SFTP_PASS=" + envOr("MYCEL_TEST_SFTP_PASS", "testpass"),
		// The directory the test server gives that account.
		"SFTP_PATH=" + envOr("MYCEL_TEST_SFTP_PATH", "/upload"),
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

var needsInfrastructure = []infrastructure{
	{
		example: "mq",
		needs:   []string{"MYCEL_TEST_RABBITMQ_URL"},
		env:     rabbitEnv,
	},
	{
		example: "error-handling",
		needs:   []string{"MYCEL_TEST_POSTGRES_DSN", "MYCEL_TEST_RABBITMQ_URL"},
		env: func(t *testing.T) []string {
			return append(postgresEnvFor(t, "error-handling"), rabbitEnv(t)...)
		},
	},
	{
		// A subgraph: it serves GraphQL on its own port and publishes what
		// changes to a queue. The router that would compose it with others is
		// somebody else's process.
		example: "graphql-federation",
		needs:   []string{"MYCEL_TEST_RABBITMQ_URL"},
		env:     rabbitEnv,
	},
	{
		example: "elasticsearch",
		needs:   []string{"MYCEL_TEST_ELASTICSEARCH_URL"},
		env:     elasticsearchEnv,
	},
	{
		example: "batch",
		needs:   []string{"MYCEL_TEST_ELASTICSEARCH_URL"},
		env:     elasticsearchEnv,
	},
	{
		// It writes to a downstream service it does not include, so one is
		// stood up here: without it every flow in the example fails on a
		// refused connection and none of the reusable blocks it exists to
		// demonstrate — the dedupe, the retry, the named transform — is ever
		// exercised.
		example: "reusable-blocks",
		needs:   []string{"MYCEL_TEST_REDIS_URL"},
		env: func(t *testing.T) []string {
			return append(redisEnv(t), downstreamEnv(t, "DOWNSTREAM_URL")...)
		},
	},
	{
		example: "steps",
		needs:   []string{"MYCEL_TEST_REDIS_URL", "MYCEL_TEST_RABBITMQ_URL"},
		env: func(t *testing.T) []string {
			return append(redisEnv(t), rabbitEnv(t)...)
		},
	},
	{
		example: "auth",
		needs:   []string{"MYCEL_TEST_POSTGRES_DSN"},
		env: func(t *testing.T) []string {
			return postgresEnvFor(t, "auth")
		},
	},
	{
		example: "dynamic-api-key",
		needs:   []string{"MYCEL_TEST_POSTGRES_DSN"},
		env: func(t *testing.T) []string {
			return postgresEnvFor(t, "dynamic-api-key")
		},
	},
	{
		example: "mongodb",
		needs:   []string{"MYCEL_TEST_MONGO_URI"},
		env:     mongoEnv,
	},
	{
		example: "s3",
		needs:   []string{"MYCEL_TEST_S3_ENDPOINT"},
		env:     minioEnv,
	},
	{
		example: "read-replicas",
		needs:   []string{"MYCEL_TEST_POSTGRES_DSN", "MYCEL_TEST_MYSQL_DSN"},
		env: func(t *testing.T) []string {
			pg := freshPostgres(t, "read-replicas")
			if pg == nil {
				return nil
			}
			pgPassword, _ := pg.User.Password()

			host, port, user, password, name := freshMySQL(t, "read-replicas")
			if name == "" {
				return nil
			}

			// The replica points at the same server: what is being checked is
			// that a configuration declaring replicas loads and answers, not
			// that somebody set up replication.
			return []string{
				"PG_PRIMARY_HOST=" + pg.Hostname(),
				"PG_PORT=" + pg.Port(),
				"PG_USER=" + pg.User.Username(),
				"PG_PASSWORD=" + pgPassword,
				"PG_DATABASE=" + strings.TrimPrefix(pg.Path, "/"),
				"PG_REPLICA_HOST=" + pg.Hostname(),
				"PG_REPLICA_PORT=" + pg.Port(),
				"MYSQL_PRIMARY_HOST=" + host,
				"MYSQL_PORT=" + port,
				"MYSQL_USER=" + user,
				"MYSQL_PASSWORD=" + password,
				"MYSQL_DATABASE=" + name,
				"MYSQL_REPLICA_HOST=" + host,
				"MYSQL_REPLICA_PORT=" + port,
			}
		},
	},
	{
		// The three services a saga calls — payments, inventory, shipping —
		// are not part of the example, so nothing ever ran it. With something
		// answering, the part worth checking runs: a step that succeeds, and
		// the compensation that undoes it when a later one does not.
		example: "saga",
		env: func(t *testing.T) []string {
			return downstreamEnv(t, "PAYMENTS_URL", "INVENTORY_URL", "NOTIFICATIONS_URL")
		},
		then: func(t *testing.T, calls []string) {
			// A saga that answers 200 having called nothing is a saga that did
			// not run, and the status alone cannot tell the difference.
			for _, want := range []string{"POST /reserve", "POST /charges"} {
				if !calledSomething(calls, want) {
					t.Errorf("the saga answered but never called %s; it made %v", want, calls)
				}
			}
		},
	},
	{
		// Its ship transition notifies a service the example does not include;
		// the harness used to skip that command for exactly that reason.
		example: "state-machine",
		env: func(t *testing.T) []string {
			return downstreamEnv(t, "NOTIFICATIONS_URL")
		},
	},
	{
		// Its two enrichment services are not part of it, so nothing ever ran
		// the flows — which is how `enrich` on a read came to do nothing at
		// all without anyone noticing.
		example: "enrich",
		env: func(t *testing.T) []string {
			return downstreamEnv(t, "PRICING_URL", "INVENTORY_URL")
		},
		then: func(t *testing.T, calls []string) {
			if !calledSomething(calls, "/price") {
				t.Errorf("the flows answered but nothing asked the pricing service; the calls were %v", calls)
			}
		},
	},
	{
		// Its profiles reach two upstreams it does not include; without them
		// every request falls through the whole chain and fails.
		example: "profiles",
		env: func(t *testing.T) []string {
			return downstreamEnv(t, "MAGENTO_URL", "LEGACY_URL")
		},
	},
	{
		// What the example promises, checked: the same content twice is one
		// call downstream, and that call is expensive — avoiding it is the
		// whole point of the dedupe block.
		example: "dedupe",
		needs:   []string{"MYCEL_TEST_RABBITMQ_URL"},
		env: func(t *testing.T) []string {
			// The queue belongs to the upstream publisher in a real
			// deployment; here nobody else is going to create it.
			env := append(rabbitEnv(t), downstreamEnv(t, "MAGENTO_URL")...)
			return append(env, "QUEUE_CREATE_IF_MISSING=true")
		},
		then: func(t *testing.T, _ []string) {
			update := func(job int) map[string]interface{} {
				return map[string]interface{}{
					"metadata": map[string]interface{}{
						"headers": map[string]interface{}{
							"elementType": "product",
							"operation":   "update",
						},
					},
					// The shape the flow projects from: it reads the price
					// line and the attribute collection without a fallback,
					// because a real update carries them.
					"payload": map[string]interface{}{
						"productItemId":    "SKU-1",
						"productItemName":  "Widget",
						"styleAssociation": "STYLE-1",
						"jobId":            job,
						"priceLine": map[string]interface{}{
							"guestUSPrice": 10.0,
							"tradeUSPrice": 8.0,
						},
						"dynamicAttributeCollection": []interface{}{
							map[string]interface{}{"key": "ShowOnWebUSValue", "value": "1"},
						},
					},
				}
			}

			resetQueue(t, "all.in.magento.q")
			publish(t, "all.in.magento.q", update(1))
			// A later job carrying identical content. The sequence guard lets
			// it through — it is not out of order — and the dedupe block is
			// what has to stop it.
			publish(t, "all.in.magento.q", update(2))

			made := waitForCalls(t, 2, 15*time.Second)
			if len(made) == 0 {
				t.Fatal("neither message reached the downstream service")
			}
			if len(made) > 1 {
				t.Errorf("the same content was sent downstream %d times: %v", len(made), made)
			}
		},
	},
	{
		// A consumer with nothing to curl: what it does is push each message
		// to a backend, so a message is published and the backend is expected
		// to hear about it.
		example: "timeout-handling",
		needs:   []string{"MYCEL_TEST_RABBITMQ_URL"},
		env: func(t *testing.T) []string {
			return append(rabbitEnv(t), downstreamEnv(t, "BACKEND_URL")...)
		},
		then: func(t *testing.T, _ []string) {
			resetQueue(t, "items.push")
			publish(t, "items.push", map[string]interface{}{
				"sku":   "WIDGET-1",
				"name":  "Widget",
				"price": 29.99,
			})

			if made := waitForCalls(t, 1, 15*time.Second); len(made) == 0 {
				t.Error("the message was published and the backend was never called")
			}
		},
	},
	{
		// Publishing through the service puts the event on the channel that
		// the other flow is subscribed to, so one request exercises both.
		example: "redis-pubsub",
		needs:   []string{"MYCEL_TEST_REDIS_URL", "MYCEL_TEST_POSTGRES_DSN"},
		env: func(t *testing.T) []string {
			return append(redisEnv(t), postgresEnvFor(t, "redis-pubsub")...)
		},
	},
	{
		example: "sync",
		needs:   []string{"MYCEL_TEST_REDIS_URL", "MYCEL_TEST_POSTGRES_DSN", "MYCEL_TEST_RABBITMQ_URL"},
		env: func(t *testing.T) []string {
			env := append(redisEnv(t), rabbitEnv(t)...)
			return append(env, postgresEnvFor(t, "sync")...)
		},
	},
	{
		// A directory of five services rather than one; the README's request
		// is against the first of them.
		example: "integration/rest-to-rabbit",
		needs:   []string{"MYCEL_TEST_RABBITMQ_URL"},
		env:     rabbitEnv,
	},
	{
		example: "mqtt",
		needs:   []string{"MYCEL_TEST_MQTT_BROKER"},
		env:     mqttEnv,
	},
	{
		// It keeps what it fetches, so it needs the database as well as the
		// file server.
		example: "ftp",
		needs:   []string{"MYCEL_TEST_SFTP_ADDR", "MYCEL_TEST_POSTGRES_DSN"},
		env: func(t *testing.T) []string {
			return append(sftpEnv(t), postgresEnvFor(t, "ftp")...)
		},
	},
	{
		// The round trip is HTTP in, Kafka out, Kafka in, database out, so the
		// README's own two commands cover it — with the wait the consumer
		// needs, which kafka_test.go does rather than sleeping here.
		example: "kafka",
		needs:   []string{"MYCEL_TEST_KAFKA_BROKERS"},
		env: func(t *testing.T) []string {
			return []string{"KAFKA_BROKERS=" + address(t, "MYCEL_TEST_KAFKA_BROKERS")}
		},
	},
	{
		example: "workflows",
		needs:   []string{"MYCEL_TEST_POSTGRES_DSN"},
		env: func(t *testing.T) []string {
			return postgresEnvFor(t, "workflows")
		},
	},
}

func reachable(t *testing.T, name string) bool {
	t.Helper()
	host := address(t, name)
	if host == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", host, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func TestTheExamplesThatNeedInfrastructureWorkWhenFollowed(t *testing.T) {
	if testing.Short() {
		t.Skip("starting services")
	}

	for _, want := range needsInfrastructure {
		t.Run(want.example, func(t *testing.T) {
			for _, name := range want.needs {
				if !reachable(t, name) {
					t.Skipf("%s is not set or nothing answers there", name)
				}
			}

			environment := want.env(t)
			svc := start(t, want.example, environment...)

			commands := svc.commands(t, want.example)
			// A consumer example has nothing to curl: it is started by a
			// message, which `then` publishes.
			if len(commands) == 0 && want.then == nil {
				t.Fatalf("no commands found in the README; this example is not being checked")
			}

			for _, command := range commands {
				if reason, expected := whyNotRun(command); expected {
					t.Logf("not run: %s", reason)
					continue
				}
				status, answer := svc.run(t, command)
				short := command
				if len(short) > 110 {
					short = short[:110] + "…"
				}

				switch {
				case status == 0:
					t.Errorf("no answer at all:\n  %s", short)
				case status >= 500:
					t.Errorf("answered %d:\n  %s\n  %s", status, short, strings.TrimSpace(answer))
				case routeMissing(status, answer):
					t.Errorf("answered %d — the README shows a route the example does not serve:\n  %s", status, short)
				default:
					if refused := refusedInTheBody(answer); refused != "" {
						t.Errorf("answered %d and refused in the body:\n  %s\n  %s", status, short, refused)
					}
				}
			}

			if want.then != nil {
				want.then(t, callsFor(t))
			}
		})
	}
}

// calledSomething reports whether any call matches, since what a stub records
// is a method, a path and a body on one line.
func calledSomething(calls []string, want string) bool {
	for _, call := range calls {
		if strings.Contains(call, want) {
			return true
		}
	}
	return false
}
