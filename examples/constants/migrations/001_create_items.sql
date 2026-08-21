-- The catalogue the flows read and write.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS items (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    sku    TEXT NOT NULL,
    name   TEXT,
    total  REAL,
    region TEXT,
    large  BOOLEAN
);

INSERT OR IGNORE INTO items (sku, name, total, region, large) VALUES
    ('SKU-0001', 'Widget',  9.99,  'us', 0),
    ('SKU-0002', 'Gadget',  24.99, 'us', 0),
    ('SKU-0003', 'Doohickey', 4.50, 'us', 0),
    ('SKU-0004', 'Contraption', 1500.00, 'us', 1);
