ALTER TABLE subscriptions ADD COLUMN clash_fingerprint_policy TEXT NOT NULL DEFAULT 'reject';
