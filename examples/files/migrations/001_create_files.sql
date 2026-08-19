-- What is known about each stored file. The bytes live under the file
-- connector's base_path; this is the index over them.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS files (
    filename    TEXT PRIMARY KEY,
    size        INTEGER,
    uploaded_at TEXT
);
