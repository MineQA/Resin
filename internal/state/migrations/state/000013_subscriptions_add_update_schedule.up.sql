ALTER TABLE subscriptions ADD COLUMN update_mode TEXT NOT NULL DEFAULT 'interval';
ALTER TABLE subscriptions ADD COLUMN update_time TEXT NOT NULL DEFAULT '';
ALTER TABLE subscriptions ADD COLUMN update_timezone TEXT NOT NULL DEFAULT '';
ALTER TABLE subscriptions ADD COLUMN last_checked_ns INTEGER NOT NULL DEFAULT 0;
