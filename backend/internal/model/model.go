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

// Status values for a match. Two live ones: it has a result or it does not.
// Only StatusFinal counts on the scoreboard, and only a StatusFinal match
// nobody is objecting to counts on the clean one.
//
// There is no lobby: a match is in play from the moment it is posted, because
// joining is the only commitment anybody makes and there is nothing to wait
// for before it. There is no disputed: an objection marks a match rather than
// moving it, so being contested is a question you ask the answers — see
// Match.Contested — at any stage of the match's life, including after it has
// finalized. And there is no claimed, which is the biggest of the three
// removals: naming a winner used to open a pending stage that collected answers
// and closed on the last one or on a week-long clock, and both endings wrote
// the identical result. A stage that cannot change its own outcome is a delay,
// not a check, so the claim is now the result.
type Status string

const (
	StatusInProgress Status = "in_progress"
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

// OpposingParticipants returns every joined player not on the team credited
// with the win — the people the result was recorded against.
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

// Answerable returns the opponents in a position to contradict the result: the
// people it was recorded against who have an account to say so from.
//
// This used to be a list of debts — who the claim was still waiting on. Nothing
// waits any more, so what it names now is capability: these are the players
// whose silence is worth something, because they could have spoken. A
// placeholder is excluded for the same reason as before, that there is nobody
// to ask, and their agreement stays deferred rather than assumed: once they
// sign up and an administrator links them, they inherit the history and can
// object to any of it, because objections outlive finalization.
//
// Its one remaining job is Unwitnessed, which is the honest report of a result
// this list is empty for.
func (m *Match) Answerable() []MatchParticipant {
	var out []MatchParticipant
	for _, p := range m.OpposingParticipants() {
		if !p.User.Placeholder {
			out = append(out, p)
		}
	}
	return out
}

// Unwitnessed reports that a result stands with nobody in a position to
// contradict it — every opponent of the named side is a player with no account.
//
// This is what is left of a rule that used to be enforced by delay. Match.Answered
// required at least one opponent who could speak before a claim could ratify,
// so a game whose only losers were placeholders could not settle early and sat
// out the week instead. The intent was right and the mechanism did nothing: at
// the end of the week it settled exactly as claimed, against players who by
// construction cannot contradict anybody. Seven days of silence from people
// with no way to make a sound is not agreement, and reporting it as a wait was
// worse than saying nothing.
//
// So the app says so on the result instead. It counts and it is shown like any
// other, carrying the one fact that makes it different: the check every other
// result on the board is subject to was unavailable here. It is not permanent —
// linking a placeholder to a real account hands them the history and the
// standing to object to it.
func (m *Match) Unwitnessed() bool {
	return m.ClaimedByTeamID != nil && len(m.Answerable()) == 0
}

// Objectors returns every player whose objection currently stands. An objection
// is a position held, not an event that happened: it lasts exactly as long as
// its author leaves it there, and conceding withdraws it.
//
// Every joined player, not just the losing side — a wider net than Answerable
// casts, and deliberately so. Being the person a result was recorded against
// and being able to say the result is wrong are different things. "I did not
// win this", from the person credited with the win, is the single most damning
// thing anybody could say about a result, and there has to be somewhere to say
// it — especially since whoever posts a match can hand a win to somebody who
// never asked for one, and it lands on the board before they have looked at
// their phone. See answerable in the store, which decides who may object.
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
// This is the whole of what an objection does, and — with nothing pending in
// front of a result any more — the whole of what protects the record. Nobody
// adjudicates it, no majority is needed and no permission is asked. It keeps
// the result off the clean scoreboard for as long as somebody is willing to
// stand behind saying it did not happen.
func (m *Match) Contested() bool {
	return len(m.Objectors()) > 0
}

// OnClaimingTeam reports whether userID is on the side credited with the win —
// the side that may refuse it, rather than the side it was recorded against.
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
