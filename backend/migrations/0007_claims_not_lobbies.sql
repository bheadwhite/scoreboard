-- The lobby goes, and a dispute stops being a state.
--
-- Two changes that look separate and are the same idea: the app had two places
-- where a match sat waiting for people to agree with each other, and neither
-- was earning its keep.
--
-- The lobby was a staging area between "a game exists" and "a game is being
-- played", held open by a ready check. It meant three taps — join, ready,
-- start — to say one thing. Joining now says it: the contract is declared when
-- the match is posted, so agreeing to play is agreeing to the terms, and the
-- match is live from the moment it exists. Existing lobbies become matches in
-- play, which is what everybody in them already thought they were in.
--
-- The disputed status was the other one. An objection used to stop the clock
-- and wait for the family organizer to break the tie, which meant one person
-- could freeze a result indefinitely and the only way out was a referee ruling
-- on an argument they were not in. An objection now marks the match contested
-- instead: the claim still settles on schedule, and the contested result is
-- held out of the clean scoreboard for exactly as long as the objection stands.
-- Nothing here needs a column for that — it is read off match_participants
-- rows, which outlive finalization.
--
-- So existing disputed matches go back to being ordinary claimed ones, with the
-- clock they were denied restarted from now. Their objections survive untouched
-- in match_participants, which means each of them will settle on its own and
-- land contested, correctly, without anybody having to adjudicate it.
--
-- Idempotent, like every migration here.

-- Lobbies are matches in play. started_at backfilled from created_at, since
-- what a lobby recorded as "created" is the moment it was actually posted.
UPDATE matches
SET status     = 'in_progress',
    started_at = coalesce(started_at, created_at)
WHERE status = 'lobby';

-- Disputed matches rejoin the ordinary path with a full window. Full rather
-- than resumed: the clock was stopped for the argument, and the people who
-- never answered were watching it rather than ignoring a claim.
UPDATE matches
SET status           = 'claimed',
    concede_deadline = coalesce(concede_deadline, now() + interval '7 days')
WHERE status = 'disputed';

-- A match is in play the moment it is inserted. The column default carried the
-- old first stop; there is nothing before in_progress now.
ALTER TABLE matches
    ALTER COLUMN status SET DEFAULT 'in_progress';

-- Nothing affirms terms separately any more. The column is left in place rather
-- than dropped, deliberately: dropping it in the same release that stops reading
-- it is the one schema change DEPLOY.md rules out, because it takes the rollback
-- with it. The previous binary selects ready_at on every match read, so the
-- moment this column goes, `gcloud run services update-traffic` stops being a
-- way back from a release that rewrote the whole concession protocol — which is
-- exactly the release you would most want one from.
--
-- It is nullable and nothing writes it, so it costs a byte a row and no
-- correctness. Drop it in a later migration, once the revision that still reads
-- it is one nobody would roll back to.

-- Who named the winner, as distinct from who won. They were the same fact until
-- the creator could settle a game on everybody's behalf; now the claimant can
-- be on the losing side, and their claim has to count as their own answer to it.
ALTER TABLE matches
    ADD COLUMN IF NOT EXISTS claimed_by_user_id uuid REFERENCES users (id) ON DELETE SET NULL;

-- Backfill: every claim before this change was made by the winning side, so any
-- of them will do, and the earliest-joined member of it is the stable choice.
UPDATE matches m
SET claimed_by_user_id = (
        SELECT p.user_id
        FROM match_participants p
        WHERE p.match_id = m.id AND p.team_id = m.claimed_by_team_id
        ORDER BY p.invited_at, p.user_id
        LIMIT 1)
WHERE m.claimed_by_team_id IS NOT NULL AND m.claimed_by_user_id IS NULL;
