-- The three tables the transaction writes to, in one aggregate.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS product (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    sku  TEXT,
    name TEXT
);

CREATE TABLE IF NOT EXISTS product_option (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    product_id INTEGER,
    code       TEXT,
    position   INTEGER
);

CREATE TABLE IF NOT EXISTS option_value (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    option_id INTEGER,
    label     TEXT,
    price     REAL
);
