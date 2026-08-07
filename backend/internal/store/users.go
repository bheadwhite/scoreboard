package store

import (
	"context"

	"github.com/bheadwhite/scoreboard/backend/internal/model"
)

// UpsertOAuthUser creates or refreshes the account behind a provider identity
// and returns it. Called once per sign-in.
//
// The provider's name and picture only ever seed a new row. Once a player has
// edited their own profile, a later Google sign-in must not quietly overwrite
// the name they chose — which is why those two columns are conditional while
// email is not.
func (s *Store) UpsertOAuthUser(ctx context.Context, provider, providerID, email, displayName, avatarURL string) (model.User, error) {
	const q = `
		INSERT INTO users (provider, provider_id, email, display_name, avatar_url, last_login)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (provider, provider_id) DO UPDATE SET
			email        = EXCLUDED.email,
			last_login   = now(),
			display_name = CASE WHEN users.display_name = '' THEN EXCLUDED.display_name ELSE users.display_name END,
			avatar_url   = CASE WHEN users.avatar_url   = '' THEN EXCLUDED.avatar_url   ELSE users.avatar_url   END
		RETURNING id, display_name, avatar_url, created_at`

	var u model.User
	err := s.pool.QueryRow(ctx, q, provider, providerID, email, displayName, avatarURL).
		Scan(&u.ID, &u.DisplayName, &u.AvatarURL, &u.CreatedAt)
	return u, noRows(err)
}

func (s *Store) GetUser(ctx context.Context, id string) (model.User, error) {
	const q = `SELECT id, display_name, avatar_url, created_at FROM users WHERE id = $1`

	var u model.User
	err := s.pool.QueryRow(ctx, q, id).Scan(&u.ID, &u.DisplayName, &u.AvatarURL, &u.CreatedAt)
	return u, noRows(err)
}

func (s *Store) UpdateProfile(ctx context.Context, id, displayName, avatarURL string) (model.User, error) {
	const q = `
		UPDATE users SET display_name = $2, avatar_url = $3
		WHERE id = $1
		RETURNING id, display_name, avatar_url, created_at`

	var u model.User
	err := s.pool.QueryRow(ctx, q, id, displayName, avatarURL).
		Scan(&u.ID, &u.DisplayName, &u.AvatarURL, &u.CreatedAt)
	return u, noRows(err)
}
