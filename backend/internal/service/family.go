package service

import (
	"context"
	"strings"

	"connectrpc.com/connect"

	scoreboardv1 "github.com/bheadwhite/scoreboard/backend/gen/scoreboard/v1"
	"github.com/bheadwhite/scoreboard/backend/internal/events"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

type FamilyService struct {
	base
}

func NewFamilyService(s *store.Store, hub *events.Hub) *FamilyService {
	return &FamilyService{base: base{store: s, hub: hub}}
}

const maxFamilyName = 60

func (svc *FamilyService) CreateFamily(
	ctx context.Context, req *connect.Request[scoreboardv1.CreateFamilyRequest],
) (*connect.Response[scoreboardv1.CreateFamilyResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Msg.GetName())
	if name == "" {
		return nil, invalid("give your family a name")
	}
	if len([]rune(name)) > maxFamilyName {
		return nil, invalid("that name is too long")
	}

	f, err := svc.store.CreateFamily(ctx, uid, name)
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.CreateFamilyResponse{Family: familyPB(f)}), nil
}

func (svc *FamilyService) JoinFamily(
	ctx context.Context, req *connect.Request[scoreboardv1.JoinFamilyRequest],
) (*connect.Response[scoreboardv1.JoinFamilyResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	code := strings.TrimSpace(req.Msg.GetInviteCode())
	if code == "" {
		return nil, invalid("enter an invite code")
	}

	f, err := svc.store.JoinFamily(ctx, uid, code)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(f.ID, events.KindFamily, "")
	return connect.NewResponse(&scoreboardv1.JoinFamilyResponse{Family: familyPB(f)}), nil
}

// PreviewInvite tells a signed-in caller what a code refers to. Sign-in is
// required so this cannot be scraped anonymously, and the response carries no
// family id — knowing the name does not let you act on it.
func (svc *FamilyService) PreviewInvite(
	ctx context.Context, req *connect.Request[scoreboardv1.PreviewInviteRequest],
) (*connect.Response[scoreboardv1.PreviewInviteResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	code := strings.TrimSpace(req.Msg.GetInviteCode())
	if code == "" {
		return nil, invalid("enter an invite code")
	}

	p, err := svc.store.PreviewInvite(ctx, code, uid)
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.PreviewInviteResponse{
		Name:          p.Name,
		MemberCount:   int32(p.MemberCount),
		OwnerName:     p.OwnerName,
		AlreadyMember: p.AlreadyMember,
	}), nil
}

func (svc *FamilyService) ListMyFamilies(
	ctx context.Context, _ *connect.Request[scoreboardv1.ListMyFamiliesRequest],
) (*connect.Response[scoreboardv1.ListMyFamiliesResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	families, err := svc.store.ListFamiliesForUser(ctx, uid)
	if err != nil {
		return nil, wrapErr(err)
	}

	out := make([]*scoreboardv1.Family, 0, len(families))
	for _, f := range families {
		out = append(out, familyPB(f))
	}
	return connect.NewResponse(&scoreboardv1.ListMyFamiliesResponse{Families: out}), nil
}

func (svc *FamilyService) GetFamily(
	ctx context.Context, req *connect.Request[scoreboardv1.GetFamilyRequest],
) (*connect.Response[scoreboardv1.GetFamilyResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	f, err := svc.store.GetFamily(ctx, req.Msg.GetFamilyId(), uid)
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.GetFamilyResponse{Family: familyPB(f)}), nil
}

func (svc *FamilyService) ListMembers(
	ctx context.Context, req *connect.Request[scoreboardv1.ListMembersRequest],
) (*connect.Response[scoreboardv1.ListMembersResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	if _, err := svc.requireMember(ctx, familyID, uid); err != nil {
		return nil, err
	}

	members, err := svc.store.ListMembers(ctx, familyID)
	if err != nil {
		return nil, wrapErr(err)
	}

	out := make([]*scoreboardv1.FamilyMember, 0, len(members))
	for _, m := range members {
		out = append(out, memberPB(m))
	}
	return connect.NewResponse(&scoreboardv1.ListMembersResponse{Members: out}), nil
}

func (svc *FamilyService) LeaveFamily(
	ctx context.Context, req *connect.Request[scoreboardv1.LeaveFamilyRequest],
) (*connect.Response[scoreboardv1.LeaveFamilyResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	if _, err := svc.requireMember(ctx, familyID, uid); err != nil {
		return nil, err
	}
	if err := svc.store.RemoveMember(ctx, familyID, uid); err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(familyID, events.KindFamily, "")
	return connect.NewResponse(&scoreboardv1.LeaveFamilyResponse{}), nil
}

func (svc *FamilyService) RemoveMember(
	ctx context.Context, req *connect.Request[scoreboardv1.RemoveMemberRequest],
) (*connect.Response[scoreboardv1.RemoveMemberResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	if err := svc.requireOwner(ctx, familyID, uid); err != nil {
		return nil, err
	}
	if req.Msg.GetUserId() == "" {
		return nil, invalid("user_id is required")
	}
	if err := svc.store.RemoveMember(ctx, familyID, req.Msg.GetUserId()); err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(familyID, events.KindFamily, "")
	return connect.NewResponse(&scoreboardv1.RemoveMemberResponse{}), nil
}

func (svc *FamilyService) RegenerateInviteCode(
	ctx context.Context, req *connect.Request[scoreboardv1.RegenerateInviteCodeRequest],
) (*connect.Response[scoreboardv1.RegenerateInviteCodeResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	if err := svc.requireOwner(ctx, familyID, uid); err != nil {
		return nil, err
	}

	code, err := svc.store.RegenerateInviteCode(ctx, familyID)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(familyID, events.KindFamily, "")
	return connect.NewResponse(&scoreboardv1.RegenerateInviteCodeResponse{InviteCode: code}), nil
}
