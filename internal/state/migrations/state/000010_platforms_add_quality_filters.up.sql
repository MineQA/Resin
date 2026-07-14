ALTER TABLE platforms ADD COLUMN quality_grade TEXT NOT NULL DEFAULT '';
ALTER TABLE platforms ADD COLUMN quality_min_score REAL NOT NULL DEFAULT 0.0;
ALTER TABLE platforms ADD COLUMN quality_cloudflare_challenged INTEGER;
ALTER TABLE platforms ADD COLUMN quality_checked_since_ns INTEGER NOT NULL DEFAULT 0;
ALTER TABLE platforms ADD COLUMN quality_profile TEXT NOT NULL DEFAULT '';
