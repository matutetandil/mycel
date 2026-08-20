-- The tables the batch flows read from and write to.
--
-- This example moves rows from one database to another, so each is migrated by
-- name: `mycel migrate --config . --connector old_db`, and the same for new_db.

CREATE TABLE IF NOT EXISTS products (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    description TEXT,
    price       REAL,
    category    TEXT,
    updated_at  TEXT
);

CREATE TABLE IF NOT EXISTS users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT,
    email      TEXT,
    created_at TEXT
);

CREATE TABLE IF NOT EXISTS orders (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER,
    total      REAL,
    status     TEXT,
    created_at TEXT
);

CREATE TABLE IF NOT EXISTS orders_archive (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id    INTEGER,
    payload     TEXT,
    archived_at TEXT
);
