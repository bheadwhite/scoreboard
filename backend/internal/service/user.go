package service

import (
	"context"
	"log"
	"strings"

	"connectrpc.com/connect"

	scoreboardv1 "github.com/bheadwhite/scoreboard/backend/gen/scoreboard/v1"
	"github.com/bheadwhite/scoreboard/backend/internal/auth"
	"github.com/bheadwhite/scoreboard/backend/internal/events"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

type UserService struct {
	base
}

// The admin list is passed in because LinkPlaceholder is gated on it, the same
// way VoidMatch is. Both rewrite the permanent record, and neither is a family
// role.
func NewUserService(s *store.Store, hub *events.Hub, admins auth.Admins) *UserService {
	return &UserService{base: base{store: s, hub: hub, admins: admins}}
}

func (svc *UserService) GetMe(
	ctx context.Context, _ *connect.Request[scoreboardv1.GetMeRequest],
) (*connect.Response[scoreboardv1.GetMeResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	u, err := svc.store.GetUser(ctx, uid)
	if err != nil {
		return nil, wrapErr(err)
	}
	// Whether they run the service, which is the one capability the client is
	// told about outside a particular match. An error here is not fatal: failing
	// to confirm somebody is an administrator means they are shown the ordinary
	// app, which is the safe way to be wrong.
	admin := svc.requireAdmin(ctx, uid) == nil
	return connect.NewResponse(&scoreboardv1.GetMeResponse{User: userPB(u), Admin: admin}), nil
}

const maxDisplayName = 40

func (svc *UserService) UpdateProfile(
	ctx context.Context, req *connect.Request[scoreboardv1.UpdateProfileRequest],
) (*connect.Response[scoreboardv1.UpdateProfileResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Msg.GetDisplayName())
	if name == "" {
		return nil, invalid("display name is required")
	}
	if len([]rune(name)) > maxDisplayName {
		return nil, invalid("display name is too long")
	}

	u, err := svc.store.UpdateProfile(ctx, uid, name, strings.TrimSpace(req.Msg.GetAvatarUrl()))
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.UpdateProfileResponse{User: userPB(u)}), nil
}

// LinkPlaceholder declares that a placeholder and a real account are the same
// person. Administrators only.
//
// The same gate as VoidMatch, and for the same reason: this rewrites the
// permanent record. It is a gentler operation than voiding — nothing is
// destroyed, a history moves from a player who cannot speak for themselves to
// the person it belonged to all along — but it moves wins and losses between
// names, and a family organizer being able to do that to the people they play
// against is exactly the shape of thing this app keeps out of family roles.
//
// Ungated on family membership, like VoidMatch: running the deployment is the
// authorization, and requiring an administrator to join a household in order to
// tidy up after it is worse for everyone.
func (svc *UserService) LinkPlaceholder(
	ctx context.Context, req *connect.Request[scoreboardv1.LinkPlaceholderRequest],
) (*connect.Response[scoreboardv1.LinkPlaceholderResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if err := svc.requireAdmin(ctx, uid); err != nil {
		return nil, err
	}
	if req.Msg.GetPlaceholderId() == "" || req.Msg.GetUserId() == "" {
		return nil, invalid("name both players")
	}

	// Read the placeholder before it stops existing, so the log can say who this
	// was. Nothing else records it afterward.
	was, err := svc.store.GetUser(ctx, req.Msg.GetPlaceholderId())
	if err != nil {
		return nil, wrapErr(err)
	}

	u, moved, err := svc.store.LinkPlaceholder(ctx, req.Msg.GetPlaceholderId(), req.Msg.GetUserId())
	if err != nil {
		return nil, wrapErr(err)
	}

	log.Printf("link: admin %s merged placeholder %s (%q) into user %s (%q), %d participations moved",
		uid, was.ID, was.DisplayName, u.ID, u.DisplayName, moved)

	// Everything derived from a participation just changed owner: the standings,
	// every head-to-head record involving either name, two profiles. Announced
	// on every family the surviving account is now in — which, after the merge,
	// is a superset of the ones the placeholder was in, since their membership
	// moved across before they were deleted.
	families, err := svc.store.ListFamiliesForUser(ctx, u.ID)
	if err != nil {
		return nil, wrapErr(err)
	}
	for _, f := range families {
		svc.publish(f.ID, events.KindFamily, "")
	}

	return connect.NewResponse(&scoreboardv1.LinkPlaceholderResponse{
		User:         userPB(u),
		MatchesMoved: int32(moved),
	}), nil
}
