package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/bheadwhite/scoreboard/backend/internal/model"
)

// Invite codes are read aloud across a kitchen table, so the alphabet drops
// characters that get misheard or misread: 0/O, 1/I/L, 5/S, 2/Z.
const inviteAlphabet = "ABCDEFGHJKMNPQRTUVWXY346789"

const inviteCodeLen = 6

func newInviteCode() (string, error) {
	buf := make([]byte, inviteCodeLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, inviteCodeLen)
	for i, b := range buf {
		out[i] = inviteAlphabet[int(b)%len(inviteAlphabet)]
	}
	return string(out), nil
}

// CreateFamily creates a family with a unique invite code and makes the creator
// its owner.
func (s *Store) CreateFamily(ctx context.Context, ownerID, name string) (model.Family, error) {
	var f model.Family
	err := s.tx(ctx, func(tx pgx.Tx) error {
		var err error
		f, err = insertFamily(ctx, tx, ownerID, name, false)
		return err
	})
	return f, err
}

// insertFamily writes the family row, its invite code and the owner's
// membership. Shared by CreateFamily and by the sign-in path that mints the one
// family on an empty database, so both end up with the same shape of row.
//
// It retries on the astronomically unlikely code collision rather than failing
// the caller.
func insertFamily(ctx context.Context, tx pgx.Tx, ownerID, name string, isDefault bool) (model.Family, error) {
	var f model.Family

	for attempt := 0; attempt < 5; attempt++ {
		code, err := newInviteCode()
		if err != nil {
			return f, err
		}

		const q = `
			INSERT INTO families (name, invite_code, owner_id, is_default)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (invite_code) DO NOTHING
			RETURNING id, name, invite_code, owner_id, created_at`

		err = tx.QueryRow(ctx, q, name, code, ownerID, isDefault).
			Scan(&f.ID, &f.Name, &f.InviteCode, &f.OwnerID, &f.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // code taken, try another
		}
		if err != nil {
			return f, err
		}

		const member = `
			INSERT INTO family_members (family_id, user_id, role)
			VALUES ($1, $2, 'owner')`
		if _, err := tx.Exec(ctx, member, f.ID, ownerID); err != nil {
			return f, err
		}

		f.ViewerRole = model.RoleOwner
		f.MemberCount = 1
		return f, nil
	}
	return f, fmt.Errorf("could not allocate a unique invite code")
}

// JoinFamily adds a user to the family holding the given code. Joining twice is
// a no-op rather than an error, so a double-tapped invite link still works.
func (s *Store) JoinFamily(ctx context.Context, userID, code string) (model.Family, error) {
	code = strings.ToUpper(strings.TrimSpace(code))

	var f model.Family
	err := s.tx(ctx, func(tx pgx.Tx) error {
		const find = `
			SELECT id, name, invite_code, owner_id, created_at
			FROM families WHERE invite_code = $1`
		if err := tx.QueryRow(ctx, find, code).
			Scan(&f.ID, &f.Name, &f.InviteCode, &f.OwnerID, &f.CreatedAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// A bare "not found" is useless on the one screen where people
				// arrive from a shared link and cannot tell whether they
				// mistyped, the code was rotated, or the link is stale.
				return fmt.Errorf("%w: that invite code does not match any family — it may have been changed since the link was shared", ErrNotFound)
			}
			return err
		}

		const join = `
			INSERT INTO family_members (family_id, user_id, role)
			VALUES ($1, $2, 'member')
			ON CONFLICT (family_id, user_id) DO NOTHING`
		if _, err := tx.Exec(ctx, join, f.ID, userID); err != nil {
			return err
		}

		return tx.QueryRow(ctx,
			`SELECT role, (SELECT count(*) FROM family_members WHERE family_id = $1)
			 FROM family_members WHERE family_id = $1 AND user_id = $2`,
			f.ID, userID).Scan(&f.ViewerRole, &f.MemberCount)
	})

	return f, err
}

