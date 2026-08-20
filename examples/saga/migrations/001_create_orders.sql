-- The saga writes its order here in step one and deletes it again when a later
-- step fails, so the table has to exist before the first request.
CREATE TABLE IF NOT EXISTS orders (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    TEXT    NOT NULL,
    amount     REAL    NOT NULL,
    status     TEXT    NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
