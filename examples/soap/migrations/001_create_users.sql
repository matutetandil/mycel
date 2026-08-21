-- The users table the SOAP operations read and write.
--
-- Applied with `mycel migrate --config .` before starting; without it the
-- service starts and every operation comes back as a SOAP fault.

CREATE TABLE IF NOT EXISTS users (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    name  TEXT NOT NULL
);