// InvitePreview is what an invite code reveals before joining.
type InvitePreview struct {
	Name          string
	MemberCount   int
	OwnerName     string
	AlreadyMember bool
}

// PreviewInvite resolves a code to the family it names, without joining.
//
// It returns only what somebody needs to recognise what they are being asked
// to join. Guessing codes therefore yields a family name and nothing else — no
// id to act on, no roster to harvest.
func (s *Store) PreviewInvite(ctx context.Context, code, viewerID string) (InvitePreview, error) {
	code = strings.ToUpper(strings.TrimSpace(code))

	const q = `
		SELECT f.name,
		       (SELECT count(*) FROM family_members m WHERE m.family_id = f.id),
		       coalesce(o.display_name, ''),
		       EXISTS (SELECT 1 FROM family_members m WHERE m.family_id = f.id AND m.user_id = $2)
		FROM families f
		LEFT JOIN users o ON o.id = f.owner_id
		WHERE f.invite_code = $1`

	var p InvitePreview
	err := s.pool.QueryRow(ctx, q, code, viewerID).
		Scan(&p.Name, &p.MemberCount, &p.OwnerName, &p.AlreadyMember)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, fmt.Errorf("%w: that invite code does not match any family — it may have been changed since the link was shared", ErrNotFound)
	}
	return p, err
}

