package store

import (
	"context"
	"fmt"
	"time"

	"github.com/bheadwhite/scoreboard/backend/internal/model"
	"github.com/bheadwhite/scoreboard/backend/internal/stats"
)

// LoadParticipations reads a family's finalized match history: one row per
// player per match. Everything on the scoreboard is computed from this by the
// stats package.
//
// Contested matches come back too, flagged rather than filtered. The stats
// package decides what to do with them — it holds them out of every number and
// counts them separately — and it needs to see them to count them. Doing the
// filtering here instead would make a contested result indistinguishable from
// one that never happened, and "plus 2 contested" is the entire point of
// keeping it.
//
// A match is contested when any joined player has an objection standing on it.
// That mirrors model.Match.Objectors exactly, and it is the one place in the app
// where that question is asked in SQL rather than in Go, so the two have to be
// read together. Any player, including one on the winning side: a settled result
// its supposed winner denies is contested by the loudest possible margin.
// Nothing stale can leak in, because ClaimVictory wipes every answer before
// recording a new claim.
//
// gameID narrows to a single game; zero since means all time.
func (s *Store) LoadParticipations(ctx context.Context, familyID, gameID string, since time.Time) ([]stats.Participation, error) {
	// Only players who joined and were given an outcome count. Someone who
	// declined the invitation, or never answered it, has a 'pending' row on the
	// match and must not be handed a loss for a game they did not play.
	q := `
		SELECT p.match_id, p.user_id, coalesce(p.team_id::text, ''), m.game_id, p.result,
		       (extract(epoch FROM m.finalized_at) * 1000000)::bigint,
		       EXISTS (SELECT 1 FROM match_participants o
		               WHERE o.match_id = m.id
		                 AND o.status = 'joined'
		                 AND o.disputed_at IS NOT NULL)
		FROM match_participants p
		JOIN matches m ON m.id = p.match_id
		WHERE m.family_id = $1 AND m.status = 'final' AND m.finalized_at IS NOT NULL
		  AND p.status = 'joined' AND p.result IN ('win', 'loss')`
	args := []any{familyID}

	if gameID != "" {
		args = append(args, gameID)
		q += fmt.Sprintf(` AND m.game_id = $%d`, len(args))
	}
	if !since.IsZero() {
		args = append(args, since)
		q += fmt.Sprintf(` AND m.finalized_at >= $%d`, len(args))
	}

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []stats.Participation
	for rows.Next() {
		var p stats.Participation
		if err := rows.Scan(&p.MatchID, &p.UserID, &p.TeamID, &p.GameID, &p.Result,
			&p.Seq, &p.Contested); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetMatchesByIDs loads full matches for the given ids, newest first. Used for
// the list of meetings on the head-to-head screen.
func (s *Store) GetMatchesByIDs(ctx context.Context, ids []string) ([]model.Match, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT `+matchColumns+` FROM matches m WHERE m.id = ANY($1) ORDER BY m.finalized_at DESC`, ids)
	if err != nil {
		return nil, err
	}

	var out []model.Match
	for rows.Next() {
		m, err := scanMatch(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := hydrateAll(ctx, s.pool, out); err != nil {
		return nil, err
	}
	return out, nil
}
