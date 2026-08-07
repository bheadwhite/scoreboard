package service

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"

	scoreboardv1 "github.com/bheadwhite/scoreboard/backend/gen/scoreboard/v1"
	"github.com/bheadwhite/scoreboard/backend/internal/events"
	"github.com/bheadwhite/scoreboard/backend/internal/model"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

type MatchService struct {
	base
}

func NewMatchService(s *store.Store, hub *events.Hub, concedeWindow time.Duration) *MatchService {
	return &MatchService{base: base{store: s, hub: hub, concedeWindow: concedeWindow}}
}

func (svc *MatchService) CreateMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.CreateMatchRequest],
) (*connect.Response[scoreboardv1.CreateMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	role, err := svc.requireMember(ctx, familyID, uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner
	if req.Msg.GetGameId() == "" {
		return nil, invalid("pick a game")
	}

	m, err := svc.store.CreateMatch(ctx, familyID, req.Msg.GetGameId(), uid, req.Msg.GetInviteUserIds())
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(m.FamilyID, events.KindMatch, m.ID)
	return connect.NewResponse(&scoreboardv1.CreateMatchResponse{Match: svc.matchPB(m, uid, org)}), nil
}

func (svc *MatchService) JoinMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.JoinMatchRequest],
) (*connect.Response[scoreboardv1.JoinMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	_, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner

	m, err := svc.store.JoinMatch(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(m.FamilyID, events.KindMatch, m.ID)
	return connect.NewResponse(&scoreboardv1.JoinMatchResponse{Match: svc.matchPB(m, uid, org)}), nil
}

func (svc *MatchService) LeaveMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.LeaveMatchRequest],
) (*connect.Response[scoreboardv1.LeaveMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	_, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner

	m, err := svc.store.LeaveMatch(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(m.FamilyID, events.KindMatch, m.ID)
	return connect.NewResponse(&scoreboardv1.LeaveMatchResponse{Match: svc.matchPB(m, uid, org)}), nil
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
	_, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner

	out, err := svc.store.DeclineMatch(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.DeclineMatchResponse{Match: svc.matchPB(out, uid, org)}), nil
}

func (svc *MatchService) InvitePlayers(
	ctx context.Context, req *connect.Request[scoreboardv1.InvitePlayersRequest],
) (*connect.Response[scoreboardv1.InvitePlayersResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner
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
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.InvitePlayersResponse{Match: svc.matchPB(out, uid, org)}), nil
}

func (svc *MatchService) SetTeams(
	ctx context.Context, req *connect.Request[scoreboardv1.SetTeamsRequest],
) (*connect.Response[scoreboardv1.SetTeamsResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner
	// Who you are partnered with is as much a term as what you are playing to,
	// and it is affirmed the same way — changing it clears every ready. Letting
	// anyone rearrange sides would let one player rewrite what the others
	// already agreed to.
	if m.CreatedBy != uid {
		return nil, denied("only whoever set this game up can set the teams")
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
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.SetTeamsResponse{Match: svc.matchPB(out, uid, org)}), nil
}

const maxContractDetail = 140

func (svc *MatchService) SetContract(
	ctx context.Context, req *connect.Request[scoreboardv1.SetContractRequest],
) (*connect.Response[scoreboardv1.SetContractResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner
	// One person owns the terms. With a ready check in front of starting, the
	// group's agreement is expressed by affirming rather than by editing — and
	// a contract that anyone can change out from under the people who already
	// said yes is a worse kind of collaborative.
	if m.CreatedBy != uid {
		return nil, denied("only whoever set this game up can set the contract")
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
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.SetContractResponse{Match: svc.matchPB(out, uid, org)}), nil
}

// SetReady affirms, or withdraws, that the caller is ready under the contract
// as it currently stands. Only for yourself — nobody vouches for anyone else.
func (svc *MatchService) SetReady(
	ctx context.Context, req *connect.Request[scoreboardv1.SetReadyRequest],
) (*connect.Response[scoreboardv1.SetReadyResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	_, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner

	out, err := svc.store.SetReady(ctx, req.Msg.GetMatchId(), uid, req.Msg.GetReady())
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.SetReadyResponse{Match: svc.matchPB(out, uid, org)}), nil
}

func (svc *MatchService) StartMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.StartMatchRequest],
) (*connect.Response[scoreboardv1.StartMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner
	// Setting up is collaborative; committing everyone to play is not. One
	// person calls it, and only once everybody has said they are ready.
	if m.CreatedBy != uid {
		return nil, denied("only whoever set this game up can start it")
	}

	out, err := svc.store.StartMatch(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.StartMatchResponse{Match: svc.matchPB(out, uid, org)}), nil
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
	m, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner
	if !isParticipant(m, uid) {
		return nil, denied("only players in this match can claim it")
	}

	out, err := svc.store.ClaimVictory(ctx, req.Msg.GetMatchId(), uid, req.Msg.GetTeamId(), svc.concedeWindow)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.ClaimVictoryResponse{Match: svc.matchPB(out, uid, org)}), nil
}

// WithdrawClaim takes a claim back off the table. Only the team that made it
// can, and only before the match finalizes — after that the record is the
// record, and disputing it is the route.
func (svc *MatchService) WithdrawClaim(
	ctx context.Context, req *connect.Request[scoreboardv1.WithdrawClaimRequest],
) (*connect.Response[scoreboardv1.WithdrawClaimResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner
	if !isParticipant(m, uid) {
		return nil, denied("only players in this match can withdraw a claim")
	}

	out, err := svc.store.WithdrawClaim(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.WithdrawClaimResponse{Match: svc.matchPB(out, uid, org)}), nil
}

// Concede accepts the standing claim. It is also how an objection is taken
// back, which is why it is reachable on a disputed match and not only a claimed
// one — see answerable in the store.
func (svc *MatchService) Concede(
	ctx context.Context, req *connect.Request[scoreboardv1.ConcedeRequest],
) (*connect.Response[scoreboardv1.ConcedeResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	_, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner

	out, err := svc.store.Concede(ctx, req.Msg.GetMatchId(), uid, svc.concedeWindow)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.ConcedeResponse{Match: svc.matchPB(out, uid, org)}), nil
}

const maxDisputeReason = 280

func (svc *MatchService) DisputeMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.DisputeMatchRequest],
) (*connect.Response[scoreboardv1.DisputeMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	_, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner
	if len([]rune(req.Msg.GetReason())) > maxDisputeReason {
		return nil, invalid("keep the explanation short")
	}

	out, err := svc.store.Dispute(ctx, req.Msg.GetMatchId(), uid, req.Msg.GetReason(), svc.concedeWindow)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.DisputeMatchResponse{Match: svc.matchPB(out, uid, org)}), nil
}

func (svc *MatchService) ResolveDispute(
	ctx context.Context, req *connect.Request[scoreboardv1.ResolveDisputeRequest],
) (*connect.Response[scoreboardv1.ResolveDisputeResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner
	if err := svc.requireOwner(ctx, m.FamilyID, uid); err != nil {
		return nil, err
	}
	// Being the organizer is not enough. While the players still have a move
	// left — somebody yet to answer, an objection its author could withdraw —
	// the disagreement is theirs to finish, and ruling on it early is exactly
	// the shortcut this app exists to refuse.
	if !m.Deadlocked(svc.concedeWindow) {
		return nil, denied("this one is still between the players — step in once they are stuck")
	}
	if !req.Msg.GetVoidMatch() && req.Msg.GetWinningTeamId() == "" {
		return nil, invalid("name the winning team, or void the match")
	}

	out, err := svc.store.ResolveDispute(ctx, req.Msg.GetMatchId(), req.Msg.GetWinningTeamId(), req.Msg.GetVoidMatch())
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.ResolveDisputeResponse{Match: svc.matchPB(out, uid, org)}), nil
}

// CancelMatch abandons a match.
//
// Cancelling is destructive in a way the other lobby actions are not: it takes
// the game away from everyone else waiting in it. Setting the contract or the
// teams is collaborative and open to any player, but only whoever set the game
// up — or the family organizer — can delete it. A player who does not want to
// play leaves; that is what Leave is for.
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
	org := role == model.RoleOwner
	if m.Status != model.StatusLobby && !org {
		return nil, denied("only the family organizer can cancel a match in play")
	}
	if m.Status == model.StatusLobby && m.CreatedBy != uid && !org {
		return nil, denied("only whoever set this game up, or the family organizer, can call it off")
	}

	out, err := svc.store.CancelMatch(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.CancelMatchResponse{Match: svc.matchPB(out, uid, org)}), nil
}

func (svc *MatchService) GetMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.GetMatchRequest],
) (*connect.Response[scoreboardv1.GetMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	m, role, err := svc.matchForCaller(ctx, req.Msg.GetMatchId(), uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner
	return connect.NewResponse(&scoreboardv1.GetMatchResponse{Match: svc.matchPB(m, uid, org)}), nil
}

func (svc *MatchService) ListMatches(
	ctx context.Context, req *connect.Request[scoreboardv1.ListMatchesRequest],
) (*connect.Response[scoreboardv1.ListMatchesResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	role, err := svc.requireMember(ctx, familyID, uid)
	if err != nil {
		return nil, err
	}
	org := role == model.RoleOwner

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
	return connect.NewResponse(&scoreboardv1.ListMatchesResponse{Matches: svc.matchesPB(matches, uid, org)}), nil
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
