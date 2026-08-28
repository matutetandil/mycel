-- The tables the cached flows read from.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS products (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    name  TEXT NOT NULL,
    price REAL
);

CREATE TABLE IF NOT EXISTS users (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    name  TEXT NOT NULL,
    email TEXT
);

-- Which store views a product appears in. Read by republish_product's step to
-- work out the key set to invalidate, which is one entry per row and so cannot
-- be counted while the configuration is parsed.
CREATE TABLE IF NOT EXISTS product_stores (
    product_id INTEGER NOT NULL,
    store_code TEXT NOT NULL,
    PRIMARY KEY (product_id, store_code)
);

INSERT OR IGNORE INTO product_stores (product_id, store_code) VALUES
    (1, 'us'), (1, 'uk'), (1, 'fr');
