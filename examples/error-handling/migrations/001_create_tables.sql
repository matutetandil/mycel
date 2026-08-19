-- The tables the flows read from and write to, including the ones the steps in
-- get_order_details look up.
--
-- Applied with `mycel migrate --config .` before starting; nothing creates them
-- otherwise, and every request answers 500.

CREATE TABLE IF NOT EXISTS orders (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id INTEGER,
    email       TEXT,
    status      TEXT,
    total       REAL,
    created_at  TEXT
);

CREATE TABLE IF NOT EXISTS customers (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    name  TEXT,
    email TEXT,
    tier  TEXT
);

CREATE TABLE IF NOT EXISTS shipping_estimates (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id       INTEGER,
    estimated_days INTEGER,
    carrier        TEXT
);

CREATE TABLE IF NOT EXISTS loyalty (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id INTEGER,
    points      INTEGER,
    tier        TEXT
);

CREATE TABLE IF NOT EXISTS payments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id   INTEGER,
    amount     REAL,
    status     TEXT,
    created_at TEXT
);
