-- The tables the flows read from and write to, including the ones the steps in
-- get_order_details look up.
--
-- Postgres syntax, because that is the driver this example's connector names.
-- Applied with `mycel migrate --config .` before starting; nothing creates them
-- otherwise, and every request answers 500.

CREATE TABLE IF NOT EXISTS orders (
    id          SERIAL PRIMARY KEY,
    customer_id INTEGER,
    product_id  TEXT,
    quantity    INTEGER,
    email       TEXT,
    status      TEXT,
    total       NUMERIC,
    created_at  TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS customers (
    id    SERIAL PRIMARY KEY,
    name  TEXT,
    email TEXT,
    tier  TEXT
);

CREATE TABLE IF NOT EXISTS shipping_estimates (
    id             SERIAL PRIMARY KEY,
    order_id       INTEGER,
    estimated_days INTEGER,
    carrier        TEXT
);

CREATE TABLE IF NOT EXISTS loyalty (
    id          SERIAL PRIMARY KEY,
    customer_id INTEGER,
    points      INTEGER,
    tier        TEXT
);

CREATE TABLE IF NOT EXISTS payments (
    id         SERIAL PRIMARY KEY,
    order_id   INTEGER,
    amount     NUMERIC,
    status     TEXT,
    created_at TIMESTAMPTZ DEFAULT now()
);

-- One order with everything the steps in get_order_details look up, so the
-- walkthrough in README.md has something to ask about: it fetches order 1.
INSERT INTO customers (id, name, email, tier) VALUES (1, 'Ada Lovelace', 'ada@example.com', 'gold')
    ON CONFLICT (id) DO NOTHING;
INSERT INTO orders (id, customer_id, product_id, quantity, email, status, total)
    VALUES (1, 1, 'abc-123', 2, 'ada@example.com', 'pending', 99.90)
    ON CONFLICT (id) DO NOTHING;
INSERT INTO shipping_estimates (order_id, estimated_days, carrier) VALUES (1, 3, 'NZ Post');
INSERT INTO loyalty (customer_id, points, tier) VALUES (1, 1200, 'gold');
