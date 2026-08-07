-- Move a dispute's explanation onto the person who made it.
--
-- It lived on the match as one column, so a second objector overwrote the
-- first and the organizer settling the argument saw only the most recent one.
-- Several people can object to the same claim for different reasons, and each
-- of those is evidence.
--
-- matches.dispute_reason is left in place: it is written as the latest summary
-- and is harmless, but the per-participant column is what gets read.
--
-- Idempotent, like every migration here.

ALTER TABLE match_participants
    ADD COLUMN IF NOT EXISTS dispute_reason text NOT NULL DEFAULT '';
