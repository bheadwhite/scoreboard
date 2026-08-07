-- Readiness.
--
-- Joining a match says you intend to play. It does not say you have seen the
-- contract everyone eventually settled on — and since any player can change
-- that contract right up until the game starts, agreeing to play is not the
-- same as agreeing to the terms.
--
-- So every joined player affirms readiness against the contract as it stands,
-- and changing the contract or the teams clears every affirmation. Nobody ends
-- up bound to terms they never saw. Only then can the creator start.
--
-- Idempotent, like every migration here.

ALTER TABLE match_participants
    ADD COLUMN IF NOT EXISTS ready_at timestamptz;
