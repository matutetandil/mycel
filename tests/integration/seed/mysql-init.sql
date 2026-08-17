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

-- Accounts, sessions and revoked tokens, for the SQL auth stores.
--
-- These stores map onto tables somebody already has rather than creating them,
-- and they speak MySQL — so a Go test against a real server is the only place
-- they run at all. The columns are the ones the stores name.
CREATE TABLE IF NOT EXISTS auth_users (
    id             VARCHAR(64) PRIMARY KEY,
    email          VARCHAR(255) UNIQUE NOT NULL,
    password_hash  TEXT,
    name           VARCHAR(255),
    roles          TEXT,
    email_verified BOOLEAN DEFAULT false,
    active         BOOLEAN DEFAULT true,
    metadata       TEXT,
    mfa_enabled    BOOLEAN DEFAULT false,
    created_at     TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at     TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_login_at  TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS auth_sessions (
    id             VARCHAR(64) PRIMARY KEY,
    user_id        VARCHAR(64) NOT NULL,
    ip             VARCHAR(64),
    user_agent     TEXT,
    created_at     TIMESTAMP NULL,
    last_active_at TIMESTAMP NULL,
    expires_at     TIMESTAMP NULL,
    device_id      VARCHAR(128)
);

CREATE TABLE IF NOT EXISTS auth_tokens (
    token_id   VARCHAR(128) PRIMARY KEY,
    expires_at TIMESTAMP NULL
);
