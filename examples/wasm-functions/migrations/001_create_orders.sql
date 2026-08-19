-- The orders priced by the functions the plugin provides.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS orders (
    order_id       TEXT PRIMARY KEY,
    customer_email TEXT,
    subtotal       REAL,
    discount       REAL,
    tax            REAL,
    total          REAL,
    created_at     TEXT
);
