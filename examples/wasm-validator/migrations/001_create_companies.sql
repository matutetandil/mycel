-- The companies whose identifiers the WASM validator checks.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS companies (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    name  TEXT NOT NULL,
    cuit  TEXT,
    email TEXT
);
