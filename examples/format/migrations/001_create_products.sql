-- The table the flows read and write, whatever format the request arrives in.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS products (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    name  TEXT NOT NULL,
    price REAL
);
