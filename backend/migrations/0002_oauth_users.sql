-- Move identity from an external issuer to this app's own Google OAuth flow.
--
-- Users used to be keyed by a Supabase auth id supplied from outside. Now the
-- server runs the OAuth dance itself, so it mints its own ids and records which
-- provider account they belong to.
--
-- Idempotent, like every migration here: re-applied on each boot.

-- New rows get their own id; nothing hands us one any more.
ALTER TABLE users ALTER COLUMN id SET DEFAULT gen_random_uuid();

ALTER TABLE users ADD COLUMN IF NOT EXISTS provider    text NOT NULL DEFAULT 'google';
ALTER TABLE users ADD COLUMN IF NOT EXISTS provider_id text NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS email       text NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_login  timestamptz;

-- One account per provider identity. This is the conflict target the login
-- upsert relies on, so it must be a plain (non-partial) unique index.
CREATE UNIQUE INDEX IF NOT EXISTS users_provider_identity_idx
    ON users (provider, provider_id);
