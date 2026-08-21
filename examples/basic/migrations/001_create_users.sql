-- The table the flows read and write.
--
-- Applied with `mycel migrate --config .` before starting; nothing creates it
-- otherwise, and every command in README.md answers 500 without it.

-- Create users table
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- No sample rows: the walkthrough in README.md creates the first user itself,
-- and seeding john@example.com made the very first command it shows fail on
-- the unique index.
