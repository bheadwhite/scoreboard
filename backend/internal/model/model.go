// Package model holds the domain types. Handlers never return these directly —
// service/mappers.go converts them to their proto equivalents.
package model

import "time"

type User struct {
	ID          string
	DisplayName string
	AvatarURL   string
	CreatedAt   time.Time
	// True for a player who has not signed up — somebody another player added
	// so a game they were actually in could be recorded with everybody in it.
	//
	// A placeholder is a player in every way the scoreboard cares about. The
	// one thing they cannot do is answer a claim, because there is no device to
	// answer from, and that is the only place in the app this flag is consulted
	// for anything other than display. See Match.Answerable.
	Placeholder bool
}

// Role is a member's standing inside one family.
type Role string

const (
	RoleMember Role = "member"
	RoleOwner  Role = "owner"
)

type Family struct {
	ID          string
	Name        string
	InviteCode  string
	OwnerID     string
	CreatedAt   time.Time
	MemberCount int
	// The requesting user's role, filled in per-request.
	ViewerRole Role
}

type FamilyMember struct {
	User     User
	Role     Role
	JoinedAt time.Time
}

type Game struct {
	ID string
	// Nil for built-in catalog games.
	FamilyID   *string
	Name       string
	Emoji      string
	TeamBased  bool
	MinPlayers int32
	MaxPlayers int32
	CreatedAt  time.Time
}

// Status values for a match. Only StatusFinal counts on the scoreboard, and
// only a StatusFinal match nobody is objecting to counts on the clean one.
//
// There is no lobby: a match is in play from the moment it is posted, because
// joining is the only commitment anybody makes and there is nothing to wait
// for before it. And there is no disputed: an objection marks a match rather
// than moving it, so being contested is a question you ask the answers — see
// Match.Contested — at any stage of the match's life, including after it has
// finalized.
type Status string

const (
	StatusInProgress Status = "in_progress"
	StatusClaimed    Status = "claimed"
	StatusFinal      Status = "final"
	StatusCanceled   Status = "canceled"
)

// ContractKind values. See the Contract message in common.proto.
const (
	ContractFirstToScore = "first_to_score"
	ContractBestOf       = "best_of"
	ContractLastStanding = "last_standing"
	ContractSingleGame   = "single_game"
	ContractCustom       = "custom"
	ContractUnspecified  = "unspecified"
)

type Contract struct {
	Kind   string
	Target int32
	Detail string
}

// Participant status values. Being named by whoever set the match up creates
// an invitation, not a participant — only StatusJoined players hold a team,
// count toward starting, claim, concede, or reach the record.
const (
	InviteInvited  = "invited"
	InviteJoined   = "joined"
	InviteDeclined = "declined"
)

// Result values, written at finalization.
const (
	ResultPending = "pending"
	ResultWin     = "win"
	ResultLoss    = "loss"
)

type MatchTeam struct {
	ID      string
	MatchID string
	Name    string
	Ordinal int32
}

type MatchParticipant struct {
	MatchID string
	User    User
	// Nil while the player is still only invited — an invitation carries no team.
	TeamID     *string
	Result     string
	ConcededAt *time.Time
	DisputedAt *time.Time

	// InviteInvited, InviteJoined or InviteDeclined.
	Status      string
	InvitedAt   time.Time
	RespondedAt *time.Time
	// Why this player objected. Only meaningful alongside DisputedAt.
	DisputeReason string
}

// Joined reports whether this player actually agreed to play.
func (p MatchParticipant) Joined() bool { return p.Status == InviteJoined }

type Match struct {
	ID        string
	FamilyID  string
	GameID    string
	Status    Status
	Contract  Contract
	CreatedBy string
	CreatedAt time.Time

	StartedAt   *time.Time
	ClaimedAt   *time.Time
	FinalizedAt *time.Time

	// The side said to have won, and who said so. Usually somebody on that
	// side; when the creator settles a game on everybody's behalf it is them,
	// and then they are an opponent of the team they just crowned.
	ClaimedByTeamID *string
	ClaimedByUserID *string
	ConcedeDeadline *time.Time
	DisputeReason   string

	Game         *Game
	Teams        []MatchTeam
	Participants []MatchParticipant
}

// JoinedParticipants returns the players who agreed to play. This is the
// roster every rule operates on; invitations that were never answered are not
// part of the match.
func (m *Match) JoinedParticipants() []MatchParticipant {
	var out []MatchParticipant
	for _, p := range m.Participants {
		if p.Joined() {
			out = append(out, p)
		}
	}
	return out
}

// PendingInvites returns players who have been asked and have not answered.
func (m *Match) PendingInvites() []MatchParticipant {
	var out []MatchParticipant
	for _, p := range m.Participants {
		if p.Status == InviteInvited {
			out = append(out, p)
		}
	}
	return out
}

// OpposingParticipants returns the players who owe the claiming team a
// response: every joined player not on the claimed team.
func (m *Match) OpposingParticipants() []MatchParticipant {
	if m.ClaimedByTeamID == nil {
		return nil
	}
	var out []MatchParticipant
	for _, p := range m.JoinedParticipants() {
		if p.TeamID == nil || *p.TeamID != *m.ClaimedByTeamID {
			out = append(out, p)
		}
	}
	return out
}

