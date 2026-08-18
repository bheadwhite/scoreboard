package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/bheadwhite/scoreboard/backend/internal/model"
)

// Consolidation folds every family into one.
//
// It exists because the app was multi-family before it was single-family, and
// relatives who each tapped "start a family" ended up with a scoreboard each.
// The point of the app is the shared record, so the fix is not to hide the
// extra families — it is to move their matches into the one everybody now
// lands in, which is what makes "you have beaten Dad at Rook 14 times" true
// again.
//
// Nothing here runs on boot. cmd/consolidate calls it once, after printing what
// it is about to do. See migrations/0006_default_family.sql for why.

// FamilySummary is one family as the plan describes it.
type FamilySummary struct {
	ID      string
	Name    string
	Members int
	Matches int
	Games   int
}

// ConsolidationPlan is what consolidating would do, in the terms somebody
// running it against production needs to check before saying yes.
type ConsolidationPlan struct {
	Destination FamilySummary
	// Everything else, which will be emptied and deleted.
	Sources []FamilySummary
	// People who are not yet in the destination and would join it.
	JoiningMembers int
	// Matches and family-specific games that would move.
	MovingMatches int
	MovingGames   int
	// Custom games in the destination that would end up identical to another
	// and be collapsed into it, their matches repointed.
	DuplicateGames int
}

// Nothing reports whether the plan would change anything.
func (p ConsolidationPlan) Nothing() bool { return len(p.Sources) == 0 }

// ResolveFamily finds a family by id or by name, case-insensitively. An empty
// ref means the default family — the one the app already treats as itself.
func (s *Store) ResolveFamily(ctx context.Context, ref string) (model.Family, error) {
	if ref == "" {
		f, err := s.DefaultFamily(ctx)
		if errors.Is(err, ErrNotFound) {
			// Nothing is designated yet, which happens on a database that has
			// never run the migration's backfill against any families. Fall
			// back to the busiest one: on a scattered database that is the
			// family worth keeping, and on a single-family one it is the only
			// candidate anyway.
			return s.busiestFamily(ctx)
		}
		return f, err
	}

	const q = `
		SELECT f.id, f.name, f.invite_code, f.owner_id, f.created_at,
		       (SELECT count(*) FROM family_members x WHERE x.family_id = f.id)
		FROM families f
		WHERE f.id::text = $1 OR lower(f.name) = lower($1)`

	var f model.Family
	err := s.pool.QueryRow(ctx, q, ref).
		Scan(&f.ID, &f.Name, &f.InviteCode, &f.OwnerID, &f.CreatedAt, &f.MemberCount)
	return f, noRows(err)
}

// busiestFamily is the most members, ties broken by age.
func (s *Store) busiestFamily(ctx context.Context) (model.Family, error) {
	const q = `
		SELECT f.id, f.name, f.invite_code, f.owner_id, f.created_at,
		       (SELECT count(*) FROM family_members x WHERE x.family_id = f.id) AS members
		FROM families f
		ORDER BY members DESC, f.created_at, f.id
		LIMIT 1`

	var f model.Family
	err := s.pool.QueryRow(ctx, q).
		Scan(&f.ID, &f.Name, &f.InviteCode, &f.OwnerID, &f.CreatedAt, &f.MemberCount)
	return f, noRows(err)
}

// PlanConsolidation describes what Consolidate would do, without doing it.
func (s *Store) PlanConsolidation(ctx context.Context, destID string) (ConsolidationPlan, error) {
	var p ConsolidationPlan

	families, err := s.summarizeFamilies(ctx)
	if err != nil {
		return p, err
	}
	for _, f := range families {
		if f.ID == destID {
			p.Destination = f
			continue
		}
		p.Sources = append(p.Sources, f)
		p.MovingMatches += f.Matches
		p.MovingGames += f.Games
	}
	if p.Destination.ID == "" {
		return p, ErrNotFound
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT count(DISTINCT m.user_id)
		FROM family_members m
		WHERE m.family_id <> $1
		  AND NOT EXISTS (
		      SELECT 1 FROM family_members d
		      WHERE d.family_id = $1 AND d.user_id = m.user_id)`, destID).
		Scan(&p.JoiningMembers); err != nil {
		return p, err
	}

	// Counted as the merged set would look, since duplicates only exist once
	// everything is in the same family.
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) - count(DISTINCT (lower(name), team_based, min_players, max_players))
		FROM games WHERE family_id IS NOT NULL`).Scan(&p.DuplicateGames); err != nil {
		return p, err
	}

	return p, nil
}

