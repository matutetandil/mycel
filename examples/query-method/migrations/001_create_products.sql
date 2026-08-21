-- The products the QUERY searches over.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS products (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    price REAL NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO products (id, name, price) VALUES
    (1, 'Laptop Pro 14',   1999.00),
    (2, 'Laptop Pro 16',   2499.00),
    (3, 'Laptop Air',      1199.00),
    (4, 'Pro Keyboard',     149.00),
    (5, 'Pro Mouse',         89.00),
    (6, 'Monitor 27',       449.00);
