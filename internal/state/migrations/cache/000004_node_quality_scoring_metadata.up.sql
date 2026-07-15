ALTER TABLE node_quality ADD COLUMN cloudflare_status TEXT NOT NULL DEFAULT '';
ALTER TABLE node_quality ADD COLUMN scoring_policy_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_quality ADD COLUMN score_breakdown TEXT NOT NULL DEFAULT '';
