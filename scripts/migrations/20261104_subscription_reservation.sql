-- Existing unresolved subscription reservations remain NULL and require
-- reconciliation; do not infer an estimate for historical requests.
ALTER TABLE bills
  ADD COLUMN reserved_tokens BIGINT NULL,
  ADD COLUMN reserved_tier VARCHAR(64) NULL;
