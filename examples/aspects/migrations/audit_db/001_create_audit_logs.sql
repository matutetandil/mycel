-- The audit trail the `audit_writes` aspect appends to, in a database of its
-- own so that what happened is kept apart from what it happened to.
--
-- The columns are the ones the aspect's transform produces.
--
-- Applied with `mycel migrate --config . --connector audit_db`.

CREATE TABLE IF NOT EXISTS audit_logs (
    id        TEXT PRIMARY KEY,
    flow      TEXT,
    operation TEXT,
    target    TEXT,
    affected  INTEGER,
    timestamp TEXT
);

-- Every request, appended by the `request_log` aspect before the flow runs.
CREATE TABLE IF NOT EXISTS request_logs (
    id        TEXT PRIMARY KEY,
    flow      TEXT,
    operation TEXT,
    timestamp TEXT
);

-- Failures, appended by the `error_logger` aspect.
CREATE TABLE IF NOT EXISTS error_logs (
    id            TEXT PRIMARY KEY,
    flow          TEXT,
    operation     TEXT,
    error_message TEXT,
    timestamp     TEXT
);
