package service

import (
	"errors"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	scoreboardv1 "github.com/bheadwhite/scoreboard/backend/gen/scoreboard/v1"
	"github.com/bheadwhite/scoreboard/backend/internal/model"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

// wrapErr maps store errors onto Connect codes. Handlers return this rather
// than leaking driver errors to clients.
func wrapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, store.ErrConflict):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	default:
		var connectErr *connect.Error
		if errors.As(err, &connectErr) {
			return connectErr
		}
		return connect.NewError(connect.CodeInternal, err)
	}
}

func invalid(msg string) error {
	return connect.NewError(connect.CodeInvalidArgument, errors.New(msg))
}

func denied(msg string) error {
	return connect.NewError(connect.CodePermissionDenied, errors.New(msg))
}

func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func tsp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return ts(*t)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func userPB(u model.User) *scoreboardv1.User {
	return &scoreboardv1.User{
		Id:          u.ID,
		DisplayName: u.DisplayName,
		AvatarUrl:   u.AvatarURL,
		CreatedAt:   ts(u.CreatedAt),
	}
}

func rolePB(r model.Role) scoreboardv1.FamilyRole {
	switch r {
	case model.RoleOwner:
		return scoreboardv1.FamilyRole_FAMILY_ROLE_OWNER
	case model.RoleMember:
		return scoreboardv1.FamilyRole_FAMILY_ROLE_MEMBER
	default:
		return scoreboardv1.FamilyRole_FAMILY_ROLE_UNSPECIFIED
	}
}

func familyPB(f model.Family) *scoreboardv1.Family {
	return &scoreboardv1.Family{
		Id:          f.ID,
		Name:        f.Name,
		InviteCode:  f.InviteCode,
		OwnerId:     f.OwnerID,
		CreatedAt:   ts(f.CreatedAt),
		ViewerRole:  rolePB(f.ViewerRole),
		MemberCount: int32(f.MemberCount),
	}
}

func memberPB(m model.FamilyMember) *scoreboardv1.FamilyMember {
	return &scoreboardv1.FamilyMember{
		User:     userPB(m.User),
		Role:     rolePB(m.Role),
		JoinedAt: ts(m.JoinedAt),
	}
}

func gamePB(g model.Game) *scoreboardv1.Game {
	return &scoreboardv1.Game{
		Id:         g.ID,
		FamilyId:   deref(g.FamilyID),
		Name:       g.Name,
		Emoji:      g.Emoji,
		TeamBased:  g.TeamBased,
		MinPlayers: g.MinPlayers,
		MaxPlayers: g.MaxPlayers,
		CreatedAt:  ts(g.CreatedAt),
	}
}

func statusPB(s model.Status) scoreboardv1.MatchStatus {
	switch s {
	case model.StatusLobby:
		return scoreboardv1.MatchStatus_MATCH_STATUS_LOBBY
	case model.StatusInProgress:
		return scoreboardv1.MatchStatus_MATCH_STATUS_IN_PROGRESS
	case model.StatusClaimed:
		return scoreboardv1.MatchStatus_MATCH_STATUS_CLAIMED
	case model.StatusFinal:
		return scoreboardv1.MatchStatus_MATCH_STATUS_FINAL
	case model.StatusDisputed:
		return scoreboardv1.MatchStatus_MATCH_STATUS_DISPUTED
	case model.StatusCanceled:
		return scoreboardv1.MatchStatus_MATCH_STATUS_CANCELED
	default:
		return scoreboardv1.MatchStatus_MATCH_STATUS_UNSPECIFIED
	}
}

func statusFromPB(s scoreboardv1.MatchStatus) (model.Status, bool) {
	switch s {
	case scoreboardv1.MatchStatus_MATCH_STATUS_LOBBY:
		return model.StatusLobby, true
	case scoreboardv1.MatchStatus_MATCH_STATUS_IN_PROGRESS:
		return model.StatusInProgress, true
	case scoreboardv1.MatchStatus_MATCH_STATUS_CLAIMED:
		return model.StatusClaimed, true
	case scoreboardv1.MatchStatus_MATCH_STATUS_FINAL:
		return model.StatusFinal, true
	case scoreboardv1.MatchStatus_MATCH_STATUS_DISPUTED:
		return model.StatusDisputed, true
	case scoreboardv1.MatchStatus_MATCH_STATUS_CANCELED:
		return model.StatusCanceled, true
	default:
		return "", false
	}
}

