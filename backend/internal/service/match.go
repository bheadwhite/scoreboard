package service

import (
	"context"
	"log"
	"strings"
	"time"

	"connectrpc.com/connect"

	scoreboardv1 "github.com/bheadwhite/scoreboard/backend/gen/scoreboard/v1"
	"github.com/bheadwhite/scoreboard/backend/internal/auth"
	"github.com/bheadwhite/scoreboard/backend/internal/events"
	"github.com/bheadwhite/scoreboard/backend/internal/model"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

type MatchService struct {
	base
}

func NewMatchService(
	s *store.Store, hub *events.Hub, concedeWindow time.Duration, admins auth.Admins,
) *MatchService {
	return &MatchService{base: base{store: s, hub: hub, concedeWindow: concedeWindow, admins: admins}}
}

func (svc *MatchService) CreateMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.CreateMatchRequest],
) (*connect.Response[scoreboardv1.CreateMatchResponse], error) {
	familyID := req.Msg.GetFamilyId()
	v, err := svc.familyViewer(ctx, familyID)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetGameId() == "" {
		return nil, invalid("pick a game")
	}

	m, err := svc.store.CreateMatch(ctx, familyID, req.Msg.GetGameId(), v.id, req.Msg.GetInviteUserIds())
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(m.FamilyID, events.KindMatch, m.ID)
	return connect.NewResponse(&scoreboardv1.CreateMatchResponse{Match: svc.matchPB(m, v)}), nil
}

func (svc *MatchService) JoinMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.JoinMatchRequest],
) (*connect.Response[scoreboardv1.JoinMatchResponse], error) {
	_, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}

	m, err := svc.store.JoinMatch(ctx, req.Msg.GetMatchId(), v.id)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(m.FamilyID, events.KindMatch, m.ID)
	return connect.NewResponse(&scoreboardv1.JoinMatchResponse{Match: svc.matchPB(m, v)}), nil
}

func (svc *MatchService) LeaveMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.LeaveMatchRequest],
) (*connect.Response[scoreboardv1.LeaveMatchResponse], error) {
	_, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}

	m, err := svc.store.LeaveMatch(ctx, req.Msg.GetMatchId(), v.id)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(m.FamilyID, events.KindMatch, m.ID)
	return connect.NewResponse(&scoreboardv1.LeaveMatchResponse{Match: svc.matchPB(m, v)}), nil
}

// DeclineMatch turns down an invitation. Only the invited player can do this
// for themselves — nobody declines on somebody else's behalf.
func (svc *MatchService) DeclineMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.DeclineMatchRequest],
) (*connect.Response[scoreboardv1.DeclineMatchResponse], error) {
	_, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}

	out, err := svc.store.DeclineMatch(ctx, req.Msg.GetMatchId(), v.id)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.DeclineMatchResponse{Match: svc.matchPB(out, v)}), nil
}

func (svc *MatchService) InvitePlayers(
	ctx context.Context, req *connect.Request[scoreboardv1.InvitePlayersRequest],
) (*connect.Response[scoreboardv1.InvitePlayersResponse], error) {
	m, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}
	if !isParticipant(m, v.id) {
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
	return connect.NewResponse(&scoreboardv1.InvitePlayersResponse{Match: svc.matchPB(out, v)}), nil
}

func (svc *MatchService) SetTeams(
	ctx context.Context, req *connect.Request[scoreboardv1.SetTeamsRequest],
) (*connect.Response[scoreboardv1.SetTeamsResponse], error) {
	m, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}
	// Who you are partnered with is as much a term as what you are playing to,
	// and it is affirmed the same way — changing it clears every ready. Letting
	// anyone rearrange sides would let one player rewrite what the others
	// already agreed to.
	if m.CreatedBy != v.id {
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
	return connect.NewResponse(&scoreboardv1.SetTeamsResponse{Match: svc.matchPB(out, v)}), nil
}

const maxContractDetail = 140

