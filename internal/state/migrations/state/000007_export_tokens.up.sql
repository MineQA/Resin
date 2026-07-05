CREATE TABLE IF NOT EXISTS export_tokens (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    token_hash      TEXT NOT NULL UNIQUE,
    token_prefix    TEXT NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at_ns   INTEGER NOT NULL,
    last_used_at_ns INTEGER NOT NULL DEFAULT 0
);
