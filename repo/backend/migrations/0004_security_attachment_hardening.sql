ALTER TABLE refresh_tokens
ADD COLUMN IF NOT EXISTS token_hash TEXT;

UPDATE refresh_tokens
SET token_hash = encode(digest(btrim(token), 'sha256'), 'hex')
WHERE COALESCE(token_hash, '') = '' AND COALESCE(token, '') <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash
ON refresh_tokens(token_hash)
WHERE token_hash IS NOT NULL;

ALTER TABLE step_up_tokens
ADD COLUMN IF NOT EXISTS token_hash TEXT;

UPDATE step_up_tokens
SET token_hash = encode(digest(btrim(token), 'sha256'), 'hex')
WHERE COALESCE(token_hash, '') = '' AND COALESCE(token, '') <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_step_up_tokens_token_hash
ON step_up_tokens(token_hash)
WHERE token_hash IS NOT NULL;

ALTER TABLE ticket_attachments
ADD COLUMN IF NOT EXISTS storage_path TEXT,
ADD COLUMN IF NOT EXISTS size_bytes BIGINT;

ALTER TABLE inventory_ledger
ADD COLUMN IF NOT EXISTS reversal_of UUID REFERENCES inventory_ledger(id);
