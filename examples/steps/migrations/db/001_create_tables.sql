-- Every table the flows in this example read from or write to.
--
-- The example is a tour of what a step can do, so it touches a lot: each
-- lookup a flow makes needs somewhere to look. Applied with
-- `mycel migrate --config .` before starting; without them the service starts
-- and answers 500 to every request.

-- What the flows are about
CREATE TABLE IF NOT EXISTS users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT,
    email      TEXT,
    created_at TEXT
);

CREATE TABLE IF NOT EXISTS customers (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    name  TEXT,
    email TEXT,
    tier  TEXT
);

CREATE TABLE IF NOT EXISTS products (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT,
    -- Read by get_product_details, which shapes it into its answer.
    description TEXT,
    price       REAL,
    stock       INTEGER DEFAULT 0
);

-- The columns create_order writes, which is every field its transform builds.
-- A destination named by table is written field by field, so a transform that
-- computes a field the table does not have fails the whole write.
CREATE TABLE IF NOT EXISTS orders (
    -- Text, because the README asks for order "ord-123": an order identifier
    -- is rarely a counter, and a flow that looks one up by the id in the path
    -- has to be able to find it.
    id           TEXT PRIMARY KEY,
    user_id      INTEGER,
    user_email   TEXT,
    product_id   INTEGER,
    product_name TEXT,
    quantity     INTEGER,
    unit_price   REAL,
    tax_rate     REAL,
    subtotal     REAL,
    tax          REAL,
    status       TEXT DEFAULT 'pending',
    total        REAL,
    created_at   TEXT
);

CREATE TABLE IF NOT EXISTS order_items (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id   INTEGER,
    product_id INTEGER,
    quantity   INTEGER,
    price      REAL
);

-- What the steps look up
CREATE TABLE IF NOT EXISTS inventory (
    product_id INTEGER PRIMARY KEY,
    available  INTEGER DEFAULT 0,
    reserved   INTEGER DEFAULT 0,
    warehouse  TEXT
);

CREATE TABLE IF NOT EXISTS prices (
    product_id INTEGER PRIMARY KEY,
    price      REAL,
    currency   TEXT,
    discount   REAL,
    tax_rate   REAL
);

CREATE TABLE IF NOT EXISTS reviews (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    product_id INTEGER,
    rating     INTEGER,
    comment    TEXT
);

CREATE TABLE IF NOT EXISTS accounts (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    balance REAL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS payment_methods (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER,
    kind       TEXT,
    is_default BOOLEAN DEFAULT false
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id INTEGER,
    plan        TEXT,
    status      TEXT,
    expires_at  TEXT
);

CREATE TABLE IF NOT EXISTS customer_preferences (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id  INTEGER,
    language     TEXT,
    currency     TEXT,
    notify_email BOOLEAN DEFAULT true
);

CREATE TABLE IF NOT EXISTS vip_users (
    id    INTEGER PRIMARY KEY,
    since TEXT
);

CREATE TABLE IF NOT EXISTS fraud_scores (
    user_id    INTEGER PRIMARY KEY,
    risk_score REAL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS allowed_sources (
    source TEXT PRIMARY KEY
);

-- What the flows write
CREATE TABLE IF NOT EXISTS payments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id   INTEGER,
    amount     REAL,
    status     TEXT,
    created_at TEXT
);

CREATE TABLE IF NOT EXISTS order_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id   INTEGER,
    event      TEXT,
    created_at TEXT
);

CREATE TABLE IF NOT EXISTS order_summaries (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id   INTEGER,
    summary    TEXT,
    created_at TEXT
);

CREATE TABLE IF NOT EXISTS high_value_orders (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id   INTEGER,
    total      REAL,
    created_at TEXT
);

CREATE TABLE IF NOT EXISTS external_orders (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    external_id TEXT,
    payload     TEXT,
    created_at  TEXT
);

CREATE TABLE IF NOT EXISTS failed_orders (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    flow             TEXT,
    original_payload TEXT,
    failed_at        TEXT
);

CREATE TABLE IF NOT EXISTS customer_profiles (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id INTEGER,
    profile     TEXT,
    created_at  TEXT
);

CREATE TABLE IF NOT EXISTS product_cache (
    product_id INTEGER PRIMARY KEY,
    payload    TEXT,
    cached_at  TEXT
);

CREATE TABLE IF NOT EXISTS audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    flow       TEXT,
    operation  TEXT,
    detail     TEXT,
    created_at TEXT
);


-- The rows the README's commands ask for. A tour of what a step can do is a
-- tour of lookups, and a lookup against an empty table returns nothing: every
-- reference to a field of that step then fails, which reads like a mistake in
-- the expression rather than an empty database.
INSERT OR IGNORE INTO users (id, name, email, created_at)
VALUES (1, 'Ada Lovelace', 'ada@example.com', '2026-01-01T00:00:00Z');

INSERT OR IGNORE INTO customers (id, name, email, tier)
VALUES (1, 'Ada Lovelace', 'ada@example.com', 'gold');

INSERT OR IGNORE INTO products (id, name, description, price, stock)
VALUES (100, 'Widget', 'A widget, for widgeting', 29.99, 42);

-- The pricing and inventory services of this example are two more tables.
INSERT OR IGNORE INTO prices (product_id, price, currency, discount, tax_rate)
VALUES (100, 29.99, 'USD', 0.0, 0.21);

INSERT OR IGNORE INTO inventory (product_id, available, reserved, warehouse)
VALUES (100, 42, 3, 'main');

INSERT OR IGNORE INTO orders (id, user_id, product_id, status, total, created_at)
VALUES ('ord-123', 1, 100, 'confirmed', 59.98, '2026-01-02T00:00:00Z');
