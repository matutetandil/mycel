CREATE TABLE IF NOT EXISTS orders (
  id       TEXT PRIMARY KEY,
  customer TEXT NOT NULL,
  total    REAL NOT NULL,
  written  TEXT NOT NULL
);
