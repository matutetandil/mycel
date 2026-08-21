package connectors

import "github.com/matutetandil/mycel/v3/pkg/schema"

// PostgresSchema implements ConnectorSchemaProvider for PostgreSQL.
type PostgresSchema struct{}

func (PostgresSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		// The URL is taken apart before anything is checked, so a name and a
		// user written inside one end up in the same place as the discrete
		// attributes. Either way of saying it is complete; neither is.
		RequiredOneOf: [][]string{{"url", "database"}, {"url", "user"}},
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Database driver — which implementation runs behind this connector", Type: schema.TypeString, Required: true, Values: []string{"postgres", "mysql", "sqlite", "mongodb"}},
			{Name: "url", Doc: "Whole connection URL, e.g. postgres://user:pass@host:5432/db. Discrete fields below are preferred; anything set explicitly wins over the URL.", Type: schema.TypeString},
			{Name: "host", Doc: "Database server host", Type: schema.TypeString},
			{Name: "port", Doc: "Database server port", Type: schema.TypeNumber},
			{Name: "database", Doc: "Database name — or give a url that contains one", Type: schema.TypeString},
			{Name: "user", Doc: "Username — or give a url that contains one", Type: schema.TypeString},
			{Name: "password", Doc: "Password", Type: schema.TypeString},
			{Name: "sslmode", Doc: "SSL mode (disable, require, verify-ca, verify-full)", Type: schema.TypeString},
			// The factory reads both spellings, and the documentation shows
			// this one.
			{Name: "ssl_mode", Doc: "SSL mode; alias of sslmode", Type: schema.TypeString},
			{Name: "use_replicas", Doc: "Enable read replicas", Type: schema.TypeBool},
		},
		Children: []schema.Block{
			poolBlock(),
			{Type: "replicas", Doc: "Read replica configuration", Open: true, Attrs: []schema.Attr{
				{Name: "host", Doc: "Replica host", Type: schema.TypeString, Required: true},
				{Name: "port", Doc: "Replica port", Type: schema.TypeNumber},
				{Name: "weight", Doc: "Load balancing weight", Type: schema.TypeNumber},
				{Name: "max_connections", Doc: "Max connections for this replica", Type: schema.TypeNumber},
			}},
		},
	}
}

func (PostgresSchema) SourceSchema() *schema.Block { return dbSourceSchema() }
func (PostgresSchema) TargetSchema() *schema.Block { return dbTargetSchema() }

// MySQLSchema implements ConnectorSchemaProvider for MySQL.
type MySQLSchema struct{}

func (MySQLSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		// The URL is taken apart before anything is checked, so a name and a
		// user written inside one end up in the same place as the discrete
		// attributes. Either way of saying it is complete; neither is.
		RequiredOneOf: [][]string{{"url", "database"}, {"url", "user"}},
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Database driver — which implementation runs behind this connector", Type: schema.TypeString, Required: true, Values: []string{"postgres", "mysql", "sqlite", "mongodb"}},
			{Name: "url", Doc: "Whole connection URL, e.g. postgres://user:pass@host:5432/db. Discrete fields below are preferred; anything set explicitly wins over the URL.", Type: schema.TypeString},
			{Name: "host", Doc: "Database server host", Type: schema.TypeString},
			{Name: "port", Doc: "Database server port", Type: schema.TypeNumber},
			{Name: "database", Doc: "Database name — or give a url that contains one", Type: schema.TypeString},
			{Name: "user", Doc: "Username — or give a url that contains one", Type: schema.TypeString},
			{Name: "password", Doc: "Password", Type: schema.TypeString},
			{Name: "charset", Doc: "Character set", Type: schema.TypeString},
			{Name: "use_replicas", Doc: "Enable read replicas", Type: schema.TypeBool},
		},
		Children: []schema.Block{
			poolBlock(),
			{Type: "replicas", Doc: "Read replica configuration", Open: true},
		},
	}
}

func (MySQLSchema) SourceSchema() *schema.Block { return dbSourceSchema() }
func (MySQLSchema) TargetSchema() *schema.Block { return dbTargetSchema() }

