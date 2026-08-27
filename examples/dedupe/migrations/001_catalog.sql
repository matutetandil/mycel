-- The catalog table the existence gate reads.
--
-- Applied with `mycel migrate --config .` before starting. In a real
-- deployment this is the downstream's own schema (Magento's
-- catalog_product_entity, an ERP's item master), read but never written by
-- this consumer.
CREATE TABLE IF NOT EXISTS catalog_items (
    sku        TEXT PRIMARY KEY,
    name       TEXT,
    updated_at TEXT
);
