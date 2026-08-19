-- The users behind the GraphQL schema.
--
-- Applied with `mycel migrate --config .` before starting; nothing creates it
-- otherwise, and every query answers 500 without it.

CREATE TABLE IF NOT EXISTS users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    email      TEXT,
    created_at TEXT
);
