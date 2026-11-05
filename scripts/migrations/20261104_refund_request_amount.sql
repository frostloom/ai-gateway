-- Existing refunds remain NULL because requested amount cannot be inferred
-- reliably from the amount actually refunded.
ALTER TABLE topups ADD COLUMN requested_cent BIGINT NULL;