func (svc *MatchService) SetContract(
	ctx context.Context, req *connect.Request[scoreboardv1.SetContractRequest],
) (*connect.Response[scoreboardv1.SetContractResponse], error) {
	m, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}
	// One person owns the terms. With a ready check in front of starting, the
	// group's agreement is expressed by affirming rather than by editing — and
	// a contract that anyone can change out from under the people who already
	// said yes is a worse kind of collaborative.
	if m.CreatedBy != v.id {
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
	return connect.NewResponse(&scoreboardv1.SetContractResponse{Match: svc.matchPB(out, v)}), nil
}

// SetReady affirms, or withdraws, that the caller is ready under the contract
// as it currently stands. Only for yourself — nobody vouches for anyone else.
func (svc *MatchService) SetReady(
	ctx context.Context, req *connect.Request[scoreboardv1.SetReadyRequest],
) (*connect.Response[scoreboardv1.SetReadyResponse], error) {
	_, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}

	out, err := svc.store.SetReady(ctx, req.Msg.GetMatchId(), v.id, req.Msg.GetReady())
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.SetReadyResponse{Match: svc.matchPB(out, v)}), nil
}

func (svc *MatchService) StartMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.StartMatchRequest],
) (*connect.Response[scoreboardv1.StartMatchResponse], error) {
	m, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}
	// Setting up is collaborative; committing everyone to play is not. One
	// person calls it, and only once everybody has said they are ready.
	if m.CreatedBy != v.id {
		return nil, denied("only whoever set this game up can start it")
	}

	out, err := svc.store.StartMatch(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.StartMatchResponse{Match: svc.matchPB(out, v)}), nil
}

// ClaimVictory starts the concession clock. Everything downstream of this — the
// concessions, the timeout, the eventual place in the record — is the point of
// the app, so it is deliberately not a plain "record the score" call.
func (svc *MatchService) ClaimVictory(
	ctx context.Context, req *connect.Request[scoreboardv1.ClaimVictoryRequest],
) (*connect.Response[scoreboardv1.ClaimVictoryResponse], error) {
	m, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}
	if !isParticipant(m, v.id) {
		return nil, denied("only players in this match can claim it")
	}

	out, err := svc.store.ClaimVictory(ctx, req.Msg.GetMatchId(), v.id, req.Msg.GetTeamId(), svc.concedeWindow)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.ClaimVictoryResponse{Match: svc.matchPB(out, v)}), nil
}

// WithdrawClaim takes a claim back off the table. Only the team that made it
// can, and only before the match finalizes — after that the record is the
// record, and disputing it is the route.
func (svc *MatchService) WithdrawClaim(
	ctx context.Context, req *connect.Request[scoreboardv1.WithdrawClaimRequest],
) (*connect.Response[scoreboardv1.WithdrawClaimResponse], error) {
	m, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}
	if !isParticipant(m, v.id) {
		return nil, denied("only players in this match can withdraw a claim")
	}

	out, err := svc.store.WithdrawClaim(ctx, req.Msg.GetMatchId(), v.id)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.WithdrawClaimResponse{Match: svc.matchPB(out, v)}), nil
}

// Concede accepts the standing claim. It is also how an objection is taken
// back, which is why it is reachable on a disputed match and not only a claimed
// one — see answerable in the store.
func (svc *MatchService) Concede(
	ctx context.Context, req *connect.Request[scoreboardv1.ConcedeRequest],
) (*connect.Response[scoreboardv1.ConcedeResponse], error) {
	_, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}

	out, err := svc.store.Concede(ctx, req.Msg.GetMatchId(), v.id, svc.concedeWindow)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.ConcedeResponse{Match: svc.matchPB(out, v)}), nil
}

const maxDisputeReason = 280

func (svc *MatchService) DisputeMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.DisputeMatchRequest],
) (*connect.Response[scoreboardv1.DisputeMatchResponse], error) {
	_, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}
	if len([]rune(req.Msg.GetReason())) > maxDisputeReason {
		return nil, invalid("keep the explanation short")
	}

	out, err := svc.store.Dispute(ctx, req.Msg.GetMatchId(), v.id, req.Msg.GetReason(), svc.concedeWindow)
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.DisputeMatchResponse{Match: svc.matchPB(out, v)}), nil
}

