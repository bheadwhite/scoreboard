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

func TestObjectorsIgnoresTheClaimingTeam(t *testing.T) {
	// A claimant cannot object to their own claim, so even if the column were
	// somehow set it must not read as a dispute.
	m := claimed(nil)
	at := time.Now()
	m.Participants[0].DisputedAt = &at
	if got := len(m.Objectors()); got != 0 {
		t.Fatalf("objectors = %d, want 0 — the claiming team does not answer its own claim", got)
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

func TestDeadlockedNeedsEveryoneToHaveAnswered(t *testing.T) {
	week := 7 * 24 * time.Hour
	claimedAt := time.Now().Add(-time.Hour)

	// Cal objects, Dee has said nothing. Dee conceding could still settle it, so
	// this is a live disagreement and not the organizer's to call.
	m := claimed(map[string]string{"cal": "disputed"})
	m.Status = StatusDisputed
	m.ClaimedAt = &claimedAt
	if m.Deadlocked(week) {
		t.Error("someone has yet to answer; the players are not stuck")
	}

	// Now everyone has had their say and they disagree. Nothing the players can
	// do moves it, short of somebody changing their mind.
	m = claimed(map[string]string{"cal": "disputed", "dee": "conceded"})
	m.Status = StatusDisputed
	m.ClaimedAt = &claimedAt
	if !m.Deadlocked(week) {
		t.Error("everyone answered and they disagree; the organizer should be able to step in")
	}
}

func TestDeadlockedFallsOpenOnceTheClaimOutlivesItsWindow(t *testing.T) {
	week := 7 * 24 * time.Hour
	stale := time.Now().Add(-8 * 24 * time.Hour)

	// An objection stops the clock, so a silent player would otherwise freeze
	// the match forever. Age is the escape hatch.
	m := claimed(map[string]string{"cal": "disputed"})
	m.Status = StatusDisputed
	m.ClaimedAt = &stale
	if !m.Deadlocked(week) {
		t.Error("claim has stood longer than the window; the organizer needs a way in")
	}

	// A window of zero disables the age test rather than making it always true.
	if m.Deadlocked(0) {
		t.Error("no window configured; only the everyone-answered test should apply")
	}
}

func TestDeadlockedIsFalseWithoutAStandingObjection(t *testing.T) {
	week := 7 * 24 * time.Hour
	claimedAt := time.Now().Add(-30 * 24 * time.Hour)

	// Old, and nobody has answered — but nobody is objecting either, so this is
	// silence. Silence belongs to the concession clock, not the organizer.
	m := claimed(nil)
	m.ClaimedAt = &claimedAt
	if m.Deadlocked(week) {
		t.Error("nothing is disputed; there is no deadlock to break")
	}

	// A match still in play is not resolvable however old it is.
	m = claimed(map[string]string{"cal": "disputed"})
	m.Status = StatusInProgress
	m.ClaimedAt = &claimedAt
	if m.Deadlocked(week) {
		t.Error("not disputed; want false")
	}
}
