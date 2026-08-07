// Package service holds the Connect handlers. Handlers are thin: they
// authenticate, authorize, validate, call the store, and map the result to
// proto. Business rules live in the store and the stats package.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/bheadwhite/scoreboard/backend/internal/auth"
	"github.com/bheadwhite/scoreboard/backend/internal/model"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

// base carries the dependencies every service shares.
type base struct {
	store *store.Store
	// How long a claim stands before outstanding concessions are assumed.
	concedeWindow time.Duration
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

// matchForCaller loads a match and checks the caller is in its family.
func (b base) matchForCaller(ctx context.Context, matchID, userID string) (model.Match, model.Role, error) {
	if matchID == "" {
		return model.Match{}, "", invalid("match_id is required")
	}
	m, err := b.store.GetMatch(ctx, matchID)
	if err != nil {
		return model.Match{}, "", wrapErr(err)
	}
	role, err := b.requireMember(ctx, m.FamilyID, userID)
	if err != nil {
		return model.Match{}, "", err
	}
	return m, role, nil
}
