package examples

import (
	"database/sql"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
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
		"RABBITMQ_HOST=" + u.Hostname(),
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
			if len(commands) == 0 {
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
		})
	}
}
