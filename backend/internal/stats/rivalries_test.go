package stats

import "testing"

// dad has beaten bill three times running since bill's only win.
func longRivalry() []Participation {
	return concat(
		match("m1", 1, "rook", []string{"bill"}, []string{"dad"}),
		match("m2", 2, "rook", []string{"dad"}, []string{"bill"}),
		match("m3", 3, "rook", []string{"dad"}, []string{"bill"}),
		match("m4", 4, "chess", []string{"dad"}, []string{"bill"}),
	)
}

func TestRivalryCountsMeetingsSinceTheLastWin(t *testing.T) {
	r := Rivalries(longRivalry(), "bill")["dad"]

	if r.Wins != 1 || r.Losses != 3 {
		t.Fatalf("record = %d–%d, want 1–3", r.Wins, r.Losses)
	}
	if r.NeverBeaten {
		t.Error("bill has beaten dad once")
	}
	if r.MeetingsSinceLastWin != 3 {
		t.Errorf("meetings since last win = %d, want 3", r.MeetingsSinceLastWin)
	}
	if r.LastWinSeq != 1 {
		t.Errorf("last win seq = %d, want 1", r.LastWinSeq)
	}
	if r.Debt() != 2 {
		t.Errorf("debt = %d, want 2 (3 losses minus 1 win)", r.Debt())
	}
}

func TestRivalryZeroSinceWhenTheLastMeetingWasAWin(t *testing.T) {
	ps := concat(
		match("m1", 1, "rook", []string{"dad"}, []string{"bill"}),
		match("m2", 2, "rook", []string{"bill"}, []string{"dad"}),
	)

	r := Rivalries(ps, "bill")["dad"]
	if r.MeetingsSinceLastWin != 0 {
		t.Errorf("meetings since last win = %d, want 0", r.MeetingsSinceLastWin)
	}
	if r.Debt() != 0 {
		t.Errorf("debt = %d, want 0 — the record is level", r.Debt())
	}
}

func TestRivalryNeverBeatenCountsEveryMeeting(t *testing.T) {
	ps := concat(
		match("m1", 1, "rook", []string{"dad"}, []string{"bill"}),
		match("m2", 2, "rook", []string{"dad"}, []string{"bill"}),
	)

	r := Rivalries(ps, "bill")["dad"]
	if !r.NeverBeaten {
		t.Fatal("bill has never beaten dad")
	}
	if r.MeetingsSinceLastWin != 2 {
		t.Errorf("meetings = %d, want 2", r.MeetingsSinceLastWin)
	}
}

func TestRivalriesIgnoreTeammates(t *testing.T) {
	// Playing alongside dad is not playing dad.
	ps := match("m1", 1, "rook", []string{"bill", "dad"}, []string{"sue"})

	rs := Rivalries(ps, "bill")
	if _, ok := rs["dad"]; ok {
		t.Error("a teammate is not a rival")
	}
	if rs["sue"].Wins != 1 {
		t.Errorf("record against sue = %d–%d, want 1–0", rs["sue"].Wins, rs["sue"].Losses)
	}
}

func TestDroughtPrefersTheLongerOne(t *testing.T) {
	ps := concat(
		longRivalry(), // bill: 3 meetings since beating dad
		match("m5", 5, "rook", []string{"sue"}, []string{"bill"}),
		match("m6", 6, "rook", []string{"sue"}, []string{"bill"}),
	)

	d, ok := Drought(Rivalries(ps, "bill"))
	if !ok {
		t.Fatal("bill has droughts")
	}
	if d.OpponentID != "dad" {
		t.Errorf("drought = %q, want dad (3 meetings beats sue's 2)", d.OpponentID)
	}
}

func TestDroughtBreaksTiesTowardNeverBeaten(t *testing.T) {
	ps := concat(
		// Beaten dad once, then lost twice.
		match("m1", 1, "rook", []string{"bill"}, []string{"dad"}),
		match("m2", 2, "rook", []string{"dad"}, []string{"bill"}),
		match("m3", 3, "rook", []string{"dad"}, []string{"bill"}),
		// Never beaten sue, also two meetings.
		match("m4", 4, "rook", []string{"sue"}, []string{"bill"}),
		match("m5", 5, "rook", []string{"sue"}, []string{"bill"}),
	)

	d, _ := Drought(Rivalries(ps, "bill"))
	if d.OpponentID != "sue" || !d.NeverBeaten {
		t.Errorf("drought = %q (never beaten %v), want sue", d.OpponentID, d.NeverBeaten)
	}
}

