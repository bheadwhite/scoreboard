package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

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
	q := `SELECT u.id, u.display_name, u.avatar_url, u.created_at, ` + isPlaceholder + `
		FROM users u WHERE u.id = $1`

	var u model.User
	err := s.pool.QueryRow(ctx, q, id).
		Scan(&u.ID, &u.DisplayName, &u.AvatarURL, &u.CreatedAt, &u.Placeholder)
	return u, noRows(err)
}

// UserEmail returns the address a player signed in with.
//
// Separate from GetUser, and not a field on model.User, because model.User maps
// straight to the proto User that travels inside every participant on every
// match. This is only ever used to decide whether the caller runs the service;
// it is never sent to a client.
func (s *Store) UserEmail(ctx context.Context, id string) (string, error) {
	const q = `SELECT email FROM users WHERE id = $1`

	var email string
	err := s.pool.QueryRow(ctx, q, id).Scan(&email)
	return email, noRows(err)
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

// AddPlaceholderPlayer creates a player nobody can sign in to and puts them in
// the family, so a game they were actually in can be recorded with everybody in
// it.
//
// Reuses an existing placeholder of the same name in the same family rather
// than making a second. Two people setting up two games on two phones would
// otherwise produce two of Grandma, each with half her record, and merging
// those afterward is a worse job than not splitting them: the name is the only
// handle anybody has on a player who cannot log in and correct it. Matching is
// case- and space-insensitive for the same reason. Real accounts are never
// matched — somebody who signed up owns their name, and a placeholder must
// never resolve onto them.
//
// provider_id is a fresh uuid rather than empty because
// users_provider_identity_idx is a plain unique index on (provider,
// provider_id): it has to be, since the sign-in upsert names it as an
// ON CONFLICT target, so two placeholders sharing a blank id would collide.
func (s *Store) AddPlaceholderPlayer(ctx context.Context, familyID, displayName string) (model.User, error) {
	var out model.User

	err := s.tx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT u.id, u.display_name, u.avatar_url, u.created_at, true
			FROM users u
			JOIN family_members m ON m.user_id = u.id
			WHERE m.family_id = $1
			  AND u.provider = $2
			  AND lower(btrim(u.display_name)) = lower(btrim($3))
			ORDER BY u.created_at, u.id
			LIMIT 1`, familyID, placeholderProvider, displayName).
			Scan(&out.ID, &out.DisplayName, &out.AvatarURL, &out.CreatedAt, &out.Placeholder)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		if err := tx.QueryRow(ctx, `
			INSERT INTO users (provider, provider_id, email, display_name)
			VALUES ($1, gen_random_uuid()::text, '', $2)
			RETURNING id, display_name, avatar_url, created_at, true`,
			placeholderProvider, displayName).
			Scan(&out.ID, &out.DisplayName, &out.AvatarURL, &out.CreatedAt, &out.Placeholder); err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO family_members (family_id, user_id, role)
			VALUES ($1, $2, 'member')
			ON CONFLICT (family_id, user_id) DO NOTHING`, familyID, out.ID)
		return err
	})

	return out, err
}

// LinkPlaceholder declares that a placeholder and a real account are the same
// person, and makes it so: everything the placeholder ever did is repointed at
// the account, and the placeholder is deleted.
//
// The account's id survives, not the placeholder's. That is not a preference —
// the account id is the one tied to a sign-in and carried in a session cookie,
// so moving it would sign the person out of their own history.
//
// Refused when both appear in the same match. Whichever row were dropped, the
// result would be a guess about something the database cannot settle: either
// somebody was entered twice, or this placeholder is not who the administrator
// thinks it is. The second possibility is the reason to stop rather than pick —
// there is no undo here, and a wrong link silently hands one person another
// person's record.
//
// Everything else is a straight rewrite. It has to reach every reference, not
// just participations: a match the placeholder set up, a claim they were
// recorded as making, a game they added. Anything missed would be a foreign key
// pointing at a row that is about to stop existing.
func (s *Store) LinkPlaceholder(ctx context.Context, placeholderID, userID string) (model.User, int, error) {
	var out model.User
	var moved int

	err := s.tx(ctx, func(tx pgx.Tx) error {
		if placeholderID == userID {
			return fmt.Errorf("%w: that is the same player", ErrConflict)
		}

		var sourceIsPlaceholder bool
		if err := tx.QueryRow(ctx, `SELECT provider = $2 FROM users WHERE id = $1`,
			placeholderID, placeholderProvider).Scan(&sourceIsPlaceholder); err != nil {
			return noRows(err)
		}
		if !sourceIsPlaceholder {
			return fmt.Errorf("%w: that player already has an account of their own", ErrConflict)
		}

		var targetIsPlaceholder bool
		if err := tx.QueryRow(ctx, `SELECT provider = $2 FROM users WHERE id = $1`,
			userID, placeholderProvider).Scan(&targetIsPlaceholder); err != nil {
			return noRows(err)
		}
		if targetIsPlaceholder {
			return fmt.Errorf("%w: link a placeholder to a signed-up account, not to another placeholder", ErrConflict)
		}

		// Both in one match: stop, and say which.
		rows, err := tx.Query(ctx, `
			SELECT a.match_id FROM match_participants a
			JOIN match_participants b ON b.match_id = a.match_id
			WHERE a.user_id = $1 AND b.user_id = $2
			ORDER BY a.match_id`, placeholderID, userID)
		if err != nil {
			return err
		}
		var clashes []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			clashes = append(clashes, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(clashes) > 0 {
			return fmt.Errorf(
				"%w: both are in the same match, so this would enter one person twice — sort out %s first",
				ErrConflict, strings.Join(clashes, ", "))
		}

		tag, err := tx.Exec(ctx,
			`UPDATE match_participants SET user_id = $2 WHERE user_id = $1`, placeholderID, userID)
		if err != nil {
			return err
		}
		moved = int(tag.RowsAffected())

		for _, q := range []string{
			`UPDATE matches SET created_by = $2 WHERE created_by = $1`,
			`UPDATE matches SET claimed_by_user_id = $2 WHERE claimed_by_user_id = $1`,
			`UPDATE games SET created_by = $2 WHERE created_by = $1`,
			`UPDATE families SET owner_id = $2 WHERE owner_id = $1`,
		} {
			if _, err := tx.Exec(ctx, q, placeholderID, userID); err != nil {
				return err
			}
		}

		// Membership moves rather than being rewritten in place: the account may
		// already be in the same family, and (family_id, user_id) is the key.
		if _, err := tx.Exec(ctx, `
			INSERT INTO family_members (family_id, user_id, role, joined_at)
			SELECT family_id, $2, role, joined_at FROM family_members WHERE user_id = $1
			ON CONFLICT (family_id, user_id) DO NOTHING`, placeholderID, userID); err != nil {
			return err
		}

		// The placeholder goes. family_members cascades from here.
		if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, placeholderID); err != nil {
			return err
		}

		return tx.QueryRow(ctx, `
			SELECT u.id, u.display_name, u.avatar_url, u.created_at, `+isPlaceholder+`
			FROM users u WHERE u.id = $1`, userID).
			Scan(&out.ID, &out.DisplayName, &out.AvatarURL, &out.CreatedAt, &out.Placeholder)
	})

	return out, moved, err
}
