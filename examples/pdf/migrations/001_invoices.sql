CREATE TABLE IF NOT EXISTS invoices (
  id       TEXT PRIMARY KEY,
  number   TEXT NOT NULL,
  customer TEXT NOT NULL,
  issued   TEXT NOT NULL,
  total    REAL NOT NULL
);

INSERT OR IGNORE INTO invoices (id, number, customer, issued, total) VALUES
  ('1', 'INV-001', 'Acme Inc',      '2026-08-01', 1240.00),
  ('2', 'INV-002', 'Globex Export', '2026-08-14',  318.75);
