package service

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"

	scoreboardv1 "github.com/bheadwhite/scoreboard/backend/gen/scoreboard/v1"
	"github.com/bheadwhite/scoreboard/backend/internal/model"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

type MatchService struct {
	base
}

func NewMatchService(s *store.Store, concedeWindow time.Duration) *MatchService {
	return &MatchService{base: base{store: s, concedeWindow: concedeWindow}}
}

func (svc *MatchService) CreateMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.CreateMatchRequest],
) (*connect.Response[scoreboardv1.CreateMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	if _, err := svc.requireMember(ctx, familyID, uid); err != nil {
		return nil, err
	}
	if req.Msg.GetGameId() == "" {
		return nil, invalid("pick a game")
	}

	m, err := svc.store.CreateMatch(ctx, familyID, req.Msg.GetGameId(), uid, req.Msg.GetInviteUserIds())
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.CreateMatchResponse{Match: matchPB(m, uid)}), nil
}

func (svc *MatchService) JoinMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.JoinMatchRequest],
) (*connect.Response[scoreboardv1.JoinMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if _, _, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid); err != nil {
		return nil, err
	}

	m, err := svc.store.JoinMatch(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.JoinMatchResponse{Match: matchPB(m, uid)}), nil
}

func (svc *MatchService) LeaveMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.LeaveMatchRequest],
) (*connect.Response[scoreboardv1.LeaveMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if _, _, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid); err != nil {
		return nil, err
	}

	m, err := svc.store.LeaveMatch(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.LeaveMatchResponse{Match: matchPB(m, uid)}), nil
}

// DeclineMatch turns down an invitation. Only the invited player can do this
// for themselves — nobody declines on somebody else's behalf.
func (svc *MatchService) DeclineMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.DeclineMatchRequest],
) (*connect.Response[scoreboardv1.DeclineMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if _, _, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid); err != nil {
		return nil, err
	}

	out, err := svc.store.DeclineMatch(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.DeclineMatchResponse{Match: matchPB(out, uid)}), nil
}

func (svc *MatchService) InvitePlayers(
	ctx context.Context, req *connect.Request[scoreboardv1.InvitePlayersRequest],
) (*connect.Response[scoreboardv1.InvitePlayersResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, _, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	if !isParticipant(m, uid) {
		return nil, denied("only players in this match can invite others")
	}
	if len(req.Msg.GetUserIds()) == 0 {
		return nil, invalid("pick someone to invite")
	}

	out, err := svc.store.InvitePlayers(ctx, req.Msg.GetMatchId(), req.Msg.GetUserIds())
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.InvitePlayersResponse{Match: matchPB(out, uid)}), nil
}

func (svc *MatchService) SetTeams(
	ctx context.Context, req *connect.Request[scoreboardv1.SetTeamsRequest],
) (*connect.Response[scoreboardv1.SetTeamsResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, _, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	if !isParticipant(m, uid) {
		return nil, denied("only players in this match can set the teams")
	}

	teams := make([]store.TeamAssignment, 0, len(req.Msg.GetTeams()))
	for _, t := range req.Msg.GetTeams() {
		teams = append(teams, store.TeamAssignment{
			Name:    strings.TrimSpace(t.GetName()),
			UserIDs: t.GetUserIds(),
		})
	}

	out, err := svc.store.SetTeams(ctx, req.Msg.GetMatchId(), teams)
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.SetTeamsResponse{Match: matchPB(out, uid)}), nil
}

const maxContractDetail = 140

func (svc *MatchService) SetContract(
	ctx context.Context, req *connect.Request[scoreboardv1.SetContractRequest],
) (*connect.Response[scoreboardv1.SetContractResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, _, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	if !isParticipant(m, uid) {
		return nil, denied("only players in this match can set the contract")
	}

	in := req.Msg.GetContract()
	if in == nil {
		return nil, invalid("contract is required")
	}
	kind, ok := contractKindFromPB(in.GetKind())
	if !ok {
		return nil, invalid("pick how this game is won")
	}

	detail := strings.TrimSpace(in.GetDetail())
	if len([]rune(detail)) > maxContractDetail {
		return nil, invalid("keep the contract description short")
	}

	target := in.GetTarget()
	switch kind {
	case model.ContractFirstToScore, model.ContractBestOf:
		if target <= 0 {
			return nil, invalid("set the number to play to")
		}
	case model.ContractCustom:
		if detail == "" {
			return nil, invalid("describe how this game is won")
		}
		target = 0
	default:
		target = 0
	}

	out, err := svc.store.SetContract(ctx, req.Msg.GetMatchId(), model.Contract{
		Kind: kind, Target: target, Detail: detail,
	})
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.SetContractResponse{Match: matchPB(out, uid)}), nil
}

