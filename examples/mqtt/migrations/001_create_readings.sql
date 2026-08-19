-- What each sensor reported.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS readings (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id   TEXT,
    value       REAL,
    unit        TEXT,
    received_at TEXT
);
