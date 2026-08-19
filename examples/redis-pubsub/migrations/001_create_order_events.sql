-- The events published on the channel, kept.
--
-- Postgres syntax, because that is the driver this example's connector names.
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS order_events (
    id           SERIAL PRIMARY KEY,
    order_id     TEXT,
    status       TEXT,
    channel      TEXT,
    processed_at TIMESTAMPTZ DEFAULT now()
);
