-- Irreversible migration. ANY and MUST_NOT rules cannot be represented by the
-- legacy all-regexes-must-match format. Downgrading requires restoring a
-- pre-upgrade state.db backup.
-- This file exists solely to satisfy golang-migrate's down() requirement.
SELECT 1;