func contractKindPB(k string) scoreboardv1.ContractKind {
	switch k {
	case model.ContractFirstToScore:
		return scoreboardv1.ContractKind_CONTRACT_KIND_FIRST_TO_SCORE
	case model.ContractBestOf:
		return scoreboardv1.ContractKind_CONTRACT_KIND_BEST_OF
	case model.ContractLastStanding:
		return scoreboardv1.ContractKind_CONTRACT_KIND_LAST_STANDING
	case model.ContractSingleGame:
		return scoreboardv1.ContractKind_CONTRACT_KIND_SINGLE_GAME
	case model.ContractCustom:
		return scoreboardv1.ContractKind_CONTRACT_KIND_CUSTOM
	default:
		return scoreboardv1.ContractKind_CONTRACT_KIND_UNSPECIFIED
	}
}

func contractKindFromPB(k scoreboardv1.ContractKind) (string, bool) {
	switch k {
	case scoreboardv1.ContractKind_CONTRACT_KIND_FIRST_TO_SCORE:
		return model.ContractFirstToScore, true
	case scoreboardv1.ContractKind_CONTRACT_KIND_BEST_OF:
		return model.ContractBestOf, true
	case scoreboardv1.ContractKind_CONTRACT_KIND_LAST_STANDING:
		return model.ContractLastStanding, true
	case scoreboardv1.ContractKind_CONTRACT_KIND_SINGLE_GAME:
		return model.ContractSingleGame, true
	case scoreboardv1.ContractKind_CONTRACT_KIND_CUSTOM:
		return model.ContractCustom, true
	default:
		return "", false
	}
}

func contractPB(c model.Contract) *scoreboardv1.Contract {
	return &scoreboardv1.Contract{
		Kind:   contractKindPB(c.Kind),
		Target: c.Target,
		Detail: c.Detail,
	}
}

func resultPB(r string) scoreboardv1.ParticipantResult {
	switch r {
	case model.ResultWin:
		return scoreboardv1.ParticipantResult_PARTICIPANT_RESULT_WIN
	case model.ResultLoss:
		return scoreboardv1.ParticipantResult_PARTICIPANT_RESULT_LOSS
	default:
		return scoreboardv1.ParticipantResult_PARTICIPANT_RESULT_PENDING
	}
}

func teamPB(t model.MatchTeam) *scoreboardv1.MatchTeam {
	return &scoreboardv1.MatchTeam{
		Id:      t.ID,
		MatchId: t.MatchID,
		Name:    t.Name,
		Ordinal: t.Ordinal,
	}
}

func participantStatusPB(s string) scoreboardv1.ParticipantStatus {
	switch s {
	case model.InviteInvited:
		return scoreboardv1.ParticipantStatus_PARTICIPANT_STATUS_INVITED
	case model.InviteJoined:
		return scoreboardv1.ParticipantStatus_PARTICIPANT_STATUS_JOINED
	case model.InviteDeclined:
		return scoreboardv1.ParticipantStatus_PARTICIPANT_STATUS_DECLINED
	default:
		return scoreboardv1.ParticipantStatus_PARTICIPANT_STATUS_UNSPECIFIED
	}
}

func participantPB(p model.MatchParticipant) *scoreboardv1.MatchParticipant {
	return &scoreboardv1.MatchParticipant{
		MatchId:       p.MatchID,
		User:          userPB(p.User),
		TeamId:        deref(p.TeamID),
		Result:        resultPB(p.Result),
		ConcededAt:    tsp(p.ConcededAt),
		DisputedAt:    tsp(p.DisputedAt),
		Status:        participantStatusPB(p.Status),
		InvitedAt:     ts(p.InvitedAt),
		RespondedAt:   tsp(p.RespondedAt),
		ReadyAt:       tsp(p.ReadyAt),
		DisputeReason: p.DisputeReason,
	}
}

