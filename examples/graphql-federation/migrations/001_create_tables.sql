-- The two subgraphs' tables. Products and reviews are separate services in a
-- federation; here they share one database file for the sake of the example.
--
-- The columns are the ones the flows write and the types declare.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS products (
    sku         TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    price       REAL,
    description TEXT,
    category    TEXT,
    in_stock    BOOLEAN DEFAULT true,
    created_at  TEXT,
    updated_at  TEXT
);

CREATE TABLE IF NOT EXISTS reviews (
    id          TEXT PRIMARY KEY,
    product_sku TEXT,
    rating      INTEGER,
    comment     TEXT,
    author      TEXT,
    created_at  TEXT
);