// Answerable returns the opponents who could actually answer the claim: the
// ones who owe it a response and have an account to give it from.
//
// A placeholder owes nothing, because there is nobody to owe it. Asking them
// would not be a stricter rule, it would be an unanswerable one — every match
// with somebody's grandmother in it would sit outstanding for a week and then
// settle exactly as it was always going to. Their agreement is deferred instead:
// once they sign up and an administrator links them, they inherit the history
// and can object to any of it, because objections outlive finalization.
func (m *Match) Answerable() []MatchParticipant {
	var out []MatchParticipant
	for _, p := range m.OpposingParticipants() {
		if !p.User.Placeholder {
			out = append(out, p)
		}
	}
	return out
}

// Answered reports whether the claim has been put to somebody and settled.
//
// Two conditions, and the second is the one worth explaining. Every opponent
// who could answer must have — either way, since an objection marks a result
// rather than blocking it, and waiting for unanimous agreement would mean
// waiting forever on a claim everybody rejected.
//
// And at least one such opponent must exist. Without that, a game whose only
// opponents are placeholders has nobody outstanding from the first instant, so
// it would ratify immediately — a win recorded on the say-so of the one person
// who claimed it, against players who by construction cannot contradict them.
// That is the exact thing this app refuses to do. So it does not ratify: it
// waits out the concession window and settles on the clock, which is silence
// being read as agreement rather than agreement being invented.
func (m *Match) Answered() bool {
	return len(m.Answerable()) > 0 && len(m.AwaitingConcession()) == 0
}

// AwaitingConcession returns opponents who could answer and have not.
func (m *Match) AwaitingConcession() []MatchParticipant {
	var out []MatchParticipant
	for _, p := range m.Answerable() {
		if p.ConcededAt == nil && p.DisputedAt == nil {
			out = append(out, p)
		}
	}
	return out
}

// Objectors returns every player whose objection currently stands. An objection
// is a position held, not an event that happened: it lasts exactly as long as
// its author leaves it there, and conceding withdraws it.
//
// Every joined player, not just the opposing side — which is a wider net than
// AwaitingConcession casts, and deliberately so. Owing the claim an answer and
// being able to say the result is wrong are different things. Somebody on the
// winning side is never asked, because agreeing with a win handed to them is not
// news; but once the match has settled, "I did not win this" from the person
// credited with the win is the single most damning thing anybody could say
// about a result, and there has to be somewhere to say it. See answerable in
// the store, which is what actually decides who may object and when.
//
// Nothing stale can turn up here: ClaimVictory wipes every answer on the match
// before recording a new claim, so a disputed_at row always answers the claim
// currently standing.
func (m *Match) Objectors() []MatchParticipant {
	var out []MatchParticipant
	for _, p := range m.JoinedParticipants() {
		if p.DisputedAt != nil {
			out = append(out, p)
		}
	}
	return out
}

// Contested reports whether an objection stands against the result.
//
// Deliberately not a status and deliberately not stored. It is true of a match
// mid-claim and true of one that settled years ago, because the objection lives
// on the participant row and that row outlives finalization — and it stops being
// true the moment its author concedes, at any point, forever. A stored flag
// would have to be maintained at every one of those transitions; a question
// asked of the answers cannot fall out of step with them.
//
// This is the whole of what an objection does. It does not stop the clock, it
// does not block finalization, and nobody adjudicates it. It keeps the result
// off the clean scoreboard for as long as somebody is willing to stand behind
// saying it did not happen.
func (m *Match) Contested() bool {
	return len(m.Objectors()) > 0
}

// OnClaimingTeam reports whether userID answers for the standing claim rather
// than owing a concession to it.
func (m *Match) OnClaimingTeam(userID string) bool {
	if m.ClaimedByTeamID == nil {
		return false
	}
	for _, p := range m.JoinedParticipants() {
		if p.User.ID == userID {
			return p.TeamID != nil && *p.TeamID == *m.ClaimedByTeamID
		}
	}
	return false
}

// StandingsRow is one player's record within a family.
type StandingsRow struct {
	User   User
	Wins   int32
	Losses int32
	// Positive for an active winning streak, negative for a losing one.
	Streak int32
	// Settled matches somebody is objecting to. In neither column above.
	ContestedWins   int32
	ContestedLosses int32
}

// GameRecord is a win/loss split for a single game.
type GameRecord struct {
	Game   Game
	Wins   int32
	Losses int32
}

// HeadToHead is one player's record against one opponent.
type HeadToHead struct {
	User     User
	Opponent User
	// Meetings on opposing teams.
	Wins   int32
	Losses int32
	// Meetings as teammates.
	TogetherWins   int32
	TogetherLosses int32
	Games          []GameRecord
	Streak         int32
	// Opposing meetings somebody is objecting to. In none of the above.
	ContestedWins   int32
	ContestedLosses int32
}

// PlayerProfile is the bragging-rights summary for one player.
type PlayerProfile struct {
	User          User
	Wins          int32
	Losses        int32
	CurrentStreak int32
	LongestWin    int32
	// The opponent who has beaten this player the most.
	Nemesis       *User
	NemesisLosses int32
	NemesisWins   int32
	// The opponent this player has beaten the most.
	Victim       *User
	VictimWins   int32
	VictimLosses int32
	Games        []GameRecord
	// Settled matches somebody is objecting to. In none of the above.
	ContestedWins   int32
	ContestedLosses int32
}
