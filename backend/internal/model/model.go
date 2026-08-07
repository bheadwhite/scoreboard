// Package model holds the domain types. Handlers never return these directly —
// service/mappers.go converts them to their proto equivalents.
package model

import "time"

type User struct {
	ID          string
	DisplayName string
	AvatarURL   string
	CreatedAt   time.Time
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

// Status values for a match. Only StatusFinal counts on the scoreboard.
type Status string

const (
	StatusLobby      Status = "lobby"
	StatusInProgress Status = "in_progress"
	StatusClaimed    Status = "claimed"
	StatusFinal      Status = "final"
	StatusDisputed   Status = "disputed"
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
	// When this player affirmed the contract as it then stood. Cleared whenever
	// the contract or the teams change.
	ReadyAt *time.Time
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

	ClaimedByTeamID *string
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

// AwaitingReady returns joined players who have not affirmed the current
// contract.
func (m *Match) AwaitingReady() []MatchParticipant {
	var out []MatchParticipant
	for _, p := range m.JoinedParticipants() {
		if p.ReadyAt == nil {
			out = append(out, p)
		}
	}
	return out
}

// AllReady reports whether every joined player has affirmed the contract.
// False for an empty roster: nobody being ready is not everybody being ready.
func (m *Match) AllReady() bool {
	joined := m.JoinedParticipants()
	return len(joined) > 0 && len(m.AwaitingReady()) == 0
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

// AwaitingConcession returns opponents who have neither conceded nor disputed.
func (m *Match) AwaitingConcession() []MatchParticipant {
	var out []MatchParticipant
	for _, p := range m.OpposingParticipants() {
		if p.ConcededAt == nil && p.DisputedAt == nil {
			out = append(out, p)
		}
	}
	return out
}

// Objectors returns opponents whose objection currently stands. An objection is
// a position held, not an event that happened: it lasts exactly as long as its
// author leaves it there, and conceding withdraws it.
func (m *Match) Objectors() []MatchParticipant {
	var out []MatchParticipant
	for _, p := range m.OpposingParticipants() {
		if p.DisputedAt != nil {
			out = append(out, p)
		}
	}
	return out
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

// Deadlocked reports whether the family organizer may step in.
//
// The organizer is a last resort, not a referee standing over the game. While
// players still have moves to make — an answer nobody has given yet, an
// objection its author could withdraw — the disagreement belongs to them, and
// handing it to an adjudicator early takes away the argument the app exists to
// have. So this is true only when the match cannot move on its own:
//
//   - everyone has had their say and they still disagree, or
//   - the claim has stood longer than the window it was given, which is the
//     escape hatch for an objection whose author has gone quiet — a dispute
//     stops the clock, so without this nothing would ever finish it.
//
// A window of zero disables the second test rather than making it always true,
// so a service that was never given one simply falls back to the first.
func (m *Match) Deadlocked(window time.Duration) bool {
	if m.Status != StatusDisputed || len(m.Objectors()) == 0 {
		return false
	}
	if len(m.AwaitingConcession()) == 0 {
		return true
	}
	return window > 0 && m.ClaimedAt != nil && time.Since(*m.ClaimedAt) > window
}

// StandingsRow is one player's record within a family.
type StandingsRow struct {
	User   User
	Wins   int32
	Losses int32
	// Positive for an active winning streak, negative for a losing one.
	Streak int32
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
}
