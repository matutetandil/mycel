-- The table the rate-limited flows read and write.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS items (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TEXT
);