func (svc *MatchService) StartMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.StartMatchRequest],
) (*connect.Response[scoreboardv1.StartMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, _, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	if !isParticipant(m, uid) {
		return nil, denied("only players in this match can start it")
	}

	out, err := svc.store.StartMatch(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.StartMatchResponse{Match: matchPB(out, uid)}), nil
}

// ClaimVictory starts the concession clock. Everything downstream of this — the
// concessions, the timeout, the eventual place in the record — is the point of
// the app, so it is deliberately not a plain "record the score" call.
func (svc *MatchService) ClaimVictory(
	ctx context.Context, req *connect.Request[scoreboardv1.ClaimVictoryRequest],
) (*connect.Response[scoreboardv1.ClaimVictoryResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, _, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	if !isParticipant(m, uid) {
		return nil, denied("only players in this match can claim it")
	}

	out, err := svc.store.ClaimVictory(ctx, req.Msg.GetMatchId(), uid, req.Msg.GetTeamId(), svc.concedeWindow)
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.ClaimVictoryResponse{Match: matchPB(out, uid)}), nil
}

func (svc *MatchService) Concede(
	ctx context.Context, req *connect.Request[scoreboardv1.ConcedeRequest],
) (*connect.Response[scoreboardv1.ConcedeResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if _, _, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid); err != nil {
		return nil, err
	}

	out, err := svc.store.Concede(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.ConcedeResponse{Match: matchPB(out, uid)}), nil
}

const maxDisputeReason = 280

func (svc *MatchService) DisputeMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.DisputeMatchRequest],
) (*connect.Response[scoreboardv1.DisputeMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if _, _, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid); err != nil {
		return nil, err
	}
	if len([]rune(req.Msg.GetReason())) > maxDisputeReason {
		return nil, invalid("keep the explanation short")
	}

	out, err := svc.store.Dispute(ctx, req.Msg.GetMatchId(), uid, req.Msg.GetReason())
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.DisputeMatchResponse{Match: matchPB(out, uid)}), nil
}

func (svc *MatchService) ResolveDispute(
	ctx context.Context, req *connect.Request[scoreboardv1.ResolveDisputeRequest],
) (*connect.Response[scoreboardv1.ResolveDisputeResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, _, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	if err := svc.requireOwner(ctx, m.FamilyID, uid); err != nil {
		return nil, err
	}
	if !req.Msg.GetVoidMatch() && req.Msg.GetWinningTeamId() == "" {
		return nil, invalid("name the winning team, or void the match")
	}

	out, err := svc.store.ResolveDispute(ctx, req.Msg.GetMatchId(), req.Msg.GetWinningTeamId(), req.Msg.GetVoidMatch())
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.ResolveDisputeResponse{Match: matchPB(out, uid)}), nil
}

// CancelMatch abandons a match. Anyone in it can cancel while it is still a
// lobby; after that it takes the family organizer.
func (svc *MatchService) CancelMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.CancelMatchRequest],
) (*connect.Response[scoreboardv1.CancelMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	if m.Status != model.StatusLobby && role != model.RoleOwner {
		return nil, denied("only the family organizer can cancel a match in play")
	}
	if m.Status == model.StatusLobby && !isParticipant(m, uid) && role != model.RoleOwner {
		return nil, denied("only players in this match can cancel it")
	}

	out, err := svc.store.CancelMatch(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.CancelMatchResponse{Match: matchPB(out, uid)}), nil
}

func (svc *MatchService) GetMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.GetMatchRequest],
) (*connect.Response[scoreboardv1.GetMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, _, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&scoreboardv1.GetMatchResponse{Match: matchPB(m, uid)}), nil
}

func (svc *MatchService) ListMatches(
	ctx context.Context, req *connect.Request[scoreboardv1.ListMatchesRequest],
) (*connect.Response[scoreboardv1.ListMatchesResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	if _, err := svc.requireMember(ctx, familyID, uid); err != nil {
		return nil, err
	}

	statuses := make([]string, 0, len(req.Msg.GetStatuses()))
	for _, s := range req.Msg.GetStatuses() {
		v, ok := statusFromPB(s)
		if !ok {
			return nil, invalid("unknown match status")
		}
		statuses = append(statuses, string(v))
	}

	matches, err := svc.store.ListMatches(ctx, store.ListMatchesFilter{
		FamilyID: familyID,
		Statuses: statuses,
		UserID:   req.Msg.GetUserId(),
		Limit:    req.Msg.GetLimit(),
		Offset:   req.Msg.GetOffset(),
	})
	if err != nil {
		return nil, wrapErr(err)
	}
	return connect.NewResponse(&scoreboardv1.ListMatchesResponse{Matches: matchesPB(matches, uid)}), nil
}

// isParticipant reports whether the caller actually joined. Holding an
// unanswered invitation does not make you a player.
func isParticipant(m model.Match, userID string) bool {
	for _, p := range m.JoinedParticipants() {
		if p.User.ID == userID {
			return true
		}
	}
	return false
}