// matchPB converts a match for viewerID, filling in the convenience fields the
// UI needs: who still owes a concession, and whether the viewer is one of them.
//
// viewerIsOrganizer controls one piece of redaction. Who disputed a result is
// only sent to the family organizer, because they are the person who has to
// settle it. Everyone else sees that the match is disputed without seeing which
// relative called foul — the app already asks people to concede to each other's
// faces, and naming the objector turns a paused game into a grudge. Redacted
// here rather than hidden in the UI, so the name is not merely off-screen while
// still sitting in the response.
//
// A method rather than a function because whether the organizer may step in
// depends on the concession window, which lives on base. A service constructed
// without one still maps correctly — see Match.Deadlocked.
func (b base) matchPB(m model.Match, viewerID string, viewerIsOrganizer bool) *scoreboardv1.Match {
	out := &scoreboardv1.Match{
		Id:              m.ID,
		FamilyId:        m.FamilyID,
		Status:          statusPB(m.Status),
		Contract:        contractPB(m.Contract),
		CreatedBy:       m.CreatedBy,
		CreatedAt:       ts(m.CreatedAt),
		StartedAt:       tsp(m.StartedAt),
		ClaimedAt:       tsp(m.ClaimedAt),
		ClaimedByTeamId: deref(m.ClaimedByTeamID),
		ConcedeDeadline: tsp(m.ConcedeDeadline),
		FinalizedAt:     tsp(m.FinalizedAt),
	}
	if m.Game != nil {
		out.Game = gamePB(*m.Game)
	}
	for _, t := range m.Teams {
		out.Teams = append(out.Teams, teamPB(t))
	}
	for _, p := range m.Participants {
		out.Participants = append(out.Participants, redactDispute(participantPB(p), p, viewerID, viewerIsOrganizer))
	}
	// Split rosters so the UI never has to re-derive who is actually playing.
	for _, p := range m.JoinedParticipants() {
		out.Joined = append(out.Joined, redactDispute(participantPB(p), p, viewerID, viewerIsOrganizer))
	}
	for _, p := range m.PendingInvites() {
		out.Invited = append(out.Invited, participantPB(p))
		if p.User.ID == viewerID {
			out.ViewerIsInvited = true
		}
	}

	out.AllReady = m.AllReady()
	out.ViewerIsCreator = m.CreatedBy == viewerID
	for _, p := range m.AwaitingReady() {
		out.AwaitingReady = append(out.AwaitingReady, userPB(p.User))
	}
	for _, p := range m.JoinedParticipants() {
		if p.User.ID == viewerID && p.ReadyAt != nil {
			out.ViewerIsReady = true
		}
	}
	for _, p := range m.AwaitingConcession() {
		out.AwaitingConcession = append(out.AwaitingConcession, userPB(p.User))
		if p.User.ID == viewerID {
			out.ViewerOwesConcession = true
		}
	}

	out.ViewerIsClaimant = m.OnClaimingTeam(viewerID)
	out.OrganizerMayResolve = m.Deadlocked(b.concedeWindow)
	return out
}

// redactDispute strips who objected unless the viewer is the organizer, or it
// is their own objection — you always know what you did.
func redactDispute(
	pb *scoreboardv1.MatchParticipant, p model.MatchParticipant, viewerID string, organizer bool,
) *scoreboardv1.MatchParticipant {
	if organizer || p.User.ID == viewerID {
		return pb
	}
	pb.DisputedAt = nil
	pb.DisputeReason = ""
	return pb
}

func (b base) matchesPB(ms []model.Match, viewerID string, viewerIsOrganizer bool) []*scoreboardv1.Match {
	out := make([]*scoreboardv1.Match, 0, len(ms))
	for _, m := range ms {
		out = append(out, b.matchPB(m, viewerID, viewerIsOrganizer))
	}
	return out
}

// rangeStart turns a StatsRange into the earliest finalization instant that
// still counts. A zero time means all of it.
func rangeStart(r scoreboardv1.StatsRange, now time.Time) time.Time {
	switch r {
	case scoreboardv1.StatsRange_STATS_RANGE_YEAR:
		return now.AddDate(-1, 0, 0)
	case scoreboardv1.StatsRange_STATS_RANGE_MONTH:
		return now.AddDate(0, -1, 0)
	default:
		return time.Time{}
	}
}
