-- Initial schema.
--
-- Written to be idempotent: this file is re-applied on every boot, so every
-- statement uses IF NOT EXISTS or ON CONFLICT DO NOTHING.

-- Players. See 0002 for the provider identity columns; ids are minted here, so
-- this schema stands alone and can be pointed at any Postgres.
CREATE TABLE IF NOT EXISTS users (
    id           uuid PRIMARY KEY,
    display_name text        NOT NULL DEFAULT '',
    avatar_url   text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS families (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text        NOT NULL,
    invite_code text        NOT NULL UNIQUE,
    owner_id    uuid        NOT NULL REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS family_members (
    family_id uuid        NOT NULL REFERENCES families (id) ON DELETE CASCADE,
    user_id   uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role      text        NOT NULL DEFAULT 'member',
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (family_id, user_id)
);

CREATE INDEX IF NOT EXISTS family_members_user_idx ON family_members (user_id);

-- A game with a null family_id belongs to the built-in catalog and is visible
-- to every family.
CREATE TABLE IF NOT EXISTS games (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id   uuid        REFERENCES families (id) ON DELETE CASCADE,
    name        text        NOT NULL,
    emoji       text        NOT NULL DEFAULT '',
    team_based  boolean     NOT NULL DEFAULT false,
    min_players integer     NOT NULL DEFAULT 2,
    max_players integer     NOT NULL DEFAULT 8,
    created_by  uuid        REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS games_family_idx ON games (family_id);

-- status: lobby | in_progress | claimed | final | disputed | canceled
CREATE TABLE IF NOT EXISTS matches (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id          uuid        NOT NULL REFERENCES families (id) ON DELETE CASCADE,
    game_id            uuid        NOT NULL REFERENCES games (id),
    status             text        NOT NULL DEFAULT 'lobby',
    contract_kind      text        NOT NULL DEFAULT 'unspecified',
    contract_target    integer     NOT NULL DEFAULT 0,
    contract_detail    text        NOT NULL DEFAULT '',
    created_by         uuid        NOT NULL REFERENCES users (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    started_at         timestamptz,
    claimed_at         timestamptz,
    claimed_by_team_id uuid,
    -- After this instant, outstanding concessions are assumed. A dispute clears
    -- it, which is how disagreement is distinguished from silence.
    concede_deadline   timestamptz,
    dispute_reason     text        NOT NULL DEFAULT '',
    finalized_at       timestamptz
);

CREATE INDEX IF NOT EXISTS matches_family_idx ON matches (family_id, status);
CREATE INDEX IF NOT EXISTS matches_family_game_idx ON matches (family_id, game_id);
-- Drives the sweeper that finalizes claims whose clock ran out.
CREATE INDEX IF NOT EXISTS matches_sweep_idx ON matches (status, concede_deadline);

-- Every match has teams, including free-for-all matches, where each player gets
-- a team of one. That collapses "concede to a player" and "concede to a team"
-- into a single code path.
CREATE TABLE IF NOT EXISTS match_teams (
    id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id uuid    NOT NULL REFERENCES matches (id) ON DELETE CASCADE,
    name     text    NOT NULL,
    ordinal  integer NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS match_teams_match_idx ON match_teams (match_id);

-- result: pending | win | loss, written at finalization.
CREATE TABLE IF NOT EXISTS match_participants (
    match_id    uuid NOT NULL REFERENCES matches (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    team_id     uuid REFERENCES match_teams (id) ON DELETE SET NULL,
    result      text NOT NULL DEFAULT 'pending',
    conceded_at timestamptz,
    disputed_at timestamptz,
    PRIMARY KEY (match_id, user_id)
);

-- The head-to-head self-join is the query this whole app exists for.
CREATE INDEX IF NOT EXISTS match_participants_user_idx ON match_participants (user_id);
CREATE INDEX IF NOT EXISTS match_participants_match_idx ON match_participants (match_id);

-- Built-in catalog: a starting point, not a constraint. Families add their own
-- from the app, which is where anything not listed here belongs. Fixed ids so
-- re-running this file updates each row in place, so a corrected name or player
-- count reaches existing databases on the next boot. Removing a line, though,
-- does not delete the row from a database that already has it.
INSERT INTO games (id, family_id, name, emoji, team_based, min_players, max_players)
VALUES
    ('11111111-0000-4000-8000-000000000001', NULL, 'Rook',              '🐦', true,  4, 4),
    ('11111111-0000-4000-8000-000000000002', NULL, 'Catan',             '🌾', false, 3, 6),
    ('11111111-0000-4000-8000-000000000003', NULL, 'Ping Pong',         '🏓', false, 2, 4),
    ('11111111-0000-4000-8000-000000000004', NULL, 'Monopoly',          '🎩', false, 2, 8),
    ('11111111-0000-4000-8000-000000000005', NULL, 'Ticket to Ride',    '🚂', false, 2, 5),
    ('11111111-0000-4000-8000-000000000006', NULL, 'Pickleball',        '🥒', true,  2, 4),
    ('11111111-0000-4000-8000-000000000007', NULL, 'Chess',             '♟️', false, 2, 2),
    ('11111111-0000-4000-8000-000000000008', NULL, 'Hearts',            '♥️', false, 3, 6),
    ('11111111-0000-4000-8000-000000000009', NULL, 'Spades',            '♠️', true,  4, 4),
    ('11111111-0000-4000-8000-00000000000a', NULL, 'Uno',               '🃏', false, 2, 10),
    ('11111111-0000-4000-8000-00000000000b', NULL, 'Cornhole',          '🌽', true,  2, 4),
    ('11111111-0000-4000-8000-00000000000c', NULL, 'Scrabble',          '🔤', false, 2, 4)
ON CONFLICT (id) DO UPDATE SET
    name        = EXCLUDED.name,
    emoji       = EXCLUDED.emoji,
    team_based  = EXCLUDED.team_based,
    min_players = EXCLUDED.min_players,
    max_players = EXCLUDED.max_players;
