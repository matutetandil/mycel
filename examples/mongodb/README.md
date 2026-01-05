# MongoDB Example

This example demonstrates MongoDB NoSQL operations.

## Features

- Full CRUD operations
- MongoDB query operators (`$regex`, `$gte`, `$or`, etc.)
- Bulk updates with `UPDATE_MANY`
- ObjectID handling
- Connection pooling

## Files

- `config.hcl` - MongoDB connector configuration
- `flows.hcl` - CRUD and query flows

## Environment Variables

```bash
export MONGO_URI="mongodb://localhost:27017"
```

## Usage

```bash
# Start the service
mycel start --config ./examples/mongodb

# Create a user
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"name":"John","email":"john@example.com"}'

# List users
curl http://localhost:3000/users

# Get user by ID
curl http://localhost:3000/users/{id}

# Update user
curl -X PUT http://localhost:3000/users/{id} \
  -H "Content-Type: application/json" \
  -d '{"name":"John Updated","email":"john.new@example.com"}'

# Delete user
curl -X DELETE http://localhost:3000/users/{id}

# Search users
curl "http://localhost:3000/users/search?q=john"

# Get active users
curl http://localhost:3000/users/active
```

## Configuration

```hcl
connector "mongo" {
  type     = "database"
  driver   = "mongodb"
  uri      = env("MONGO_URI")
  database = "myapp"

  pool {
    max             = 100
    min             = 10
    connect_timeout = 30
  }
}
```

## Query Filters

MongoDB query filters use standard MongoDB operators:

```hcl
to {
  connector    = "mongo"
  target       = "users"
  query_filter = {
    status = "active"
    age    = { "$gte" = 18 }
  }
}
```

## Update Operations

```hcl
to {
  connector    = "mongo"
  target       = "users"
  query_filter = { "_id" = ":id" }
  update = {
    "$set" = {
      status     = "input.status"
      updated_at = "now()"
    }
  }
  operation = "UPDATE_ONE"
}
```

## Operations

| Operation | Description |
|-----------|-------------|
| (default) | Find documents |
| `INSERT_ONE` | Insert single document |
| `INSERT_MANY` | Insert multiple documents |
| `UPDATE_ONE` | Update single document |
| `UPDATE_MANY` | Update multiple documents |
| `DELETE_ONE` | Delete single document |
| `DELETE_MANY` | Delete multiple documents |
| `REPLACE_ONE` | Replace entire document |

## MongoDB Operators

Common operators supported in `query_filter`:

| Operator | Description |
|----------|-------------|
| `$eq` | Equal |
| `$ne` | Not equal |
| `$gt`, `$gte` | Greater than (or equal) |
| `$lt`, `$lte` | Less than (or equal) |
| `$in`, `$nin` | In / not in array |
| `$regex` | Regular expression |
| `$or`, `$and` | Logical operators |
| `$exists` | Field exists |

## Verify It Works

### 1. Start MongoDB

```bash
docker run -d --name mongodb -p 27017:27017 mongo:latest
```

### 2. Start the service

```bash
export MONGO_URI="mongodb://localhost:27017"
mycel start --config ./examples/mongodb
```

You should see:
```
INFO  Starting service: mongodb-example
INFO  Loaded 2 connectors: api, mongo
INFO    mongo: connected to mongodb://localhost:27017/myapp
INFO  REST server listening on :3000
```

### 3. Create a user

```bash
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"name":"John Doe","email":"john@example.com","status":"active"}'
```

Expected response:
```json
{
  "_id": "507f1f77bcf86cd799439011",
  "name": "John Doe",
  "email": "john@example.com",
  "status": "active"
}
```

### 4. List users

```bash
curl http://localhost:3000/users
```

Expected response:
```json
[{"_id": "507f...", "name": "John Doe", "email": "john@example.com"}]
```

### 5. Search with query

```bash
curl "http://localhost:3000/users/search?q=john"
```

Expected response (uses `$regex`):
```json
[{"_id": "507f...", "name": "John Doe"}]
```

### 6. Verify in MongoDB shell

```bash
docker exec -it mongodb mongosh myapp --eval "db.users.find()"
```

### Common Issues

**"Connection refused"**

Ensure MongoDB is running:
```bash
docker ps | grep mongo
```

**"Authentication failed"**

If MongoDB requires auth:
```bash
export MONGO_URI="mongodb://user:pass@localhost:27017"
```

**"Invalid ObjectId"**

MongoDB IDs are 24-character hex strings. Ensure you're using the `_id` from the create response.
