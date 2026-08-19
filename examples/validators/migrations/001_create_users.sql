-- The table the validated flows write to.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    username   TEXT NOT NULL,
    email      TEXT NOT NULL,
    password   TEXT,
    age        INTEGER,
    phone      TEXT,
    status     TEXT,
    created_at TEXT
);
