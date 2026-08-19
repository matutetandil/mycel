-- The orders whose state the machine advances.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS orders (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    status           TEXT NOT NULL DEFAULT 'pending',
    tracking_number  TEXT,
    updated_at       TEXT
);

-- One order to transition, so that the walkthrough in README.md has something
-- to act on: it posts events to /orders/1/status.
INSERT OR IGNORE INTO orders (id, status) VALUES (1, 'pending');
