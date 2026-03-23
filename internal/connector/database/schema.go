package database

import "github.com/matutetandil/mycel/pkg/schema"

// PostgresSchema implements ConnectorSchemaProvider for PostgreSQL.
type PostgresSchema struct{}

func (PostgresSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "host", Doc: "Database server host", Type: schema.TypeString},
			{Name: "port", Doc: "Database server port", Type: schema.TypeNumber},
			{Name: "database", Doc: "Database name", Type: schema.TypeString, Required: true},
			{Name: "user", Doc: "Username", Type: schema.TypeString},
			{Name: "password", Doc: "Password", Type: schema.TypeString},
			{Name: "sslmode", Doc: "SSL mode (disable, require, verify-ca, verify-full)", Type: schema.TypeString},
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

func (PostgresSchema) SourceSchema() *schema.Block  { return dbSourceSchema() }
func (PostgresSchema) TargetSchema() *schema.Block   { return dbTargetSchema() }

// MySQLSchema implements ConnectorSchemaProvider for MySQL.
type MySQLSchema struct{}

func (MySQLSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "host", Doc: "Database server host", Type: schema.TypeString},
			{Name: "port", Doc: "Database server port", Type: schema.TypeNumber},
			{Name: "database", Doc: "Database name", Type: schema.TypeString, Required: true},
			{Name: "user", Doc: "Username", Type: schema.TypeString},
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

func (MySQLSchema) SourceSchema() *schema.Block  { return dbSourceSchema() }
func (MySQLSchema) TargetSchema() *schema.Block   { return dbTargetSchema() }

// SQLiteSchema implements ConnectorSchemaProvider for SQLite.
type SQLiteSchema struct{}

func (SQLiteSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "database", Doc: "Database file path", Type: schema.TypeString, Required: true},
		},
	}
}

func (SQLiteSchema) SourceSchema() *schema.Block  { return dbSourceSchema() }
func (SQLiteSchema) TargetSchema() *schema.Block   { return dbTargetSchema() }

// MongoDBSchema implements ConnectorSchemaProvider for MongoDB.
type MongoDBSchema struct{}

func (MongoDBSchema) ConnectorSchema() schema.Block {
	return schema.Block{
		Attrs: []schema.Attr{
			{Name: "uri", Doc: "MongoDB connection URI", Type: schema.TypeString},
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

func (MongoDBSchema) SourceSchema() *schema.Block  { return dbSourceSchema() }
func (MongoDBSchema) TargetSchema() *schema.Block   { return dbTargetSchema() }

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
		Open: true,
		Attrs: []schema.Attr{
			{Name: "target", Doc: "Table name", Type: schema.TypeString},
			{Name: "query", Doc: "Raw SQL query", Type: schema.TypeString},
		},
	}
}
