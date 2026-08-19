-- The users the real flows use, when the mocks are not standing in for them.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT,
    email      TEXT,
    created_at TEXT
);
