CREATE TABLE IF NOT EXISTS orders (
  id       TEXT PRIMARY KEY,
  customer TEXT NOT NULL,
  written  TEXT NOT NULL
);
