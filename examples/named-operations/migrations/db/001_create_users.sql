-- The users table the named operations read and write.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    email      TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_at TEXT,
    updated_at TEXT
);

INSERT OR IGNORE INTO users (id, email, name, created_at) VALUES
    ('1', 'john@example.com', 'John Doe', '2026-01-01T00:00:00Z'),
    ('2', 'jane@example.com', 'Jane Smith', '2026-01-02T00:00:00Z');
