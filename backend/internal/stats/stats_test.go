package stats

import (
	"testing"

	"github.com/bheadwhite/scoreboard/backend/internal/model"
)

// match builds the participations for one finalized match. Everyone in winners
// beats everyone in losers.
func match(id string, seq int64, gameID string, winners, losers []string) []Participation {
	var out []Participation
	for _, u := range winners {
		out = append(out, Participation{
			MatchID: id, UserID: u, TeamID: id + "-w", GameID: gameID,
			Result: model.ResultWin, Seq: seq,
		})
	}
	for _, u := range losers {
		out = append(out, Participation{
			MatchID: id, UserID: u, TeamID: id + "-l", GameID: gameID,
			Result: model.ResultLoss, Seq: seq,
		})
	}
	return out
}

// freeForAll builds a match where every player is their own team, which is how
// the app stores a game with no sides. winner beats everybody in losers, and
// the losers did not beat each other.
func freeForAll(id string, seq int64, gameID, winner string, losers ...string) []Participation {
	out := []Participation{{
		MatchID: id, UserID: winner, TeamID: id + "-" + winner, GameID: gameID,
		Result: model.ResultWin, Seq: seq,
	}}
	for _, u := range losers {
		out = append(out, Participation{
			MatchID: id, UserID: u, TeamID: id + "-" + u, GameID: gameID,
			Result: model.ResultLoss, Seq: seq,
		})
	}
	return out
}

