package store

import (
	"context"

	"github.com/bheadwhite/scoreboard/backend/internal/model"
)

// ListGames returns the built-in catalog plus the family's own games, catalog
// first and alphabetical within each group.
func (s *Store) ListGames(ctx context.Context, familyID string) ([]model.Game, error) {
	const q = `
		SELECT id, family_id, name, emoji, team_based, min_players, max_players, created_at
		FROM games
		WHERE family_id IS NULL OR family_id = $1
		ORDER BY (family_id IS NOT NULL), name`

	rows, err := s.pool.Query(ctx, q, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanGames(rows)
}

func (s *Store) GetGame(ctx context.Context, id string) (model.Game, error) {
	const q = `
		SELECT id, family_id, name, emoji, team_based, min_players, max_players, created_at
		FROM games WHERE id = $1`

	var g model.Game
	err := s.pool.QueryRow(ctx, q, id).Scan(&g.ID, &g.FamilyID, &g.Name, &g.Emoji,
		&g.TeamBased, &g.MinPlayers, &g.MaxPlayers, &g.CreatedAt)
	return g, noRows(err)
}

func (s *Store) CreateGame(ctx context.Context, familyID, createdBy string, g model.Game) (model.Game, error) {
	const q = `
		INSERT INTO games (family_id, name, emoji, team_based, min_players, max_players, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, family_id, name, emoji, team_based, min_players, max_players, created_at`

	var out model.Game
	err := s.pool.QueryRow(ctx, q, familyID, g.Name, g.Emoji, g.TeamBased,
		g.MinPlayers, g.MaxPlayers, createdBy).
		Scan(&out.ID, &out.FamilyID, &out.Name, &out.Emoji, &out.TeamBased,
			&out.MinPlayers, &out.MaxPlayers, &out.CreatedAt)
	return out, noRows(err)
}

type gameScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanGames(rows gameScanner) ([]model.Game, error) {
	var out []model.Game
	for rows.Next() {
		var g model.Game
		if err := rows.Scan(&g.ID, &g.FamilyID, &g.Name, &g.Emoji, &g.TeamBased,
			&g.MinPlayers, &g.MaxPlayers, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
