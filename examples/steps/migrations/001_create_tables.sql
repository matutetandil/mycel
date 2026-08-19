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
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    name  TEXT,
    price REAL,
    stock INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS orders (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER,
    product_id INTEGER,
    status     TEXT DEFAULT 'pending',
    total      REAL,
    created_at TEXT
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
