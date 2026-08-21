-- The users table the TCP and REST flows share.
--
-- Applied with `mycel migrate --config .` before starting; without it the
-- service starts and answers 500 to every request.

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL
);

-- Sample data
INSERT OR IGNORE INTO users (id, email, name, created_at) VALUES
    ('user-001', 'john@example.com', 'John Doe', '2025-01-01T00:00:00Z'),
    ('user-002', 'jane@example.com', 'Jane Smith', '2025-01-01T00:00:00Z');
