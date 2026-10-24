ALTER TABLE users
  ADD COLUMN referral_code    VARCHAR(12),
  ADD COLUMN referred_by_code VARCHAR(12),
  ADD COLUMN referred_by_user_id UUID REFERENCES users(id);

-- Backfill existing users with a code derived deterministically from their UUID.
-- substring(replace(id::text, '-', ''), 1, 8) is unique because UUIDs are unique.
UPDATE users
SET referral_code = upper(substring(replace(id::text, '-', ''), 1, 8))
WHERE referral_code IS NULL;

ALTER TABLE users ALTER COLUMN referral_code SET NOT NULL;

CREATE UNIQUE INDEX users_referral_code_idx ON users(referral_code);
CREATE INDEX users_referred_by_user_id_idx ON users(referred_by_user_id)
  WHERE referred_by_user_id IS NOT NULL;
