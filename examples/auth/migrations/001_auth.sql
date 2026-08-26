-- The tables the auth system reads and writes.
--
-- Applied with `mycel migrate --config .` before starting. They used to be a
-- block of SQL in the README for the reader to paste, which meant nothing kept
-- them in step with the configuration beside them — and two of the columns
-- below are not decoration: naming `roles` in the users fields block is what
-- turns roles on for a database-backed store, and naming
-- `password_changed_at` is what lets `password { max_age }` expire anything.

CREATE TABLE IF NOT EXISTS users (
  id                  VARCHAR(64) PRIMARY KEY,
  email               VARCHAR(255) UNIQUE NOT NULL,
  password_hash       VARCHAR(255) NOT NULL,
  roles               TEXT,
  password_changed_at TIMESTAMP,
  created_at          TIMESTAMP DEFAULT NOW(),
  updated_at          TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS auth_audit_log (
  id           SERIAL PRIMARY KEY,
  event        VARCHAR(50) NOT NULL,
  user_id      VARCHAR(64),
  email        VARCHAR(255),
  ip           VARCHAR(45),
  user_agent   TEXT,
  success      BOOLEAN,
  error_reason TEXT,
  created_at   TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS linked_accounts (
  id            VARCHAR(64) PRIMARY KEY,
  user_id       VARCHAR(64) NOT NULL REFERENCES users(id),
  provider      VARCHAR(50) NOT NULL,
  provider_id   VARCHAR(255) NOT NULL,
  email         VARCHAR(255),
  name          VARCHAR(255),
  picture       TEXT,
  access_token  TEXT,
  refresh_token TEXT,
  expires_at    TIMESTAMP,
  created_at    TIMESTAMP DEFAULT NOW(),
  updated_at    TIMESTAMP DEFAULT NOW()
);

-- What the two hook flows write.
CREATE TABLE IF NOT EXISTS welcome_log (
  user_id    VARCHAR(64) NOT NULL,
  email      VARCHAR(255) NOT NULL,
  greeted_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS failed_sign_ins (
  email      VARCHAR(255),
  ip         VARCHAR(45),
  noticed_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS reset_deliveries (
  email       VARCHAR(255) NOT NULL,
  reset_token TEXT NOT NULL,
  sent_at     TIMESTAMP NOT NULL
);