func TestDroughtNeedsMoreThanOneMeeting(t *testing.T) {
	ps := match("m1", 1, "rook", []string{"dad"}, []string{"bill"})

	if _, ok := Drought(Rivalries(ps, "bill")); ok {
		t.Error("a single loss is not a drought")
	}
}

func TestUnfinishedBusinessPrefersDeadEvenThenVolume(t *testing.T) {
	ps := concat(
		// bill 2–2 with dad
		match("m1", 1, "rook", []string{"bill"}, []string{"dad"}),
		match("m2", 2, "rook", []string{"bill"}, []string{"dad"}),
		match("m3", 3, "rook", []string{"dad"}, []string{"bill"}),
		match("m4", 4, "rook", []string{"dad"}, []string{"bill"}),
		// bill 1–1 with sue: also level, but fewer meetings
		match("m5", 5, "rook", []string{"bill"}, []string{"sue"}),
		match("m6", 6, "rook", []string{"sue"}, []string{"bill"}),
	)

	u, ok := UnfinishedBusiness(Rivalries(ps, "bill"))
	if !ok {
		t.Fatal("bill has unfinished business")
	}
	if u.OpponentID != "dad" {
		t.Errorf("unfinished business = %q, want dad — 2–2 argues better than 1–1", u.OpponentID)
	}
}

func TestUnfinishedBusinessSkipsLopsidedRecords(t *testing.T) {
	ps := concat(
		match("m1", 1, "rook", []string{"dad"}, []string{"bill"}),
		match("m2", 2, "rook", []string{"dad"}, []string{"bill"}),
		match("m3", 3, "rook", []string{"dad"}, []string{"bill"}),
	)

	if _, ok := UnfinishedBusiness(Rivalries(ps, "bill")); ok {
		t.Error("0–3 is not unfinished business, it is a hiding")
	}
}

func TestGameKingsCrownPerGame(t *testing.T) {
	ps := concat(
		match("m1", 1, "catan", []string{"lisa"}, []string{"bill", "dad"}),
		match("m2", 2, "catan", []string{"lisa"}, []string{"bill"}),
		match("m3", 3, "chess", []string{"dad"}, []string{"bill"}),
	)

	kings := GameKings(ps)
	if len(kings) != 2 {
		t.Fatalf("got %d kings, want one per game", len(kings))
	}
	// Catan is played more, so it leads.
	if kings[0].GameID != "catan" || kings[0].UserID != "lisa" {
		t.Errorf("first king = %s of %s, want lisa of catan", kings[0].UserID, kings[0].GameID)
	}
	if kings[0].Wins != 2 || kings[0].Losses != 0 {
		t.Errorf("lisa's catan record = %d–%d, want 2–0", kings[0].Wins, kings[0].Losses)
	}
	if kings[1].UserID != "dad" {
		t.Errorf("chess king = %s, want dad", kings[1].UserID)
	}
}

func TestGameKingsNeedAWin(t *testing.T) {
	// Everybody in this game has only ever lost it, which happens when the
	// winning side is a team of somebody who never appears again.
	ps := []Participation{
		{MatchID: "m1", UserID: "bill", TeamID: "l", GameID: "rook", Result: "loss", Seq: 1},
	}

	if kings := GameKings(ps); len(kings) != 0 {
		t.Errorf("got %d kings, want none — nobody has won it", len(kings))
	}
}

func TestProfileCarriesTheDebtAndTheDrought(t *testing.T) {
	p := PlayerProfile(longRivalry(), "bill")

	if p.NemesisID != "dad" {
		t.Fatalf("nemesis = %q, want dad", p.NemesisID)
	}
	if p.NemesisDebt != 2 {
		t.Errorf("nemesis debt = %d, want 2", p.NemesisDebt)
	}
	if !p.HasDrought || p.Drought.OpponentID != "dad" {
		t.Errorf("drought = %+v, want dad", p.Drought)
	}
	if p.HasUnfinished {
		t.Errorf("1–3 is not unfinished business, got %+v", p.Unfinished)
	}
}
