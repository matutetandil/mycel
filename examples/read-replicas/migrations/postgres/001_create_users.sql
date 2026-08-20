-- The users the Postgres flows read and write. Reads go to a replica and
-- writes to the primary; both need the table.
--
-- Applied with `mycel migrate --config . --connector postgres`.

CREATE TABLE IF NOT EXISTS users (
    id         SERIAL PRIMARY KEY,
    name       TEXT,
    email      TEXT,
    created_at TIMESTAMPTZ DEFAULT now()
);
