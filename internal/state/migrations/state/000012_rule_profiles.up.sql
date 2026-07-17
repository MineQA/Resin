CREATE TABLE IF NOT EXISTS rule_profiles (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL COLLATE NOCASE UNIQUE,
    template_yaml   TEXT NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at_ns   INTEGER NOT NULL,
    updated_at_ns   INTEGER NOT NULL
);
