CREATE TABLE IF NOT EXISTS orders (
  id         TEXT PRIMARY KEY,
  customer   TEXT NOT NULL,
  total      REAL NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS reports (
  id         TEXT PRIMARY KEY,
  month      TEXT NOT NULL,
  orders     INTEGER NOT NULL,
  revenue    REAL NOT NULL,
  created_at TEXT NOT NULL
);
