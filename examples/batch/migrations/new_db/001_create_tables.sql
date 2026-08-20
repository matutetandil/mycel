-- Where the migration writes: the same shapes, in the database being moved to.
--
-- Applied with `mycel migrate --config . --connector new_db`.

CREATE TABLE IF NOT EXISTS products (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    description TEXT,
    price       REAL,
    category    TEXT,
    updated_at  TEXT
);

CREATE TABLE IF NOT EXISTS orders_archive (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id    INTEGER,
    payload     TEXT,
    archived_at TEXT
);
