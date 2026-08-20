-- The products the enrichment flows read and write.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS products (
    id         TEXT PRIMARY KEY,
    name       TEXT,
    -- What the inventory enrichment looks the stock up by.
    sku        TEXT,
    price      REAL,
    currency   TEXT,
    in_stock   BOOLEAN,
    fetched_at TEXT
);

-- The product the README asks for. A read flow shaped by a transform answers
-- with what the transform names, and it can only name columns that are there.
INSERT OR IGNORE INTO products (id, name, sku, price, currency, in_stock, fetched_at)
VALUES ('123', 'Widget', 'WIDGET-1', 0.0, 'USD', 1, '2026-01-01T00:00:00Z');
