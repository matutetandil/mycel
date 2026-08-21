-- The orders a message from the queue becomes.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS orders (
    id           TEXT PRIMARY KEY,
    customer     TEXT,
    product      TEXT,
    quantity     INTEGER,
    status       TEXT,
    processed_at TEXT,
    created_at   TEXT
);
