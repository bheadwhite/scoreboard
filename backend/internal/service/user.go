package service

import (
	"context"
	"strings"

	"connectrpc.com/connect"

	scoreboardv1 "github.com/bheadwhite/scoreboard/backend/gen/scoreboard/v1"
	"github.com/bheadwhite/scoreboard/backend/internal/events"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

type UserService struct {
	base
}

func NewUserService(s *store.Store, hub *events.Hub) *UserService {
	return &UserService{base: base{store: s, hub: hub}}
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
	return connect.NewResponse(&scoreboardv1.GetMeResponse{User: userPB(u)}), nil
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
