-- The users the flows read and write, alongside the products.

CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    name       TEXT,
    email      TEXT,
    created_at TEXT
);
