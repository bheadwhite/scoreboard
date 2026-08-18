// Package service holds the Connect handlers. Handlers are thin: they
// authenticate, authorize, validate, call the store, and map the result to
// proto. Business rules live in the store and the stats package.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/bheadwhite/scoreboard/backend/internal/auth"
	"github.com/bheadwhite/scoreboard/backend/internal/events"
	"github.com/bheadwhite/scoreboard/backend/internal/model"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

// base carries the dependencies every service shares.
type base struct {
	store *store.Store
	// Where handlers announce that something changed, so open browsers can
	// re-read. Nil is allowed: publishing becomes a no-op.
	hub *events.Hub
	// How long a claim stands before outstanding concessions are assumed.
	concedeWindow time.Duration
	// Who runs the deployment. Empty for the services that never ask — a zero
	// set is also the right answer for one that does, since it means nobody has
	// been granted anything.
	admins auth.Admins
}

// viewer is the caller as a response is being written for them: who they are,
// and the two footings that unlock controls an ordinary player does not get.
//
// One value rather than a string and a pair of bools threaded through every
// signature, because "which of these three positional arguments was the
// organizer one" is exactly the mistake that would leak a redacted objection.
type viewer struct {
	id string
	// Runs the family this match belongs to. Settles a genuine deadlock, and
	// sees who objected.
	organizer bool
	// Runs the service. Not a role inside any family, and not something an
	// organizer can grant themselves — see auth.Admins.
	admin bool
}

// caller returns the authenticated user id.
func caller(ctx context.Context) (string, error) {
	id, err := auth.Require(ctx)
	if err != nil {
		return "", err
	}
	return id.ID, nil
}

// requireMember asserts the caller belongs to the family and returns their
// role. A non-member gets NotFound rather than PermissionDenied, so families
// cannot be probed for existence.
func (b base) requireMember(ctx context.Context, familyID, userID string) (model.Role, error) {
	if familyID == "" {
		return "", invalid("family_id is required")
	}
	role, err := b.store.MemberRole(ctx, familyID, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", wrapErr(store.ErrNotFound)
		}
		return "", wrapErr(err)
	}
	return role, nil
}

// requireOwner asserts the caller runs the family.
func (b base) requireOwner(ctx context.Context, familyID, userID string) error {
	role, err := b.requireMember(ctx, familyID, userID)
	if err != nil {
		return err
	}
	if role != model.RoleOwner {
		return denied("only the family organizer can do that")
	}
	return nil
}

// isAdmin reports whether the caller runs the service.
//
// Costs nothing on a deployment that has configured nobody, which is the common
// case and the default — the email only has to be read out of the database when
// there is a list to check it against.
func (b base) isAdmin(ctx context.Context, userID string) (bool, error) {
	if len(b.admins) == 0 {
		return false, nil
	}
	email, err := b.store.UserEmail(ctx, userID)
	if err != nil {
		return false, wrapErr(err)
	}
	return b.admins.Has(email), nil
}

// requireAdmin asserts the caller runs the service.
func (b base) requireAdmin(ctx context.Context, userID string) error {
	admin, err := b.isAdmin(ctx, userID)
	if err != nil {
		return err
	}
	if !admin {
		return denied("only a scoreboard administrator can do that")
	}
	return nil
}

// matchViewer loads a match and works out on what footing the caller is reading
// it. Belonging to the family is required; the other two footings are not.
func (b base) matchViewer(ctx context.Context, matchID string) (model.Match, viewer, error) {
	// Authenticate before touching the database, so an anonymous request cannot
	// probe for match ids.
	if _, err := caller(ctx); err != nil {
		return model.Match{}, viewer{}, err
	}
	if matchID == "" {
		return model.Match{}, viewer{}, invalid("match_id is required")
	}
	m, err := b.store.GetMatch(ctx, matchID)
	if err != nil {
		return model.Match{}, viewer{}, wrapErr(err)
	}
	v, err := b.familyViewer(ctx, m.FamilyID)
	if err != nil {
		return model.Match{}, viewer{}, err
	}
	return m, v, nil
}

// familyViewer is the same for handlers that start from a family rather than
// from one match.
func (b base) familyViewer(ctx context.Context, familyID string) (viewer, error) {
	uid, err := caller(ctx)
	if err != nil {
		return viewer{}, err
	}
	role, err := b.requireMember(ctx, familyID, uid)
	if err != nil {
		return viewer{}, err
	}
	admin, err := b.isAdmin(ctx, uid)
	if err != nil {
		return viewer{}, err
	}
	return viewer{id: uid, organizer: role == model.RoleOwner, admin: admin}, nil
}
