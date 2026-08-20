-- The orders the MySQL flows read and write.
--
-- Applied with `mycel migrate --config . --connector mysql`.

CREATE TABLE IF NOT EXISTS orders (
    id         INT AUTO_INCREMENT PRIMARY KEY,
    user_id    INT,
    total      DECIMAL(10,2),
    status     VARCHAR(32),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
