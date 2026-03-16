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

-- Enable logical replication for CDC
ALTER SYSTEM SET wal_level = logical;
SELECT pg_reload_conf();

-- Create publication for CDC
CREATE PUBLICATION mycel_pub FOR ALL TABLES;
