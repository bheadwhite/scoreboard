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

const maxContractDetail = 140

// CreateMatch posts a match. It is live from here: there is no lobby, no ready
// check and nothing to start, because the only thing any of those were waiting
// on was people agreeing to play, and joining says that on its own.
//
// The contract is validated here and set once. That is what makes joining a
// complete act of consent — the terms are on the screen before anybody taps,
// and nobody can edit them afterward.
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
	contract, err := parseContract(req.Msg.GetContract())
	if err != nil {
		return nil, err
	}

	m, err := svc.store.CreateMatch(
		ctx, familyID, req.Msg.GetGameId(), v.id, contract, req.Msg.GetInviteUserIds())
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(m.FamilyID, events.KindMatch, m.ID)
	return connect.NewResponse(&scoreboardv1.CreateMatchResponse{Match: svc.matchPB(m, v)}), nil
}

// parseContract validates the terms a match is posted under.
func parseContract(in *scoreboardv1.Contract) (model.Contract, error) {
	if in == nil {
		return model.Contract{}, invalid("say how this one is won")
	}
	kind, ok := contractKindFromPB(in.GetKind())
	if !ok {
		return model.Contract{}, invalid("pick how this game is won")
	}

	detail := strings.TrimSpace(in.GetDetail())
	if len([]rune(detail)) > maxContractDetail {
		return model.Contract{}, invalid("keep the contract description short")
	}

	target := in.GetTarget()
	switch kind {
	case model.ContractFirstToScore, model.ContractBestOf:
		if target <= 0 {
			return model.Contract{}, invalid("set the number to play to")
		}
	case model.ContractCustom:
		if detail == "" {
			return model.Contract{}, invalid("describe how this game is won")
		}
		target = 0
	default:
		target = 0
	}
	return model.Contract{Kind: kind, Target: target, Detail: detail}, nil
}

// JoinMatch puts somebody in the game — normally the caller, saying they are
// playing.
//
// The host may also do it for somebody they invited. That is a real exception to
// "nobody is dragged into a game", and it exists because the alternative is
// worse: four people around a table, one of them has posted the match, and the
// game cannot begin until the other three find their phones. The exception is
// kept as narrow as it can be — an unanswered invitation only, never a refusal —
// and it buys a game that can start rather than a result that can be
// manufactured, because everybody put in this way still has to answer the claim.
func (svc *MatchService) JoinMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.JoinMatchRequest],
) (*connect.Response[scoreboardv1.JoinMatchResponse], error) {
	m0, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}

	target, byCreator := v.id, false
	if id := req.Msg.GetUserId(); id != "" && id != v.id {
		if m0.CreatedBy != v.id {
			return nil, denied("only whoever set this game up can put somebody else in")
		}
		target, byCreator = id, true
	}

	m, err := svc.store.JoinMatch(ctx, req.Msg.GetMatchId(), target, byCreator)
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
	// One person owns the shape of the match, the same way they own its terms.
	// Letting anyone rearrange sides would let one player decide who is going
	// to owe the eventual claim an answer.
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

// ClaimVictory names the winner and starts the clock. Everything downstream of
// this — the answers, the timeout, the eventual place in the record — is the
// point of the app, so it is deliberately not a plain "record the score" call.
//
// Who may name whom is enforced in the store, where the teams are: your own
// side is anybody's to claim, and any side at all is the creator's.
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

// WithdrawClaim takes a claim back off the table. Whoever made it and whoever
// it named can, and only before the match settles — after that the record is
// the record, and objecting to it is the route.
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

// Concede accepts the claim. It is also how an objection is taken back, which
// is why it is reachable on a settled match and not only a claimed one — see
// answerable in the store.
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

// DisputeMatch marks the result contested. It does not stop the clock, does not
// stop the match settling, and is not routed to anybody for a ruling — it holds
// the result off the clean scoreboard for exactly as long as the objection
// stands, and the objector can take it down whenever they like.
//
// Reachable on a settled match, which is where most real objections will be
// raised: people notice a wrong result when they see it in the standings.
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

// CancelMatch abandons a match.
//
// Cancelling is destructive in a way joining and leaving are not: it takes the
// game away from everybody else in it. So it splits on whether there is anybody
// else — the host of a match nobody has joined is deleting their own post, and
// past that only the family organizer can, because a player who is losing must
// never be able to erase a game rather than lose it. A player who does not want
// to play leaves; that is what Leave is for.
func (svc *MatchService) CancelMatch(
	ctx context.Context, req *connect.Request[scoreboardv1.CancelMatchRequest],
) (*connect.Response[scoreboardv1.CancelMatchResponse], error) {
	m, v, err := svc.matchViewer(ctx, req.Msg.GetMatchId())
	if err != nil {
		return nil, err
	}
	// Nobody has staked anything on a match nobody has joined yet, so the host
	// can drop their own. Once other people are in it, calling it off takes the
	// game away from them, and only the organizer does that.
	alone := len(m.JoinedParticipants()) <= 1
	if !alone && !v.organizer {
		return nil, denied("only the family organizer can call off a game other people are in")
	}
	if alone && m.CreatedBy != v.id && !v.organizer {
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
