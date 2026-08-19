-- What was said in each room.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS messages (
    id         TEXT PRIMARY KEY,
    room       TEXT,
    text       TEXT,
    created_at TEXT
);
