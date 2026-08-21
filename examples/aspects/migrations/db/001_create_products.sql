-- The table the flows read and write.
--
-- Applied with `mycel migrate --config . --connector db`.

CREATE TABLE IF NOT EXISTS products (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    price      REAL,
    created_at TEXT,
    updated_at TEXT
);
