-- Down migration: remove the three additive columns.
-- SQLite before 3.35.0 cannot DROP COLUMN, but modernc.org/sqlite ships
-- with a recent SQLite that supports ALTER TABLE DROP COLUMN.
-- If the engine is linked with an older sqlite3, the admin must recreate.
ALTER TABLE node_quality DROP COLUMN score_breakdown;
ALTER TABLE node_quality DROP COLUMN scoring_policy_version;
ALTER TABLE node_quality DROP COLUMN cloudflare_status;
