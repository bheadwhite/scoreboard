-- One scoreboard, still scoped by family.
--
-- The app stopped being multi-family: the UI no longer creates, joins, names or
-- switches between families, and signing in puts you in the one that already
-- exists. The column stays on every row — collapsing it would rewrite matches,
-- games and stats for no gain — so what this file adds is the answer to "which
-- family is *the* family", which is what sign-in now needs to know.
--
-- Consolidating the families that already exist is deliberately not done here.
-- Every boot re-applies this file, and folding people's matches together is not
-- something that should happen as a side effect of a deploy: cmd/consolidate
-- does it, once, on purpose, after printing what it is about to do.

ALTER TABLE families ADD COLUMN IF NOT EXISTS is_default boolean NOT NULL DEFAULT false;

-- At most one, enforced by the database rather than by remembering to clear the
-- old one. Indexing a constant is how a predicate gets a uniqueness constraint
-- with no column to hang it on.
CREATE UNIQUE INDEX IF NOT EXISTS families_one_default_idx
    ON families ((true)) WHERE is_default;

-- Designate the oldest family on a database that predates this column, so an
-- existing deployment has an answer before anybody signs in. The NOT EXISTS is
-- what makes re-applying safe: once something is marked, this never moves it,
-- and cmd/consolidate is free to point it somewhere else.
UPDATE families SET is_default = true
WHERE id = (SELECT id FROM families ORDER BY created_at, id LIMIT 1)
  AND NOT EXISTS (SELECT 1 FROM families WHERE is_default);
