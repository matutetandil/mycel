-- The products the validated flows write, alongside the users.

CREATE TABLE IF NOT EXISTS products (
    id         TEXT PRIMARY KEY,
    name       TEXT,
    slug       TEXT,
    price      REAL,
    created_at TEXT
);
