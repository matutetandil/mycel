-- Where the changes captured from the source database are written.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    event      TEXT,
    data       TEXT,
    order_id   TEXT,
    email      TEXT,
    old_status TEXT,
    new_status TEXT,
    changed_at TEXT,
    created_at TEXT
);
