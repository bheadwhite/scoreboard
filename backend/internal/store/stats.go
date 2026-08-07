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
// gameID narrows to a single game; zero since means all time.
func (s *Store) LoadParticipations(ctx context.Context, familyID, gameID string, since time.Time) ([]stats.Participation, error) {
	q := `
		SELECT p.match_id, p.user_id, coalesce(p.team_id::text, ''), m.game_id, p.result,
		       (extract(epoch FROM m.finalized_at) * 1000000)::bigint
		FROM match_participants p
		JOIN matches m ON m.id = p.match_id
		WHERE m.family_id = $1 AND m.status = 'final' AND m.finalized_at IS NOT NULL`
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
		if err := rows.Scan(&p.MatchID, &p.UserID, &p.TeamID, &p.GameID, &p.Result, &p.Seq); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetMatchesByIDs loads full matches for the given ids, newest first. Used for
// the recent-meetings list on the head-to-head screen.
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

	for i := range out {
		if err := hydrate(ctx, s.pool, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
