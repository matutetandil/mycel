-- Tables the scheduled jobs and the API read and write.
--
-- Applied with `mycel migrate --config .` before starting the service; nothing
-- creates them otherwise, and every endpoint answers 500 without them.

CREATE TABLE IF NOT EXISTS logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    message    TEXT NOT NULL,
    level      TEXT NOT NULL DEFAULT 'info',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS heartbeats (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    service    TEXT NOT NULL,
    status     TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS reports (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    total_logs   INTEGER NOT NULL,
    period_start TEXT NOT NULL,
    period_end   TEXT NOT NULL,
    generated_at TEXT,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);
