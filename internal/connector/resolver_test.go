package connector

import (
	"strings"
	"testing"
)

// Operations a connector gives a name to.
//
// A connector may declare `operation "get_user" { method = "GET" path =
// "/users/{id}" }` and a flow then says `operation = "get_user"` rather than
// spelling out the address. The name is resolved to the form the connector
// speaks — and every connector speaks a different one, which is what this
// does and what nothing checked.

func resolverWith(t *testing.T, config *Config) *OperationResolver {
	t.Helper()
	resolver := NewOperationResolver()
	resolver.Register(config)
	return resolver
}

func TestANameBecomesWhateverTheConnectorSpeaks(t *testing.T) {
	for name, tc := range map[string]struct {
		config *Config
		op     *OperationDef
		want   string
	}{
		"an address, for a REST API": {
			&Config{Name: "api", Type: "rest"},
			&OperationDef{Name: "get_user", Method: "get", Path: "/users/{id}"},
			"GET /users/{id}",
		},
		"a table, for a database": {
			&Config{Name: "db", Type: "database", Driver: "postgres"},
			&OperationDef{Name: "users", Table: "users"},
			"users",
		},
		"a statement, when one was written": {
			&Config{Name: "db", Type: "database", Driver: "postgres"},
			&OperationDef{Name: "recent", Query: "SELECT * FROM orders WHERE created_at > now() - interval '1 day'"},
			"SELECT * FROM orders WHERE created_at > now() - interval '1 day'",
		},
		"a field on a root type, for GraphQL": {
			&Config{Name: "gql", Type: "graphql"},
			&OperationDef{Name: "users", OperationType: "query", Field: "users"},
			"Query.users",
		},
		"a service and a method, for gRPC": {
			&Config{Name: "grpc", Type: "grpc"},
			&OperationDef{Name: "get_user", Service: "UserService", RPC: "GetUser"},
			"UserService/GetUser",
		},
		"an exchange and a routing key, for RabbitMQ": {
			&Config{Name: "rabbit", Type: "mq", Driver: "rabbitmq"},
			&OperationDef{Name: "user_created", Exchange: "events", RoutingKey: "user.created"},
			"events.user.created",
		},
		"a queue, when there is no exchange": {
			&Config{Name: "rabbit", Type: "mq", Driver: "rabbitmq"},
			&OperationDef{Name: "inbox", Queue: "user_events"},
			"user_events",
		},
		"a topic, for Kafka": {
			&Config{Name: "kafka", Type: "mq", Driver: "kafka"},
			&OperationDef{Name: "orders", Queue: "orders"},
			"orders",
		},
		"an action, for TCP": {
			&Config{Name: "tcp", Type: "tcp"},
			&OperationDef{Name: "get_user", Action: "get_user"},
			"get_user",
		},
		"a path pattern, for files": {
			&Config{Name: "files", Type: "file"},
			&OperationDef{Name: "invoices", PathPattern: "/data/invoices/*.csv"},
			"/data/invoices/*.csv",
		},
		"a key pattern, for a cache": {
			&Config{Name: "cache", Type: "cache"},
			&OperationDef{Name: "user", KeyPattern: "user:{id}"},
			"user:{id}",
		},
		"a command, for exec": {
			&Config{Name: "shell", Type: "exec"},
			&OperationDef{Name: "backup", Command: "/usr/local/bin/backup.sh"},
			"/usr/local/bin/backup.sh",
		},
	} {
		t.Run(name, func(t *testing.T) {
			tc.config.Operations = []*OperationDef{tc.op}
			resolved, err := resolverWith(t, tc.config).Resolve(tc.config.Name, tc.op.Name)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if resolved.Inline != tc.want {
				t.Errorf("resolved to %q, want %q", resolved.Inline, tc.want)
			}
			if resolved.Operation == nil || resolved.Operation.Name != tc.op.Name {
				t.Errorf("the operation itself did not come back: %+v", resolved.Operation)
			}
		})
	}
}

func TestAnOperationMissingWhatItsConnectorNeeds(t *testing.T) {
	// Each connector needs different parts, and a half-written operation has
	// to be reported by name — the flow that uses it says only "get_user".
	for name, tc := range map[string]struct {
		config *Config
		op     *OperationDef
	}{
		"REST with no path":         {&Config{Name: "api", Type: "rest"}, &OperationDef{Name: "get_user", Method: "GET"}},
		"a database with neither":   {&Config{Name: "db", Type: "database"}, &OperationDef{Name: "users"}},
		"GraphQL with no field":     {&Config{Name: "gql", Type: "graphql"}, &OperationDef{Name: "users", OperationType: "query"}},
		"gRPC with no method":       {&Config{Name: "grpc", Type: "grpc"}, &OperationDef{Name: "get", Service: "UserService"}},
		"a queue with nothing":      {&Config{Name: "rabbit", Type: "mq", Driver: "rabbitmq"}, &OperationDef{Name: "inbox"}},
		"TCP with no action":        {&Config{Name: "tcp", Type: "tcp"}, &OperationDef{Name: "get"}},
		"a file with no pattern":    {&Config{Name: "files", Type: "file"}, &OperationDef{Name: "invoices"}},
		"an object with no pattern": {&Config{Name: "bucket", Type: "s3"}, &OperationDef{Name: "invoices"}},
		"a cache with no key":       {&Config{Name: "cache", Type: "cache"}, &OperationDef{Name: "user"}},
		"exec with no command":      {&Config{Name: "shell", Type: "exec"}, &OperationDef{Name: "backup"}},
	} {
		t.Run(name, func(t *testing.T) {
			tc.config.Operations = []*OperationDef{tc.op}
			_, err := resolverWith(t, tc.config).Resolve(tc.config.Name, tc.op.Name)
			if err == nil {
				t.Fatal("a half-written operation resolved to something")
			}
			if !strings.Contains(err.Error(), tc.op.Name) {
				t.Errorf("the error does not name the operation: %v", err)
			}
		})
	}
}

