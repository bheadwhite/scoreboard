-- The clock goes, and with it the pending stage in front of every result.
--
-- Naming a winner used to open a claim: it sat collecting answers and settled
-- either when the last opponent had spoken or when a week ran out. The honest
-- reading of that design is that the wait decided nothing. An unanswered claim
-- settled exactly as claimed. A unanimously rejected one settled exactly as
-- claimed, and landed contested. Both endings wrote the same row, so the week
-- bought a delay between somebody saying a thing and the app admitting they had
-- said it — not a check on whether they should have said it.
--
-- What actually protects the permanent record is unchanged and always was the
-- real mechanism: anybody in a match can object to its result, at any point in
-- its life including years later, without permission and without a majority,
-- and a contested result counts for nobody. That works on a settled match, so
-- it never needed anything to be pending in front of it.
--
-- Idempotent, like every migration here.

-- Every claim in flight is a result. This is not a policy applied to them
-- retroactively so much as the same policy stated earlier: each of these rows
-- was going to finalize with exactly this winner, on the answers it already had
-- or on its deadline, and nothing that could have happened in between would
-- have changed the outcome. Objections on them survive untouched in
-- match_participants, so the ones that were contested land contested.
UPDATE match_participants p
SET result = CASE WHEN p.team_id = m.claimed_by_team_id THEN 'win' ELSE 'loss' END
FROM matches m
WHERE p.match_id = m.id
  AND m.status = 'claimed'
  AND m.claimed_by_team_id IS NOT NULL
  AND p.status = 'joined';

-- finalized_at is when the claim was made rather than now(): the result dates
-- from the game, not from the deploy that stopped making it wait.
UPDATE matches
SET status       = 'final',
    finalized_at = coalesce(finalized_at, claimed_at, now())
WHERE status = 'claimed'
  AND claimed_by_team_id IS NOT NULL;

-- A claim with no team on it never existed, but the CHECK does not know that.
-- Such a row would be a result crediting nobody, so it goes back to being a
-- game in play for somebody to call properly.
UPDATE matches
SET status = 'in_progress'
WHERE status = 'claimed'
  AND claimed_by_team_id IS NULL;

-- Nothing waits on a clock, so nothing reads this. Left in place rather than
-- dropped, for the same reason ready_at was left in 0007: dropping a column in
-- the release that stops reading it takes the rollback with it, and the
-- previous binary selects concede_deadline on every match read. The moment it
-- goes, `gcloud run services update-traffic` stops being a way back from the
-- release that rewrote the concession protocol — which is precisely the release
-- you would most want a way back from.
--
-- Nulled rather than left stale so nothing can be tempted to read a deadline
-- that no longer means anything. Drop the column, and ready_at with it, once
-- the revision that still selects them is one nobody would roll back to.
UPDATE matches SET concede_deadline = NULL WHERE concede_deadline IS NOT NULL;

-- The sweeper index went with the sweeper. Nothing queries (status,
-- concede_deadline) any more, and unlike the column itself an index is free to
-- drop: the older binary's sweep query still runs without it, over a table that
-- by now has no claimed rows for it to find.
DROP INDEX IF EXISTS matches_sweep_idx;
