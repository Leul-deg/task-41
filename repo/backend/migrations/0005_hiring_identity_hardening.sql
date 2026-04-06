ALTER TABLE candidates
ADD COLUMN IF NOT EXISTS name_search_token TEXT;

CREATE INDEX IF NOT EXISTS idx_candidates_name_search_token
ON candidates(name_search_token);
