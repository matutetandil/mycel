// Package mongodb provides a MongoDB database connector.
package mongodb

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/mycel-labs/mycel/internal/connector"
)

// Connector implements a MongoDB database connector.
type Connector struct {
	name     string
	uri      string
	database string
	client   *mongo.Client
	db       *mongo.Database

	// Connection settings
	connectTimeout time.Duration
	maxPoolSize    uint64
	minPoolSize    uint64
}

// New creates a new MongoDB connector.
func New(name, uri, database string) *Connector {
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}

	return &Connector{
		name:           name,
		uri:            uri,
		database:       database,
		connectTimeout: 10 * time.Second,
		maxPoolSize:    100,
		minPoolSize:    5,
	}
}

// SetPoolConfig sets connection pool configuration.
func (c *Connector) SetPoolConfig(maxPool, minPool uint64, connectTimeout time.Duration) {
	if maxPool > 0 {
		c.maxPoolSize = maxPool
	}
	if minPool > 0 {
		c.minPoolSize = minPool
	}
	if connectTimeout > 0 {
		c.connectTimeout = connectTimeout
	}
}

// Name returns the connector name.
func (c *Connector) Name() string {
	return c.name
}

// Type returns the connector type.
func (c *Connector) Type() string {
	return "database"
}

// Connect establishes the database connection.
func (c *Connector) Connect(ctx context.Context) error {
	clientOptions := options.Client().
		ApplyURI(c.uri).
		SetMaxPoolSize(c.maxPoolSize).
		SetMinPoolSize(c.minPoolSize).
		SetConnectTimeout(c.connectTimeout)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to mongodb: %w", err)
	}

	// Verify connection
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return fmt.Errorf("failed to ping mongodb: %w", err)
	}

	c.client = client
	c.db = client.Database(c.database)
	return nil
}

// Close closes the database connection.
func (c *Connector) Close(ctx context.Context) error {
	if c.client != nil {
		return c.client.Disconnect(ctx)
	}
	return nil
}

// Health checks if the connector is healthy.
func (c *Connector) Health(ctx context.Context) error {
	if c.client == nil {
		return fmt.Errorf("database not connected")
	}
	return c.client.Ping(ctx, nil)
}

// Read executes a query and returns results (implements connector.Reader).
func (c *Connector) Read(ctx context.Context, query connector.Query) (*connector.Result, error) {
	if c.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	collection := c.db.Collection(query.Target)

	// Build the filter
	filter := c.buildFilter(query)

	// Build find options
	findOptions := options.Find()

	// Add projection (fields)
	if len(query.Fields) > 0 {
		projection := bson.M{}
		for _, field := range query.Fields {
			projection[field] = 1
		}
		findOptions.SetProjection(projection)
	}

	// Add sorting
	if len(query.OrderBy) > 0 {
		sort := bson.D{}
		for _, o := range query.OrderBy {
			order := 1
			if o.Desc {
				order = -1
			}
			sort = append(sort, bson.E{Key: o.Field, Value: order})
		}
		findOptions.SetSort(sort)
	}

	// Add pagination
	if query.Pagination != nil {
		if query.Pagination.Limit > 0 {
			findOptions.SetLimit(int64(query.Pagination.Limit))
		}
		if query.Pagination.Offset > 0 {
			findOptions.SetSkip(int64(query.Pagination.Offset))
		}
	}

	// Execute query
	cursor, err := collection.Find(ctx, filter, findOptions)
	if err != nil {
		return nil, fmt.Errorf("find failed: %w", err)
	}
	defer cursor.Close(ctx)

	// Collect results
	var results []map[string]interface{}
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode document: %w", err)
		}

		// Convert BSON to regular map and handle ObjectID
		row := c.convertBSONToMap(doc)
		results = append(results, row)
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	return &connector.Result{
		Rows:     results,
		Affected: int64(len(results)),
	}, nil
}

