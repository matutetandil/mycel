CREATE TABLE IF NOT EXISTS contacts (
  id          TEXT PRIMARY KEY,
  email       TEXT NOT NULL,
  domain      TEXT NOT NULL,
  display     TEXT NOT NULL,
  initials    TEXT NOT NULL,
  tags        TEXT NOT NULL,
  tag_count   INTEGER NOT NULL,
  email_len   INTEGER NOT NULL,
  source      TEXT NOT NULL,
  signed_up   TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  received_at INTEGER NOT NULL
);