// SQLiteSchema implements ConnectorSchemaProvider for SQLite.
type SQLiteSchema struct{}

func (SQLiteSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Database driver — which implementation runs behind this connector", Type: schema.TypeString, Required: true, Values: []string{"postgres", "mysql", "sqlite", "mongodb"}},
			{Name: "database", Doc: "Database file path", Type: schema.TypeString, Default: "./data/mycel.db"},
		},
	}
}

func (SQLiteSchema) SourceSchema() *schema.Block { return dbSourceSchema() }
func (SQLiteSchema) TargetSchema() *schema.Block { return dbTargetSchema() }

// MongoDBSchema implements ConnectorSchemaProvider for MongoDB.
type MongoDBSchema struct{}

func (MongoDBSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "driver", Doc: "Database driver — which implementation runs behind this connector", Type: schema.TypeString, Required: true, Values: []string{"postgres", "mysql", "sqlite", "mongodb"}},
			{Name: "uri", Doc: "MongoDB connection URI. Given whole, it is used as written and the options below are ignored", Type: schema.TypeString},
			{Name: "srv", Doc: "Resolve the host through a DNS SRV seed list (mongodb+srv://)", Type: schema.TypeBool},
			{Name: "auth_source", Doc: "Database to authenticate against", Type: schema.TypeString},
			{Name: "replica_set", Doc: "Replica set name", Type: schema.TypeString},
			{Name: "read_concern", Doc: "Read concern level: local, majority, linearizable, available", Type: schema.TypeString},
			{Name: "direct", Doc: "Connect to one server directly rather than discovering the topology", Type: schema.TypeBool},
			{Name: "host", Doc: "MongoDB host", Type: schema.TypeString},
			{Name: "port", Doc: "MongoDB port", Type: schema.TypeNumber},
			{Name: "user", Doc: "Username", Type: schema.TypeString},
			{Name: "password", Doc: "Password", Type: schema.TypeString},
			{Name: "database", Doc: "Database name", Type: schema.TypeString, Required: true},
		},
		Children: []schema.Block{
			{Type: "pool", Doc: "Connection pool settings", Attrs: []schema.Attr{
				{Name: "max", Doc: "Maximum pool size", Type: schema.TypeNumber},
				{Name: "min", Doc: "Minimum pool size", Type: schema.TypeNumber},
				{Name: "connect_timeout", Doc: "Connection timeout in seconds", Type: schema.TypeNumber},
			}},
		},
	}
}

func (MongoDBSchema) SourceSchema() *schema.Block { return dbSourceSchema() }
func (MongoDBSchema) TargetSchema() *schema.Block { return dbTargetSchema() }

// Shared helpers

func poolBlock() schema.Block {
	return schema.Block{
		Type: "pool", Doc: "Connection pool settings",
		Attrs: []schema.Attr{
			{Name: "max", Doc: "Maximum open connections", Type: schema.TypeNumber},
			{Name: "min", Doc: "Minimum idle connections", Type: schema.TypeNumber},
			{Name: "max_lifetime", Doc: "Maximum connection lifetime in seconds", Type: schema.TypeNumber},
		},
	}
}

func dbSourceSchema() *schema.Block {
	return &schema.Block{
		Open: true,
		Attrs: []schema.Attr{
			{Name: "operation", Doc: "REST operation (e.g., GET /users)", Type: schema.TypeString},
		},
	}
}

func dbTargetSchema() *schema.Block {
	return &schema.Block{
		// Open, because a destination carries connector-specific parameters
		// this does not enumerate — so a misspelt attribute is swept up and
		// ignored rather than refused. What can still be said is that a
		// destination has to name what it writes to: without a table or a
		// query there is nothing to write, and `targt = "users"` produced a
		// SQL syntax error at the first request rather than a word at startup.
		Open:          true,
		RequiredOneOf: [][]string{{"target", "query"}},
		Attrs: []schema.Attr{
			{Name: "target", Doc: "Table name", Type: schema.TypeString},
			{Name: "query", Doc: "Raw SQL query", Type: schema.TypeString},
		},
	}
}
