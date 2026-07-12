CREATE TABLE IF NOT EXISTS node_quality (
    node_hash TEXT NOT NULL,
    profile TEXT NOT NULL,
    grade TEXT NOT NULL DEFAULT '',
    score REAL NOT NULL DEFAULT 0,
    unstable INTEGER NOT NULL DEFAULT 0,
    service_reachable INTEGER NOT NULL DEFAULT 0,
    api_reachable INTEGER NOT NULL DEFAULT 0,
    cloudflare_challenged INTEGER NOT NULL DEFAULT 0,
    cloudflare_challenge_type TEXT NOT NULL DEFAULT '',
    avg_latency_ms REAL NOT NULL DEFAULT 0,
    last_checked_ns INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(node_hash, profile)
);
