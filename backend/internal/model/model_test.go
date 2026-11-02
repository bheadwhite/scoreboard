package model

import (
	"testing"
	"time"
)

// A two-a-side match that the "winners" team has been credited with. Answers
// are filled in by each test.
//
// StatusFinal rather than the claimed status this helper used to build, because
// there is no longer a stage between a claim and a result. Everything these
// tests exercise — objecting, taking it back, who could have contradicted it —
// happens on a settled match now, which is also where it happens in real life.
func decided(answers map[string]string) Match {
	winners, losers := "t-win", "t-lose"
	m := Match{
		Status:          StatusFinal,
		ClaimedByTeamID: &winners,
		Teams:           []MatchTeam{{ID: winners}, {ID: losers}},
	}
	for _, seat := range []struct{ id, team string }{
		{"ann", winners},
		{"bob", winners},
		{"cal", losers},
		{"dee", losers},
	} {
		team := seat.team
		p := MatchParticipant{User: User{ID: seat.id}, TeamID: &team, Status: InviteJoined}
		switch answers[seat.id] {
		case "conceded":
			at := time.Now().Add(-time.Hour)
			p.ConcededAt = &at
		case "disputed":
			at := time.Now().Add(-time.Hour)
			p.DisputedAt = &at
		}
		m.Participants = append(m.Participants, p)
	}
	return m
}

func TestObjectorsCountsOnlyStandingObjections(t *testing.T) {
	m := decided(map[string]string{"cal": "disputed", "dee": "conceded"})
	if got := len(m.Objectors()); got != 1 {
		t.Fatalf("objectors = %d, want 1", got)
	}

	// Conceding withdraws the objection — the position is gone, not archived.
	m = decided(map[string]string{"cal": "conceded", "dee": "conceded"})
	if got := len(m.Objectors()); got != 0 {
		t.Fatalf("objectors after both conceded = %d, want 0", got)
	}
}

func TestObjectorsIncludesTheNamedWinner(t *testing.T) {
	// Wider than Answerable on purpose. Ann is on the side the result credits,
	// so she is not one of the people it was recorded against — but "I did not
	// win this", from her, is the strongest objection there is, and it has to
	// count. It matters more now than it used to: whoever posts a match can
	// credit any side, and the result is on the board before the person it
	// names has looked at their phone.
	m := decided(nil)
	at := time.Now()
	m.Participants[0].DisputedAt = &at

	if got := len(m.Objectors()); got != 1 {
		t.Fatalf("objectors = %d, want 1", got)
	}
	if !m.Contested() {
		t.Error("the named winner denies it; nothing is more contested than that")
	}
	// And she is still not one of the people it was recorded against.
	if got := len(m.Answerable()); got != 2 {
		t.Errorf("answerable = %d, want 2 — objecting is not the same as being on the losing side", got)
	}
}

func TestOnClaimingTeam(t *testing.T) {
	m := decided(nil)
	if !m.OnClaimingTeam("ann") {
		t.Error("ann is credited with the win; want true")
	}
	if m.OnClaimingTeam("cal") {
		t.Error("cal lost; want false")
	}
	if m.OnClaimingTeam("nobody") {
		t.Error("a stranger to the match is not on the winning team")
	}

	// Nothing credited: there is no side to be on.
	m.ClaimedByTeamID = nil
	if m.OnClaimingTeam("ann") {
		t.Error("no result standing; want false")
	}
}

func TestContestedTracksStandingObjections(t *testing.T) {
	clean := decided(nil)
	if clean.Contested() {
		t.Error("nobody has objected; want clean")
	}
	lone := decided(map[string]string{"cal": "disputed"})
	if !lone.Contested() {
		t.Error("one objection is enough — a majority was never the bar")
	}

	// The whole point of an objection being a position rather than an event:
	// taking it back makes the result clean again, at any time. This is the
	// only thing standing between the record and somebody entering whatever
	// they like, so it has to work on a settled match and it has to be
	// reversible in both directions.
	m := decided(map[string]string{"cal": "disputed"})
	if !m.Contested() {
		t.Error("a settled match carries its objections; that is what keeps it off the scoreboard")
	}
	m.Participants[2].DisputedAt = nil
	at := time.Now()
	m.Participants[2].ConcededAt = &at
	if m.Contested() {
		t.Error("the objection was withdrawn; the result is clean again")
	}
}

func TestAnswerableIsTheLosingSideWithAccounts(t *testing.T) {
	// The people the result was recorded against, who have a way to say it is
	// wrong. Not a queue of outstanding answers — nothing is outstanding, the
	// result already counts — but the list that decides whether anybody could
	// have contradicted it at all.
	m := decided(map[string]string{"cal": "conceded"})
	if got := len(m.Answerable()); got != 2 {
		t.Fatalf("answerable = %d, want 2 — conceding does not remove you from it", got)
	}
	if m.Unwitnessed() {
		t.Error("two people could have denied this; it is witnessed whatever they said")
	}
}

// placeholderize marks one of the seats as a player with no account.
func withoutAccount(m Match, userID string) Match {
	for i := range m.Participants {
		if m.Participants[i].User.ID == userID {
			m.Participants[i].User.Placeholder = true
		}
	}
	return m
}

func TestPlaceholdersAreNeverAsked(t *testing.T) {
	// Cal has no account. Dee is the only person who could contradict this, and
	// that is enough for the result to have been open to challenge.
	m := withoutAccount(decided(map[string]string{"dee": "conceded"}), "cal")

	if got := len(m.Answerable()); got != 1 {
		t.Fatalf("answerable = %d, want 1 — only Dee can speak", got)
	}
	if m.Unwitnessed() {
		t.Error("Dee could have objected; one is enough")
	}
}

func TestAResultNobodyCouldDenyIsMarked(t *testing.T) {
	// Both losers are placeholders. Nobody in this match is in a position to
	// contradict the winner, so the check every other result on the board is
	// subject to was simply unavailable here.
	//
	// The old protocol expressed this as a refusal to ratify early, which made
	// the match sit out the week and then settle exactly as claimed anyway —
	// seven days of silence from people with no way to make a sound, reported
	// as a wait. Saying so on the result is what is left, and it is the honest
	// version of the same worry.
	m := withoutAccount(withoutAccount(decided(nil), "cal"), "dee")

	if got := len(m.Answerable()); got != 0 {
		t.Fatalf("answerable = %d, want 0", got)
	}
	if !m.Unwitnessed() {
		t.Error("nobody could deny this result; it has to say so")
	}
}

func TestUnwitnessedNeedsAResult(t *testing.T) {
	// A game still in play is not an unwitnessed result, it is not a result.
	m := withoutAccount(withoutAccount(decided(nil), "cal"), "dee")
	m.Status = StatusInProgress
	m.ClaimedByTeamID = nil

	if m.Unwitnessed() {
		t.Error("nothing has been claimed; there is nothing to be unwitnessed")
	}
}

func TestPlaceholdersDoNotContestAnything(t *testing.T) {
	// A placeholder has no way to object either, so a match with one in it is
	// not contested on their behalf. Their objection is deferred, not assumed:
	// it arrives if they ever sign up and get linked.
	m := withoutAccount(decided(nil), "cal")
	if m.Contested() {
		t.Error("nobody objected")
	}
}