func (s *Store) ListFamiliesForUser(ctx context.Context, userID string) ([]model.Family, error) {
	const q = `
		SELECT f.id, f.name, f.invite_code, f.owner_id, f.created_at, m.role,
		       (SELECT count(*) FROM family_members x WHERE x.family_id = f.id)
		FROM families f
		JOIN family_members m ON m.family_id = f.id
		WHERE m.user_id = $1
		ORDER BY f.created_at`

	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Family
	for rows.Next() {
		var f model.Family
		if err := rows.Scan(&f.ID, &f.Name, &f.InviteCode, &f.OwnerID,
			&f.CreatedAt, &f.ViewerRole, &f.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFamily loads a family as seen by viewerID. It returns ErrNotFound when the
// viewer is not a member, so non-members cannot probe for families.
func (s *Store) GetFamily(ctx context.Context, familyID, viewerID string) (model.Family, error) {
	const q = `
		SELECT f.id, f.name, f.invite_code, f.owner_id, f.created_at, m.role,
		       (SELECT count(*) FROM family_members x WHERE x.family_id = f.id)
		FROM families f
		JOIN family_members m ON m.family_id = f.id AND m.user_id = $2
		WHERE f.id = $1`

	var f model.Family
	err := s.pool.QueryRow(ctx, q, familyID, viewerID).
		Scan(&f.ID, &f.Name, &f.InviteCode, &f.OwnerID, &f.CreatedAt, &f.ViewerRole, &f.MemberCount)
	return f, noRows(err)
}

// MemberRole returns a user's role in a family, or ErrNotFound if they are not
// a member. This is the authorization check every other service calls.
func (s *Store) MemberRole(ctx context.Context, familyID, userID string) (model.Role, error) {
	const q = `SELECT role FROM family_members WHERE family_id = $1 AND user_id = $2`

	var role model.Role
	err := s.pool.QueryRow(ctx, q, familyID, userID).Scan(&role)
	return role, noRows(err)
}

func (s *Store) ListMembers(ctx context.Context, familyID string) ([]model.FamilyMember, error) {
	const q = `
		SELECT u.id, u.display_name, u.avatar_url, u.created_at, m.role, m.joined_at
		FROM family_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.family_id = $1
		ORDER BY m.joined_at`

	rows, err := s.pool.Query(ctx, q, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.FamilyMember
	for rows.Next() {
		var m model.FamilyMember
		if err := rows.Scan(&m.User.ID, &m.User.DisplayName, &m.User.AvatarURL,
			&m.User.CreatedAt, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// RemoveMember drops a member. The owner cannot be removed — ownership would
// have to be transferred first, which the app does not support yet.
func (s *Store) RemoveMember(ctx context.Context, familyID, userID string) error {
	const q = `
		DELETE FROM family_members
		WHERE family_id = $1 AND user_id = $2
		  AND user_id <> (SELECT owner_id FROM families WHERE id = $1)`

	tag, err := s.pool.Exec(ctx, q, familyID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: cannot remove the family owner", ErrConflict)
	}
	return nil
}

func (s *Store) RegenerateInviteCode(ctx context.Context, familyID string) (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		code, err := newInviteCode()
		if err != nil {
			return "", err
		}

		const q = `
			UPDATE families SET invite_code = $2 WHERE id = $1
			  AND NOT EXISTS (SELECT 1 FROM families WHERE invite_code = $2)
			RETURNING invite_code`

		var out string
		err = s.pool.QueryRow(ctx, q, familyID, code).Scan(&out)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", err
		}
		return out, nil
	}
	return "", fmt.Errorf("could not allocate a unique invite code")
}

// ── The one family ───────────────────────────────────────────────────────────
//
// The app is single-family: the UI never names a family, never offers to create
// or join one, and signing in puts you in the one that exists. Rows are still
// scoped by family_id — that stayed, because collapsing it would rewrite every
// match and every stats query for no gain — so exactly one family carries the
// is_default flag and everything lands in it.

// DefaultFamily returns the family the app is. ErrNotFound means nobody has
// ever signed in, which is only true of a brand new database.
func (s *Store) DefaultFamily(ctx context.Context) (model.Family, error) {
	const q = `
		SELECT f.id, f.name, f.invite_code, f.owner_id, f.created_at,
		       (SELECT count(*) FROM family_members x WHERE x.family_id = f.id)
		FROM families f WHERE f.is_default`

	var f model.Family
	err := s.pool.QueryRow(ctx, q).
		Scan(&f.ID, &f.Name, &f.InviteCode, &f.OwnerID, &f.CreatedAt, &f.MemberCount)
	return f, noRows(err)
}

// EnsureDefaultMembership puts a user in the one family, creating it if there
// is not one yet. Called on every sign-in, which is what makes onboarding
// unnecessary: there is no screen to choose a family on because there is no
// choice to make.
//
// On an empty database the first person to sign in creates the family and owns
// it — the same bargain as before, minus the form. name is only ever read in
// that case.
//
// Joining twice is a no-op, so this is safe to run on every sign-in forever.
func (s *Store) EnsureDefaultMembership(ctx context.Context, userID, name string) (model.Family, error) {
	var f model.Family

	err := s.tx(ctx, func(tx pgx.Tx) error {
		// Two people signing in for the first time on an empty database would
		// otherwise race to create the family, and the partial unique index
		// would fail one of their sign-ins. The lock is held to commit and
		// costs one round trip on a path that already does several.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('scoreboard.default_family'))`); err != nil {
			return err
		}

		const find = `
			SELECT id, name, invite_code, owner_id, created_at
			FROM families WHERE is_default`

		err := tx.QueryRow(ctx, find).
			Scan(&f.ID, &f.Name, &f.InviteCode, &f.OwnerID, &f.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			f, err = insertFamily(ctx, tx, userID, name, true)
			return err
		}
		if err != nil {
			return err
		}

		const join = `
			INSERT INTO family_members (family_id, user_id, role)
			VALUES ($1, $2, 'member')
			ON CONFLICT (family_id, user_id) DO NOTHING`
		_, err = tx.Exec(ctx, join, f.ID, userID)
		return err
	})

	return f, err
}

// SetDefaultFamily moves the designation, which only cmd/consolidate does. The
// clear has to happen first: the database allows exactly one.
func (s *Store) SetDefaultFamily(ctx context.Context, familyID string) error {
	return s.tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE families SET is_default = false WHERE is_default AND id <> $1`, familyID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE families SET is_default = true WHERE id = $1`, familyID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}
