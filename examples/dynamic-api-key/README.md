# Dynamic API Key Validation Example

This example demonstrates how to validate API keys dynamically against a database instead of using static configuration.

## Use Case

When you need to:
- Store API keys in a database for management
- Associate keys with users, tenants, or metadata
- Support key expiration and revocation
- Track API key usage and permissions

## Configuration

### Database Schema

```sql
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash VARCHAR(64) NOT NULL UNIQUE,  -- SHA256 hash of the key
    user_id UUID NOT NULL REFERENCES users(id),
    metadata JSONB DEFAULT '{}',
    active BOOLEAN DEFAULT true,
    expires_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_api_keys_hash ON api_keys(key_hash) WHERE active = true;
```

### Dynamic Validation Block

```hcl
connector "api" {
  type = "rest"
  port = 8080

  auth {
    type = "api_key"

    api_key {
      header = "X-API-Key"

      # Dynamic validation against database
      validate {
        connector = "connector.keys_db"
        query     = "SELECT user_id, metadata FROM api_keys WHERE key_hash = :key AND active = true"
      }
    }
  }
}
```

## How It Works

1. Client sends request with `X-API-Key` header
2. Mycel extracts the key and queries the database
3. If found and active, the `user_id` and `metadata` are added to auth context
4. Flows can access this via `auth.user_id` and `auth.claims`

## Auth Context

After validation, flows have access to:

```hcl
transform {
  user_id   = "auth.user_id"        # From query result
  tenant_id = "auth.claims.tenant_id"  # From metadata JSON
  role      = "auth.claims.role"
}
```

## Running

```bash
# Set environment variables
export DB_HOST=localhost
export DB_USER=mycel
export DB_PASSWORD=secret
export DB_NAME=api_keys

# Start the service
mycel start --config ./examples/dynamic-api-key

# Test with API key
curl -H "X-API-Key: your-api-key-here" http://localhost:8080/me
```

## Comparison: Static vs Dynamic

| Feature | Static Keys | Dynamic Keys |
|---------|-------------|--------------|
| Configuration | In HCL file | In database |
| Key rotation | Requires restart | Immediate |
| User association | Not supported | Full support |
| Expiration | Not supported | Supported |
| Metadata/Claims | Not supported | Full support |
| Audit trail | Not supported | Can be added |

## Security Notes

- Store only key hashes, never plain text keys
- Use SHA256 or bcrypt for hashing
- Add rate limiting to prevent brute force
- Consider key prefix for identification (e.g., `mk_live_xxx`)
