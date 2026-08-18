package model

import (
	"testing"
	"time"
)

// A two-a-side match with the "winners" team claiming. Answers are filled in by
// each test.
func claimed(answers map[string]string) Match {
	winners, losers := "t-win", "t-lose"
	m := Match{
		Status:          StatusClaimed,
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
	m := claimed(map[string]string{"cal": "disputed", "dee": "conceded"})
	if got := len(m.Objectors()); got != 1 {
		t.Fatalf("objectors = %d, want 1", got)
	}

	// Conceding withdraws the objection — the position is gone, not archived.
	m = claimed(map[string]string{"cal": "conceded", "dee": "conceded"})
	if got := len(m.Objectors()); got != 0 {
		t.Fatalf("objectors after both conceded = %d, want 0", got)
	}
}

func TestObjectorsIncludesTheNamedWinner(t *testing.T) {
	// Wider than AwaitingConcession on purpose. Ann is on the side that was
	// named, so she is never *asked* — but once a result is on the board, "I did
	// not win this" from the person credited with it is the strongest objection
	// there is, and it has to count. answerable is what decides she may only say
	// it after the match settles; this only has to count it when she does.
	m := claimed(nil)
	m.Status = StatusFinal
	at := time.Now()
	m.Participants[0].DisputedAt = &at

	if got := len(m.Objectors()); got != 1 {
		t.Fatalf("objectors = %d, want 1", got)
	}
	if !m.Contested() {
		t.Error("the named winner denies it; nothing is more contested than that")
	}
	// And she is still not somebody the claim was waiting on.
	if got := len(m.AwaitingConcession()); got != 2 {
		t.Errorf("awaiting = %d, want 2 — objecting is not the same as being asked", got)
	}
}

func TestOnClaimingTeam(t *testing.T) {
	m := claimed(nil)
	if !m.OnClaimingTeam("ann") {
		t.Error("ann claimed; want true")
	}
	if m.OnClaimingTeam("cal") {
		t.Error("cal is an opponent; want false")
	}
	if m.OnClaimingTeam("nobody") {
		t.Error("a stranger to the match is not on the claiming team")
	}

	// Nothing claimed yet: nobody answers for a claim that does not exist.
	m.ClaimedByTeamID = nil
	if m.OnClaimingTeam("ann") {
		t.Error("no claim standing; want false")
	}
}

func TestContestedTracksStandingObjections(t *testing.T) {
	clean := claimed(nil)
	if clean.Contested() {
		t.Error("nobody has objected; want clean")
	}
	lone := claimed(map[string]string{"cal": "disputed"})
	if !lone.Contested() {
		t.Error("one objection is enough — a majority was never the bar")
	}

	// The whole point of an objection being a position rather than an event:
	// taking it back makes the result clean again, at any time.
	m := claimed(map[string]string{"cal": "disputed"})
	m.Status = StatusFinal
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

func TestAnsweredIsEitherAnswer(t *testing.T) {
	half := claimed(map[string]string{"cal": "conceded"})
	if half.Answered() {
		t.Error("dee has said nothing; the claim is not answered")
	}

	// Both objected. Unanimous disagreement is still a complete set of answers,
	// and the match settles — contested. Waiting for a concession that is never
	// coming is how the old rules produced a match nobody could finish.
	m := claimed(map[string]string{"cal": "disputed", "dee": "disputed"})
	if !m.Answered() {
		t.Error("everyone answered; the claim settles, contested")
	}
	if !m.Contested() {
		t.Error("and it settles contested")
	}

	// Mixed answers are complete too. There is no vote to win.
	mixed := claimed(map[string]string{"cal": "disputed", "dee": "conceded"})
	if !mixed.Answered() {
		t.Error("one of each is still everybody having spoken")
	}
}

func TestAnsweredIgnoresPlayersOnTheWinningSide(t *testing.T) {
	// Ann and Bob claimed; they are never asked. Only Cal and Dee are.
	m := claimed(map[string]string{"cal": "conceded", "dee": "conceded"})
	if !m.Answered() {
		t.Error("both opponents answered; the claiming team owes nothing")
	}
	if got := len(m.AwaitingConcession()); got != 0 {
		t.Fatalf("awaiting = %d, want 0", got)
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
	// Cal has no account. Dee conceding is all the claim can possibly get, and
	// waiting on Cal would mean waiting on somebody with no way to answer —
	// every game their grandmother is in would look stalled for a week.
	m := withoutAccount(claimed(map[string]string{"dee": "conceded"}), "cal")

	if got := len(m.Answerable()); got != 1 {
		t.Fatalf("answerable = %d, want 1 — only Dee can answer", got)
	}
	if got := len(m.AwaitingConcession()); got != 0 {
		t.Errorf("awaiting = %d, want 0 — Cal is not outstanding, Cal is incapable", got)
	}
	if !m.Answered() {
		t.Error("everyone who could answer has; the claim settles")
	}
}

func TestAClaimNobodyCouldAnswerDoesNotRatify(t *testing.T) {
	// Both opponents are placeholders. Nobody can contradict the claim, so
	// ratifying it would record a win on the say-so of the one person who made
	// it — which is the single thing this app exists to refuse. It settles on
	// the clock instead: silence read as agreement, rather than agreement
	// invented out of an empty list.
	m := withoutAccount(withoutAccount(claimed(nil), "cal"), "dee")

	if got := len(m.AwaitingConcession()); got != 0 {
		t.Fatalf("awaiting = %d, want 0", got)
	}
	if m.Answered() {
		t.Error("nobody could answer, so nothing was answered — this must wait out the window")
	}
}

func TestPlaceholdersDoNotContestAnything(t *testing.T) {
	// A placeholder has no way to object either, so a match full of them is not
	// contested. It is unanswered, which is a different thing and is what the
	// clock is for.
	m := withoutAccount(claimed(nil), "cal")
	if m.Contested() {
		t.Error("nobody objected")
	}
}
