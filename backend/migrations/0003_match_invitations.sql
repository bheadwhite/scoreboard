-- Invitations.
--
-- Naming somebody when you set up a match used to add them to it outright.
-- Now it asks. A participant row is created either way, but only a 'joined' one
-- holds a team, counts toward starting, or reaches the permanent record.
--
-- status: invited | joined | declined
--
-- Idempotent, like every migration here.

ALTER TABLE match_participants
    ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'joined';

ALTER TABLE match_participants
    ADD COLUMN IF NOT EXISTS invited_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE match_participants
    ADD COLUMN IF NOT EXISTS responded_at timestamptz;

-- Existing rows predate invitations and were all real participants, which the
-- 'joined' default already gives them.

-- Drives "who is still deciding" and the joined-roster reads on every match.
CREATE INDEX IF NOT EXISTS match_participants_status_idx
    ON match_participants (match_id, status);