func TestAConnectorTypeTheResolverDoesNotKnow(t *testing.T) {
	// A plugin's connector type. Rather than refuse, it reads what the
	// operation looks like: an address makes it a REST call, a table makes it
	// a database one, and anything else is passed through under its own name.
	for name, tc := range map[string]struct {
		op   *OperationDef
		want string
	}{
		"it looks like a REST call":   {&OperationDef{Name: "get_user", Method: "GET", Path: "/users"}, "GET /users"},
		"it looks like a query":       {&OperationDef{Name: "users", Table: "users"}, "users"},
		"it looks like nothing known": {&OperationDef{Name: "sync_contacts"}, "sync_contacts"},
	} {
		t.Run(name, func(t *testing.T) {
			config := &Config{Name: "sf", Type: "salesforce", Operations: []*OperationDef{tc.op}}
			resolved, err := resolverWith(t, config).Resolve("sf", tc.op.Name)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if resolved.Inline != tc.want {
				t.Errorf("resolved to %q, want %q", resolved.Inline, tc.want)
			}
		})
	}
}

func TestAskingForAnOperationNobodyDeclared(t *testing.T) {
	resolver := resolverWith(t, &Config{
		Name: "api", Type: "rest",
		Operations: []*OperationDef{{Name: "get_user", Method: "GET", Path: "/users/{id}"}},
	})

	// The mistake is a typo in a flow, so the error has to name both the
	// operation and the connector it was looked for in.
	_, err := resolver.Resolve("api", "get_users")
	if err == nil {
		t.Fatal("an operation nobody declared resolved to something")
	}
	if !strings.Contains(err.Error(), "get_users") || !strings.Contains(err.Error(), "api") {
		t.Errorf("the error does not say what was not found where: %v", err)
	}

	// A connector with no operations at all.
	if _, err := resolver.Resolve("db", "get_user"); err == nil {
		t.Error("an operation was resolved on a connector that declares none")
	}

	// And an empty name, which is what an unset attribute looks like.
	if _, err := resolver.Resolve("api", "   "); err == nil {
		t.Error("an operation with no name resolved to something")
	}
}

func TestTheDefaultsAnOperationCarries(t *testing.T) {
	// Parameters with defaults are applied when the flow does not give one,
	// which is what makes `operation = "list_users"` mean page one of twenty
	// rather than an unbounded query.
	limit := 20
	config := &Config{
		Name: "api", Type: "rest",
		Operations: []*OperationDef{{
			Name: "list_users", Method: "GET", Path: "/users",
			Params: []*ParamDef{
				{Name: "id", Required: true},
				{Name: "limit", Default: limit},
				{Name: "cursor"},
			},
		}},
	}

	resolved, err := resolverWith(t, config).Resolve("api", "list_users")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.Params["limit"] != limit {
		t.Errorf("defaults = %v, want the limit applied", resolved.Params)
	}
	// A parameter with no default is absent rather than present and empty:
	// an empty cursor is a different request from no cursor.
	if _, present := resolved.Params["cursor"]; present {
		t.Errorf("a parameter with no default was sent anyway: %v", resolved.Params)
	}

	op := config.GetOperation("list_users")
	if op == nil {
		t.Fatal("the connector cannot find the operation it declares")
	}
	if !config.HasOperation("list_users") || config.HasOperation("list_orders") {
		t.Error("the connector disagrees with itself about which operations exist")
	}
	if len(config.ListOperations()) != 1 {
		t.Errorf("operations = %v", config.ListOperations())
	}

	if op.GetParam("limit") == nil || !op.HasParam("id") || op.HasParam("nothing") {
		t.Errorf("params = %+v", op.Params)
	}
	required := op.RequiredParams()
	if len(required) != 1 || required[0].Name != "id" {
		t.Errorf("required = %+v, want the one that is", required)
	}
}

func TestWhatAConnectorSaysItCanDo(t *testing.T) {
	// Read by completions and by the exported documentation.
	resolver := resolverWith(t, &Config{
		Name: "api", Type: "rest",
		Operations: []*OperationDef{
			{Name: "get_user", Method: "GET", Path: "/users/{id}"},
			{Name: "create_user", Method: "POST", Path: "/users"},
		},
	})

	if !resolver.HasOperation("api", "get_user") {
		t.Error("an operation that was declared is not there")
	}
	if resolver.HasOperation("api", "delete_user") {
		t.Error("an operation nobody declared is there")
	}
	if resolver.GetOperation("db", "anything") != nil {
		t.Error("an operation came back for a connector nobody registered")
	}

	if got := resolver.ListOperations("api"); len(got) != 2 {
		t.Errorf("operations = %v, want both", got)
	}
	if got := resolver.ListOperations("db"); got != nil {
		t.Errorf("a connector nobody registered lists %v", got)
	}
}