func (s *Store) summarizeFamilies(ctx context.Context) ([]FamilySummary, error) {
	const q = `
		SELECT f.id, f.name,
		       (SELECT count(*) FROM family_members m WHERE m.family_id = f.id),
		       (SELECT count(*) FROM matches x WHERE x.family_id = f.id),
		       (SELECT count(*) FROM games g WHERE g.family_id = f.id)
		FROM families f
		ORDER BY f.created_at, f.id`

	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FamilySummary
	for rows.Next() {
		var f FamilySummary
		if err := rows.Scan(&f.ID, &f.Name, &f.Members, &f.Matches, &f.Games); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Consolidate moves every other family's members, matches and games into destID
// and deletes what is left behind, in one transaction.
//
// Order matters. Members and matches move before the delete, because deleting a
// family cascades to its memberships and its matches — the whole permanent
// record of the families being absorbed is one statement away from going with
// them.
func (s *Store) Consolidate(ctx context.Context, destID string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		// Membership first. ON CONFLICT DO NOTHING rather than an upsert, so
		// the destination's own organizer is never demoted to a member by the
		// row that arrives from a family they also happened to be in. Everyone
		// arriving keeps the earliest date they joined anything, which is the
		// only reading of "joined" that survives the merge.
		if _, err := tx.Exec(ctx, `
			INSERT INTO family_members (family_id, user_id, role, joined_at)
			SELECT $1, m.user_id, 'member', min(m.joined_at)
			FROM family_members m
			WHERE m.family_id <> $1
			GROUP BY m.user_id
			ON CONFLICT (family_id, user_id) DO NOTHING`, destID); err != nil {
			return fmt.Errorf("move members: %w", err)
		}

		if _, err := tx.Exec(ctx,
			`UPDATE matches SET family_id = $1 WHERE family_id <> $1`, destID); err != nil {
			return fmt.Errorf("move matches: %w", err)
		}

		// Catalog games (family_id IS NULL) belong to no family and stay put.
		if _, err := tx.Exec(ctx,
			`UPDATE games SET family_id = $1
			 WHERE family_id IS NOT NULL AND family_id <> $1`, destID); err != nil {
			return fmt.Errorf("move games: %w", err)
		}

		if err := collapseDuplicateGames(ctx, tx, destID); err != nil {
			return err
		}

		// Empty by now: their members, matches and games all moved. The cascade
		// still standing behind this is the reason it is last.
		if _, err := tx.Exec(ctx, `DELETE FROM families WHERE id <> $1`, destID); err != nil {
			return fmt.Errorf("delete emptied families: %w", err)
		}

		if _, err := tx.Exec(ctx,
			`UPDATE families SET is_default = true WHERE id = $1 AND NOT is_default`, destID); err != nil {
			return fmt.Errorf("designate default: %w", err)
		}
		return nil
	})
}

// collapseDuplicateGames folds two families' identical custom games — the
// "Rummy" each of them added — into one, so the merged scoreboard does not list
// the same game twice with the record split across them.
//
// Identical means every field a player would use to tell two games apart:
// matching on name alone would merge a 2-player Rummy with a 6-player one and
// quietly rewrite what those matches were.
const duplicateGames = `
	WITH keepers AS (
		SELECT DISTINCT ON (lower(name), team_based, min_players, max_players)
		       id, lower(name) AS key, team_based, min_players, max_players
		FROM games WHERE family_id = $1
		ORDER BY lower(name), team_based, min_players, max_players, created_at, id
	)
	SELECT g.id, k.id FROM games g
	JOIN keepers k ON k.key = lower(g.name)
		AND k.team_based = g.team_based
		AND k.min_players = g.min_players
		AND k.max_players = g.max_players
	WHERE g.family_id = $1 AND g.id <> k.id`

func collapseDuplicateGames(ctx context.Context, tx pgx.Tx, destID string) error {
	rows, err := tx.Query(ctx, duplicateGames, destID)
	if err != nil {
		return fmt.Errorf("find duplicate games: %w", err)
	}

	type merge struct{ from, to string }
	var merges []merge
	for rows.Next() {
		var m merge
		if err := rows.Scan(&m.from, &m.to); err != nil {
			rows.Close()
			return err
		}
		merges = append(merges, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, m := range merges {
		if _, err := tx.Exec(ctx,
			`UPDATE matches SET game_id = $2 WHERE game_id = $1`, m.from, m.to); err != nil {
			return fmt.Errorf("repoint matches at the kept game: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM games WHERE id = $1`, m.from); err != nil {
			return fmt.Errorf("delete duplicate game: %w", err)
		}
	}
	return nil
}