// Write executes an insert, update, or delete operation (implements connector.Writer).
func (c *Connector) Write(ctx context.Context, data *connector.Data) (*connector.Result, error) {
	if c.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	collection := c.db.Collection(data.Target)

	switch data.Operation {
	case "INSERT", "INSERT_ONE":
		return c.insertOne(ctx, collection, data)
	case "INSERT_MANY":
		return c.insertMany(ctx, collection, data)
	case "UPDATE", "UPDATE_ONE":
		return c.updateOne(ctx, collection, data)
	case "UPDATE_MANY":
		return c.updateMany(ctx, collection, data)
	case "DELETE", "DELETE_ONE":
		return c.deleteOne(ctx, collection, data)
	case "DELETE_MANY":
		return c.deleteMany(ctx, collection, data)
	case "REPLACE", "REPLACE_ONE":
		return c.replaceOne(ctx, collection, data)
	default:
		return nil, fmt.Errorf("unsupported operation: %s", data.Operation)
	}
}

// buildFilter builds a MongoDB filter from the query.
func (c *Connector) buildFilter(query connector.Query) bson.M {
	// Use RawQuery if provided (highest priority)
	if query.RawQuery != nil && len(query.RawQuery) > 0 {
		return c.convertToBSON(query.RawQuery)
	}

	// Use Filters
	if query.Filters != nil && len(query.Filters) > 0 {
		return c.convertToBSON(query.Filters)
	}

	// Empty filter (match all)
	return bson.M{}
}

// convertToBSON converts a map to BSON, handling special cases like ObjectID.
func (c *Connector) convertToBSON(m map[string]interface{}) bson.M {
	result := bson.M{}
	for k, v := range m {
		// Handle _id field - try to convert to ObjectID
		if k == "_id" || k == "id" {
			if strID, ok := v.(string); ok {
				if oid, err := primitive.ObjectIDFromHex(strID); err == nil {
					if k == "id" {
						result["_id"] = oid
					} else {
						result[k] = oid
					}
					continue
				}
			}
		}

		// Handle nested maps
		if nested, ok := v.(map[string]interface{}); ok {
			result[k] = c.convertToBSON(nested)
		} else {
			result[k] = v
		}
	}
	return result
}

// convertBSONToMap converts a BSON document to a regular map.
func (c *Connector) convertBSONToMap(doc bson.M) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range doc {
		switch val := v.(type) {
		case primitive.ObjectID:
			result[k] = val.Hex()
		case primitive.DateTime:
			result[k] = val.Time().Format(time.RFC3339)
		case bson.M:
			result[k] = c.convertBSONToMap(val)
		case bson.A:
			result[k] = c.convertBSONArray(val)
		default:
			result[k] = v
		}
	}
	return result
}

// convertBSONArray converts a BSON array to a regular slice.
func (c *Connector) convertBSONArray(arr bson.A) []interface{} {
	result := make([]interface{}, len(arr))
	for i, v := range arr {
		switch val := v.(type) {
		case primitive.ObjectID:
			result[i] = val.Hex()
		case primitive.DateTime:
			result[i] = val.Time().Format(time.RFC3339)
		case bson.M:
			result[i] = c.convertBSONToMap(val)
		case bson.A:
			result[i] = c.convertBSONArray(val)
		default:
			result[i] = v
		}
	}
	return result
}

// insertOne inserts a single document.
func (c *Connector) insertOne(ctx context.Context, coll *mongo.Collection, data *connector.Data) (*connector.Result, error) {
	doc := c.convertToBSON(data.Payload)

	result, err := coll.InsertOne(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("insert failed: %w", err)
	}

	// Convert inserted ID
	var lastID interface{}
	if oid, ok := result.InsertedID.(primitive.ObjectID); ok {
		lastID = oid.Hex()
	} else {
		lastID = result.InsertedID
	}

	return &connector.Result{
		Affected: 1,
		LastID:   lastID,
	}, nil
}

// insertMany inserts multiple documents.
func (c *Connector) insertMany(ctx context.Context, coll *mongo.Collection, data *connector.Data) (*connector.Result, error) {
	// Expect documents in Params["documents"]
	docs, ok := data.Params["documents"].([]interface{})
	if !ok {
		// Fallback: insert single document from Payload
		return c.insertOne(ctx, coll, data)
	}

	// Convert documents to BSON
	bsonDocs := make([]interface{}, len(docs))
	for i, doc := range docs {
		if m, ok := doc.(map[string]interface{}); ok {
			bsonDocs[i] = c.convertToBSON(m)
		} else {
			bsonDocs[i] = doc
		}
	}

	result, err := coll.InsertMany(ctx, bsonDocs)
	if err != nil {
		return nil, fmt.Errorf("insert many failed: %w", err)
	}

	return &connector.Result{
		Affected: int64(len(result.InsertedIDs)),
	}, nil
}

