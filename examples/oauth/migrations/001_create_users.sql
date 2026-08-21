-- The accounts a sign-in through a provider creates or finds.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS users (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    email       TEXT,
    name        TEXT,
    picture     TEXT,
    provider    TEXT,
    provider_id TEXT,
    created_at  TEXT
);
