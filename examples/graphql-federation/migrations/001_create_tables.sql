-- The two subgraphs' tables. Products and reviews are separate services in a
-- federation; here they share one database file for the sake of the example.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS products (
    id          TEXT PRIMARY KEY,
    name        TEXT,
    description TEXT,
    price       REAL,
    category    TEXT,
    in_stock    BOOLEAN,
    created_at  TEXT
);

CREATE TABLE IF NOT EXISTS reviews (
    id         TEXT PRIMARY KEY,
    product_id TEXT,
    author     TEXT,
    comment    TEXT,
    rating     INTEGER,
    created_at TEXT
);
