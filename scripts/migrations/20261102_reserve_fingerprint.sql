-- Apply before deploying code that reads/writes reserve_fingerprint.
-- Existing rows remain NULL: their original reserve inputs cannot be verified.
ALTER TABLE bills ADD COLUMN reserve_fingerprint VARCHAR(64) NULL;
