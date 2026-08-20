-- Where the changes captured from the source database are written.
--
-- One table for three flows, so it holds the union of what they produce: a
-- user creation, an order's status change, and a session ending each fill in
-- the columns that mean something to them.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    event      TEXT NOT NULL,
    user_id    TEXT,
    email      TEXT,
    order_id   TEXT,
    old_status TEXT,
    new_status TEXT,
    token      TEXT,
    data       TEXT,
    changed_at TEXT,
    created_at TEXT
);
