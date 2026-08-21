-- What the subscription client keeps: the products it follows, and each price
-- it was told about.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS tracked_products (
    id         TEXT PRIMARY KEY,
    sku        TEXT,
    name       TEXT,
    category   TEXT,
    price      REAL,
    created_at TEXT,
    tracked_at TEXT
);

CREATE TABLE IF NOT EXISTS price_history (
    id         TEXT PRIMARY KEY,
    sku        TEXT,
    old_price  REAL,
    new_price  REAL,
    changed_at TEXT
);
