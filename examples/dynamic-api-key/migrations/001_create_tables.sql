-- What the flows read once the API key has been validated.
--
-- Postgres syntax, because that is the driver this example's connector names.
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    name       TEXT,
    email      TEXT,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS resources (
    id         SERIAL PRIMARY KEY,
    tenant_id  TEXT,
    name       TEXT,
    created_at TIMESTAMPTZ DEFAULT now()
);
