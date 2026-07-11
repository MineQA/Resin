ALTER TABLE platforms ADD COLUMN protocol_filters_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE platforms ADD COLUMN exclude_protocol_filters_json TEXT NOT NULL DEFAULT '[]';
