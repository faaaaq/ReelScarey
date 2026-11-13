-- TOTP replay prevention. The last accepted time step is persisted per row so
-- the same authenticator code can never be verified twice, even inside the
-- ±1 step skew window. Old rows start at zero, which means only step 0 (the
-- Unix epoch step, impossible for a live authenticator) is treated as used.
ALTER TABLE admin_totp ADD COLUMN totp_last_step INTEGER NOT NULL DEFAULT 0;
