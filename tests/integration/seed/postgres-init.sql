-- Integration test schema for PostgreSQL
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE items (
    id SERIAL PRIMARY KEY,
    title TEXT NOT NULL,
    status TEXT DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE dlq_failed (
    id SERIAL PRIMARY KEY,
    error TEXT,
    payload TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE mq_results (
    id SERIAL PRIMARY KEY,
    source TEXT,
    data TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE step_results (
    id SERIAL PRIMARY KEY,
    data TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE transform_results (
    id SERIAL PRIMARY KEY,
    generated_id TEXT,
    lowered TEXT,
    uppered TEXT,
    timestamp TEXT,
    combined TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE validate_results (
    id SERIAL PRIMARY KEY,
    name TEXT,
    email TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE filter_results (
    id SERIAL PRIMARY KEY,
    result TEXT,
    status TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE http_results (
    id SERIAL PRIMARY KEY,
    status TEXT,
    upstream TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE products (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    price NUMERIC,
    sku TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Fan-out integration test tables
CREATE TABLE fanout_primary (
    id SERIAL PRIMARY KEY,
    name TEXT,
    target TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE fanout_secondary (
    id SERIAL PRIMARY KEY,
    name TEXT,
    target TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE fanout_mq_results (
    id SERIAL PRIMARY KEY,
    source TEXT,
    data TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE accept_results (
    id SERIAL PRIMARY KEY,
    action TEXT,
    region TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Enable logical replication for CDC
ALTER SYSTEM SET wal_level = logical;
SELECT pg_reload_conf();

-- Create publication for CDC
CREATE PUBLICATION mycel_pub FOR ALL TABLES;

-- Accounts, for the root auth block.
--
-- The SQL user stores map onto a table somebody already has rather than
-- creating one, so this is what a service points auth at. Only integration can
-- cover them: they speak Postgres, not something a unit test can stand up.
CREATE TABLE auth_users (
    id            TEXT PRIMARY KEY,
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT,
    name          TEXT,
    roles         TEXT,
    email_verified BOOLEAN DEFAULT false,
    active        BOOLEAN DEFAULT true,
    metadata      TEXT,
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_login_at TIMESTAMP
);

-- What the audit block writes: who signed in, what failed, when.
CREATE TABLE auth_audit_log (
    id         SERIAL PRIMARY KEY,
    event      TEXT,
    user_id    TEXT,
    email      TEXT,
    ip         TEXT,
    user_agent TEXT,
    success    BOOLEAN,
    reason     TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Entities driven by a state machine. The engine reads the current state from
-- the status column and writes the new one back, so an integration run is the
-- only place the read-guard-act-write cycle happens against a real database.
CREATE TABLE machine_orders (
    id TEXT PRIMARY KEY,
    status TEXT,
    tracking_number TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

INSERT INTO machine_orders (id, status) VALUES
    ('order-pending', 'pending'),
    ('order-paid', 'paid'),
    ('order-delivered', 'delivered');
