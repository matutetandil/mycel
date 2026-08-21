-- What is known about each object in the bucket. The objects themselves live
-- in S3; this is the index over them.
--
-- Applied with `mycel migrate --config .` before starting.

CREATE TABLE IF NOT EXISTS files (
    id          TEXT PRIMARY KEY,
    filename    TEXT,
    key         TEXT,
    size        INTEGER,
    content_type TEXT,
    uploaded_at TEXT
);