func (svc *MatchService) ResolveDispute(
	ctx context.Context, req *connect.Request[scoreboardv1.ResolveDisputeRequest],
) (*connect.Response[scoreboardv1.ResolveDisputeResponse], error) {
	m, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}
	if err := svc.requireOwner(ctx, m.FamilyID, v.id); err != nil {
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
	return connect.NewResponse(&scoreboardv1.ResolveDisputeResponse{Match: svc.matchPB(out, v)}), nil
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
	m, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}
	if m.Status != model.StatusLobby && !v.organizer {
		return nil, denied("only the family organizer can cancel a match in play")
	}
	if m.Status == model.StatusLobby && m.CreatedBy != v.id && !v.organizer {
		return nil, denied("only whoever set this game up, or the family organizer, can call it off")
	}

	out, err := svc.store.CancelMatch(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.CancelMatchResponse{Match: svc.matchPB(out, v)}), nil
}

// VoidMatch erases a match. Administrators only.
//
// This is the one thing in the app that reaches into the permanent record and
// takes something out of it, so it is worth being explicit about what it is
// for. Not for a result somebody dislikes — that is what claiming, conceding
// and disputing are for, and an organizer settling a deadlock still ends with
// somebody named as the winner. This is for a match that should never have
// existed at all: a test game played against real people, a claim the sweeper
// ratified while everyone was away, a family created twice.
//
// It deletes rather than marks. A voided match kept as a row would be a fourth
// thing every stats query has to remember to skip, and the day one of them
// forgot, a match nobody can see would be back on the scoreboard. Deleting is
// the version with no way to get that wrong — and everything derived from a
// match is computed on read, so the row going is the whole cleanup.
//
// Ungated on status on purpose. A finalized match is precisely the one that
// needs this, because nothing else in the app can touch it.
//
// Ungated on family membership too, which is the one place this departs from
// every other handler in the file. Running the deployment is the authorization;
// requiring membership on top would mean joining a household in order to tidy up
// after it, and an administrator who has to become a player first is worse for
// everyone's privacy than one who never appears on the scoreboard at all.
func (svc *MatchService) VoidMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.VoidMatchRequest],
) (*connect.Response[scoreboardv1.VoidMatchResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	if err := svc.requireAdmin(ctx, uid); err != nil {
		return nil, err
	}
	if req.Msg.GetMatchId() == "" {
		return nil, invalid("match_id is required")
	}
	reason := strings.TrimSpace(req.Msg.GetReason())
	if len([]rune(reason)) > maxDisputeReason {
		return nil, invalid("keep the explanation short")
	}

	out, err := svc.store.DeleteMatch(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, wrapErr(err)
	}

	// The only trace this leaves. Written before announcing it, so the record of
	// what happened exists even if everything after this fails.
	game := ""
	if out.Game != nil {
		game = out.Game.Name
	}
	log.Printf("void: admin %s erased match %s (%s, %s, family %s, %d players) reason=%q",
		uid, out.ID, game, out.Status, out.FamilyID, len(out.JoinedParticipants()), reason)

	// Watchers re-read and are told it is gone. There is no match to send back.
	svc.publish(out.FamilyID, events.KindMatch, out.ID)
	return connect.NewResponse(&scoreboardv1.VoidMatchResponse{FamilyId: out.FamilyID}), nil
}

func (svc *MatchService) GetMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.GetMatchRequest],
) (*connect.Response[scoreboardv1.GetMatchResponse], error) {
	m, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&scoreboardv1.GetMatchResponse{Match: svc.matchPB(m, v)}), nil
}

func (svc *MatchService) ListMatches(
	ctx context.Context, req *connect.Request[scoreboardv1.ListMatchesRequest],
) (*connect.Response[scoreboardv1.ListMatchesResponse], error) {
	familyID := req.Msg.GetFamilyId()
	v, err := svc.familyViewer(ctx, familyID)
	if err != nil {
		return nil, err
	}

	statuses := make([]string, 0, len(req.Msg.GetStatuses()))
	for _, s := range req.Msg.GetStatuses() {
		value, ok := statusFromPB(s)
		if !ok {
			return nil, invalid("unknown match status")
		}
		statuses = append(statuses, string(value))
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
	return connect.NewResponse(&scoreboardv1.ListMatchesResponse{Matches: svc.matchesPB(matches, v)}), nil
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
