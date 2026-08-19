-- Setup script for the example SQLite database
-- Run: sqlite3 ./data/app.db < setup.sql

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
