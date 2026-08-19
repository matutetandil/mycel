-- The products the enrichment flows read and write.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS products (
    id         TEXT PRIMARY KEY,
    name       TEXT,
    price      REAL,
    currency   TEXT,
    in_stock   BOOLEAN,
    fetched_at TEXT
);