// updateOne updates a single document.
func (c *Connector) updateOne(ctx context.Context, coll *mongo.Collection, data *connector.Data) (*connector.Result, error) {
	filter := c.convertToBSON(data.Filters)

	// Build update document
	var update bson.M
	if data.Update != nil && len(data.Update) > 0 {
		update = c.convertToBSON(data.Update)
	} else {
		// Default to $set with payload
		update = bson.M{"$set": c.convertToBSON(data.Payload)}
	}

	// Check for upsert option
	opts := options.Update()
	if upsert, ok := data.Params["upsert"].(bool); ok && upsert {
		opts.SetUpsert(true)
	}

	result, err := coll.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return nil, fmt.Errorf("update failed: %w", err)
	}

	return &connector.Result{
		Affected: result.ModifiedCount,
		Metadata: map[string]interface{}{
			"matched":  result.MatchedCount,
			"modified": result.ModifiedCount,
			"upserted": result.UpsertedCount,
		},
	}, nil
}

// updateMany updates multiple documents.
func (c *Connector) updateMany(ctx context.Context, coll *mongo.Collection, data *connector.Data) (*connector.Result, error) {
	filter := c.convertToBSON(data.Filters)

	// Build update document
	var update bson.M
	if data.Update != nil && len(data.Update) > 0 {
		update = c.convertToBSON(data.Update)
	} else {
		update = bson.M{"$set": c.convertToBSON(data.Payload)}
	}

	result, err := coll.UpdateMany(ctx, filter, update)
	if err != nil {
		return nil, fmt.Errorf("update many failed: %w", err)
	}

	return &connector.Result{
		Affected: result.ModifiedCount,
		Metadata: map[string]interface{}{
			"matched":  result.MatchedCount,
			"modified": result.ModifiedCount,
		},
	}, nil
}

// deleteOne deletes a single document.
func (c *Connector) deleteOne(ctx context.Context, coll *mongo.Collection, data *connector.Data) (*connector.Result, error) {
	filter := c.convertToBSON(data.Filters)

	result, err := coll.DeleteOne(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("delete failed: %w", err)
	}

	return &connector.Result{
		Affected: result.DeletedCount,
	}, nil
}

// deleteMany deletes multiple documents.
func (c *Connector) deleteMany(ctx context.Context, coll *mongo.Collection, data *connector.Data) (*connector.Result, error) {
	filter := c.convertToBSON(data.Filters)

	result, err := coll.DeleteMany(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("delete many failed: %w", err)
	}

	return &connector.Result{
		Affected: result.DeletedCount,
	}, nil
}

// replaceOne replaces a single document.
func (c *Connector) replaceOne(ctx context.Context, coll *mongo.Collection, data *connector.Data) (*connector.Result, error) {
	filter := c.convertToBSON(data.Filters)
	replacement := c.convertToBSON(data.Payload)

	opts := options.Replace()
	if upsert, ok := data.Params["upsert"].(bool); ok && upsert {
		opts.SetUpsert(true)
	}

	result, err := coll.ReplaceOne(ctx, filter, replacement, opts)
	if err != nil {
		return nil, fmt.Errorf("replace failed: %w", err)
	}

	return &connector.Result{
		Affected: result.ModifiedCount,
		Metadata: map[string]interface{}{
			"matched":  result.MatchedCount,
			"modified": result.ModifiedCount,
			"upserted": result.UpsertedCount,
		},
	}, nil
}

// Aggregate executes an aggregation pipeline.
func (c *Connector) Aggregate(ctx context.Context, collection string, pipeline []bson.M) ([]map[string]interface{}, error) {
	if c.db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	coll := c.db.Collection(collection)
	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("aggregate failed: %w", err)
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode document: %w", err)
		}
		results = append(results, c.convertBSONToMap(doc))
	}

	return results, cursor.Err()
}
