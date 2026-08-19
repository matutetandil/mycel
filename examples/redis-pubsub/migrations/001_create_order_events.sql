-- The events published on the channel, kept.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS order_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id     TEXT,
    status       TEXT,
    channel      TEXT,
    processed_at TEXT
);