func concat(groups ...[]Participation) []Participation {
	var out []Participation
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

func TestStandingsRanksByWinsThenRate(t *testing.T) {
	ps := concat(
		match("m1", 1, "rook", []string{"dad"}, []string{"bill"}),
		match("m2", 2, "rook", []string{"dad"}, []string{"bill"}),
		match("m3", 3, "rook", []string{"sue"}, []string{"bill"}),
		match("m4", 4, "rook", []string{"sue"}, []string{"dad"}),
	)

	rows := Standings(ps)
	if len(rows) != 3 {
		t.Fatalf("expected 3 players, got %d", len(rows))
	}

	// dad and sue both have 2 wins; sue is 2-0 and dad is 2-1, so sue ranks
	// first on win rate.
	if rows[0].UserID != "sue" {
		t.Errorf("expected sue first, got %s", rows[0].UserID)
	}
	if rows[1].UserID != "dad" {
		t.Errorf("expected dad second, got %s", rows[1].UserID)
	}
	if rows[2].UserID != "bill" || rows[2].Wins != 0 || rows[2].Losses != 3 {
		t.Errorf("expected bill 0-3 last, got %+v", rows[2])
	}
}

func TestStandingsWinRateIsZeroForNoMatches(t *testing.T) {
	var row PlayerStanding
	if got := row.WinRate(); got != 0 {
		t.Errorf("expected 0, got %v", got)
	}
}

func TestStreakSignsWinsPositiveAndLossesNegative(t *testing.T) {
	// bill loses, loses, wins, wins, wins → +3.
	ps := concat(
		match("m1", 1, "rook", []string{"dad"}, []string{"bill"}),
		match("m2", 2, "rook", []string{"dad"}, []string{"bill"}),
		match("m3", 3, "rook", []string{"bill"}, []string{"dad"}),
		match("m4", 4, "rook", []string{"bill"}, []string{"dad"}),
		match("m5", 5, "rook", []string{"bill"}, []string{"dad"}),
	)

	rows := Standings(ps)
	byID := map[string]PlayerStanding{}
	for _, r := range rows {
		byID[r.UserID] = r
	}

	if got := byID["bill"].Streak; got != 3 {
		t.Errorf("bill streak: expected 3, got %d", got)
	}
	if got := byID["dad"].Streak; got != -3 {
		t.Errorf("dad streak: expected -3, got %d", got)
	}
}

func TestHeadToHeadCountsOnlyOpposingMeetings(t *testing.T) {
	ps := concat(
		// Opposing: bill beats dad twice, dad beats bill once.
		match("m1", 1, "rook", []string{"bill"}, []string{"dad"}),
		match("m2", 2, "rook", []string{"bill"}, []string{"dad"}),
		match("m3", 3, "rook", []string{"dad"}, []string{"bill"}),
		// Teammates: should land in the "together" bucket, not the record.
		match("m4", 4, "rook", []string{"bill", "dad"}, []string{"sue"}),
		match("m5", 5, "rook", []string{"sue"}, []string{"bill", "dad"}),
		// Irrelevant third-party match.
		match("m6", 6, "rook", []string{"sue"}, []string{"gran"}),
	)

	h := HeadToHead(ps, "bill", "dad")

	if h.Wins != 2 || h.Losses != 1 {
		t.Errorf("expected bill 2-1 vs dad, got %d-%d", h.Wins, h.Losses)
	}
	if h.TogetherWins != 1 || h.TogetherLosses != 1 {
		t.Errorf("expected 1-1 as teammates, got %d-%d", h.TogetherWins, h.TogetherLosses)
	}
	// Most recent opposing meeting (m3) was a loss, so the streak is -1.
	if h.Streak != -1 {
		t.Errorf("expected streak -1, got %d", h.Streak)
	}
	if len(h.MatchIDs) != 3 || h.MatchIDs[0] != "m3" {
		t.Errorf("expected 3 meetings newest-first starting at m3, got %v", h.MatchIDs)
	}
}

func TestHeadToHeadIsSymmetric(t *testing.T) {
	ps := concat(
		match("m1", 1, "rook", []string{"bill"}, []string{"dad"}),
		match("m2", 2, "rook", []string{"dad"}, []string{"bill"}),
		match("m3", 3, "rook", []string{"dad"}, []string{"bill"}),
	)

	forward := HeadToHead(ps, "bill", "dad")
	reverse := HeadToHead(ps, "dad", "bill")

	if forward.Wins != reverse.Losses || forward.Losses != reverse.Wins {
		t.Errorf("expected mirrored records, got %d-%d and %d-%d",
			forward.Wins, forward.Losses, reverse.Wins, reverse.Losses)
	}
}

func TestHeadToHeadSplitsByGame(t *testing.T) {
	ps := concat(
		match("m1", 1, "rook", []string{"bill"}, []string{"dad"}),
		match("m2", 2, "rook", []string{"bill"}, []string{"dad"}),
		match("m3", 3, "rook", []string{"bill"}, []string{"dad"}),
		match("m4", 4, "pingpong", []string{"dad"}, []string{"bill"}),
	)

	h := HeadToHead(ps, "bill", "dad")

	// Most-played game comes first.
	if len(h.Games) != 2 || h.Games[0].GameID != "rook" {
		t.Fatalf("expected rook first, got %+v", h.Games)
	}
	if h.Games[0].Wins != 3 || h.Games[0].Losses != 0 {
		t.Errorf("expected 3-0 at rook, got %d-%d", h.Games[0].Wins, h.Games[0].Losses)
	}
	if h.Games[1].Wins != 0 || h.Games[1].Losses != 1 {
		t.Errorf("expected 0-1 at pingpong, got %d-%d", h.Games[1].Wins, h.Games[1].Losses)
	}
}

func TestPlayerProfileFindsNemesisAndVictim(t *testing.T) {
	ps := concat(
		// Dad beats bill three times — the nemesis.
		match("m1", 1, "rook", []string{"dad"}, []string{"bill"}),
		match("m2", 2, "rook", []string{"dad"}, []string{"bill"}),
		match("m3", 3, "rook", []string{"dad"}, []string{"bill"}),
		// Bill beats gran four times — the favorite victim.
		match("m4", 4, "rook", []string{"bill"}, []string{"gran"}),
		match("m5", 5, "rook", []string{"bill"}, []string{"gran"}),
		match("m6", 6, "rook", []string{"bill"}, []string{"gran"}),
		match("m7", 7, "rook", []string{"bill"}, []string{"gran"}),
		// One loss to sue, so she is a rival but not the nemesis.
		match("m8", 8, "rook", []string{"sue"}, []string{"bill"}),
	)

	p := PlayerProfile(ps, "bill")

	if p.Wins != 4 || p.Losses != 4 {
		t.Errorf("expected bill 4-4, got %d-%d", p.Wins, p.Losses)
	}
	if p.NemesisID != "dad" || p.NemesisLosses != 3 {
		t.Errorf("expected nemesis dad with 3 losses, got %s with %d", p.NemesisID, p.NemesisLosses)
	}
	if p.VictimID != "gran" || p.VictimWins != 4 {
		t.Errorf("expected victim gran with 4 wins, got %s with %d", p.VictimID, p.VictimWins)
	}
	// Bill's last match was a loss to sue.
	if p.CurrentStreak != -1 {
		t.Errorf("expected current streak -1, got %d", p.CurrentStreak)
	}
	if p.LongestWin != 4 {
		t.Errorf("expected longest win streak 4, got %d", p.LongestWin)
	}
}

func TestPlayerProfileTeammatesAreNotOpponents(t *testing.T) {
	// bill and dad are always partners, so neither can be the other's nemesis.
	ps := concat(
		match("m1", 1, "rook", []string{"sue"}, []string{"bill", "dad"}),
		match("m2", 2, "rook", []string{"sue"}, []string{"bill", "dad"}),
	)

	p := PlayerProfile(ps, "bill")

	if p.NemesisID != "sue" {
		t.Errorf("expected nemesis sue, got %q", p.NemesisID)
	}
	if p.NemesisLosses != 2 {
		t.Errorf("expected 2 losses to sue, got %d", p.NemesisLosses)
	}
}

func TestPlayerProfileWithNoHistoryIsEmpty(t *testing.T) {
	p := PlayerProfile(nil, "bill")

	if p.Wins != 0 || p.Losses != 0 || p.CurrentStreak != 0 {
		t.Errorf("expected an empty profile, got %+v", p)
	}
	if p.NemesisID != "" || p.VictimID != "" {
		t.Errorf("expected no nemesis or victim, got %q and %q", p.NemesisID, p.VictimID)
	}
}

func TestPlayerProfileUndefeatedHasNoNemesis(t *testing.T) {
	ps := concat(
		match("m1", 1, "rook", []string{"bill"}, []string{"dad"}),
		match("m2", 2, "rook", []string{"bill"}, []string{"dad"}),
	)

	p := PlayerProfile(ps, "bill")

	if p.NemesisID != "" {
		t.Errorf("expected no nemesis for an undefeated player, got %q", p.NemesisID)
	}
	if p.VictimID != "dad" || p.VictimWins != 2 {
		t.Errorf("expected victim dad 2-0, got %s %d", p.VictimID, p.VictimWins)
	}
}

func TestStandingsIsDeterministicAcrossRuns(t *testing.T) {
	// Identical records must not shuffle between calls; map iteration order in
	// the aggregation step would otherwise leak into the ranking.
	ps := concat(
		match("m1", 1, "rook", []string{"a"}, []string{"b"}),
		match("m2", 2, "rook", []string{"c"}, []string{"d"}),
	)

	first := Standings(ps)
	for i := 0; i < 20; i++ {
		again := Standings(ps)
		for j := range first {
			if first[j].UserID != again[j].UserID {
				t.Fatalf("ranking changed between runs at %d: %s vs %s",
					j, first[j].UserID, again[j].UserID)
			}
		}
	}
}

func TestHeadToHeadIgnoresTwoPlayersWhoBothLost(t *testing.T) {
	// Lisa wins a three-player game. Brent and Kaylee are on opposing teams —
	// everyone is, in a free-for-all — but neither beat the other.
	ps := freeForAll("m1", 1, "catan", "lisa", "brent", "kaylee")

	h := HeadToHead(ps, "brent", "kaylee")
	if h.Wins != 0 || h.Losses != 0 {
		t.Errorf("brent vs kaylee = %d–%d, want 0–0: they both lost to lisa", h.Wins, h.Losses)
	}
	if h.Streak != 0 {
		t.Errorf("streak = %d, want 0 — nothing happened between them", h.Streak)
	}
	if len(h.MatchIDs) != 0 {
		t.Errorf("meetings = %v, want none", h.MatchIDs)
	}
	if len(h.Games) != 0 {
		t.Errorf("per-game records = %v, want none", h.Games)
	}

	// The player who actually won it still counts, both ways.
	won := HeadToHead(ps, "lisa", "brent")
	if won.Wins != 1 || won.Losses != 0 {
		t.Errorf("lisa vs brent = %d–%d, want 1–0", won.Wins, won.Losses)
	}
	lost := HeadToHead(ps, "brent", "lisa")
	if lost.Wins != 0 || lost.Losses != 1 {
		t.Errorf("brent vs lisa = %d–%d, want 0–1", lost.Wins, lost.Losses)
	}
}

func TestPlayerProfileCoLosersAreNotNemeses(t *testing.T) {
	// Two free-for-alls, both won by lisa. Brent and kaylee have never played
	// each other in any sense that should show up on a profile.
	ps := concat(
		freeForAll("m1", 1, "catan", "lisa", "brent", "kaylee"),
		freeForAll("m2", 2, "catan", "lisa", "brent", "kaylee"),
	)

	p := PlayerProfile(ps, "kaylee")
	if p.NemesisID != "lisa" {
		t.Errorf("nemesis = %q, want lisa — she is the one who won them", p.NemesisID)
	}
	if p.HasDrought && p.Drought.OpponentID == "brent" {
		t.Error("brent is not a drought: he never beat kaylee")
	}
	if p.HasUnfinished && p.Unfinished.OpponentID == "brent" {
		t.Error("brent is not unfinished business: they have never faced off")
	}
}
