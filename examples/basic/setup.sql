-- Setup script for the example SQLite database
-- Run: sqlite3 ./data/app.db < setup.sql

-- Create users table
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Insert some sample data
INSERT OR IGNORE INTO users (email, name) VALUES
    ('john@example.com', 'John Doe'),
    ('jane@example.com', 'Jane Smith'),
    ('bob@example.com', 'Bob Johnson');
