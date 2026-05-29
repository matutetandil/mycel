-- Integration test schema for MySQL
CREATE TABLE IF NOT EXISTS users (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Target for the reusable-blocks transaction test (use = "transaction.ru_tx").
-- Transactions are supported on mysql/sqlite (not postgres), so the test
-- writes here.
CREATE TABLE IF NOT EXISTS ru_tx_results (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
