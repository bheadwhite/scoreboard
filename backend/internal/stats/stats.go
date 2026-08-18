// Package stats computes the bragging-rights numbers from finalized matches.
//
// Everything here is a pure function over a flat slice of participations. The
// store does one query for a family's finalized history and this package does
// the rest, which keeps the SQL trivial and makes all of the interesting logic
// testable without a database. At family scale — a few thousand matches, ever —
// there is no reason to push this into the query planner or to materialize it.
//
// # Contested results
//
// A finalized match somebody is still objecting to does not count. Not toward
// wins, not toward losses, not toward a streak, a rivalry, a drought or a
// crown. It is reported separately as a count, so a record reads "14–3, plus 2
// contested" and the number people quote at each other is one nobody at the
// table disputes.
//
// The split happens in exactly one place — Split, called at the top of each
// exported entry point — and everything below it works on a slice that has
// already been filtered. That is deliberate. Threading a "skip if contested"
// test through every function is how HeadToHead and Rivalries would eventually
// come to disagree about which meetings exist, and they have to agree: a
// rivalry counted by one and not the other puts a pairing on a profile that
// the rivalry screen then shows as 0–0. Filtering once, at the door, makes
// disagreeing impossible rather than merely discouraged.
//
// An objection is retractable at any time, including years after the match
// settled, so a result can move between clean and contested at any time. Since
// none of this is materialized, that costs nothing: the next read is right.
package stats

import (
	"sort"

	"github.com/bheadwhite/scoreboard/backend/internal/model"
)

// Participation is one player's row in one finalized match.
type Participation struct {
	MatchID string
	UserID  string
	TeamID  string
	GameID  string
	// model.ResultWin or model.ResultLoss.
	Result string
	// Finalization order; ties broken by MatchID for stability.
	Seq int64
	// Whether somebody is objecting to this match's result. Held out of every
	// number here and counted on its own — see Split.
	Contested bool
}

// Split separates the results everybody accepts from the ones somebody is
// objecting to. Called at the top of every exported function in this package,
// and the only place the distinction is made.
func Split(ps []Participation) (clean, contested []Participation) {
	for _, p := range ps {
		if p.Contested {
			contested = append(contested, p)
		} else {
			clean = append(clean, p)
		}
	}
	return clean, contested
}

// tally counts one player's wins and losses in a slice, which is all a
// contested count ever needs — there is no streak in a set of results nobody
// agrees about, and no rivalry worth deriving from one.
func tally(ps []Participation, userID string) Record {
	var r Record
	for _, p := range ps {
		if p.UserID == userID {
			r.add(p.won())
		}
	}
	return r
}

func (p Participation) won() bool { return p.Result == model.ResultWin }

// Record is a win/loss split.
type Record struct {
	Wins   int32
	Losses int32
}

func (r *Record) add(won bool) {
	if won {
		r.Wins++
	} else {
		r.Losses++
	}
}

func (r Record) total() int32 { return r.Wins + r.Losses }

// PlayerStanding is one row of a family scoreboard.
type PlayerStanding struct {
	UserID string
	Wins   int32
	Losses int32
	// Positive for an active winning streak, negative for a losing one.
	Streak int32
	// Settled matches somebody is objecting to. In neither column above, and
	// not in the streak either — a contested result is not in the sequence at
	// all, so it neither extends a run nor breaks one.
	ContestedWins   int32
	ContestedLosses int32
}

// WinRate returns 0..1, or 0 for a player with no finalized matches.
func (s PlayerStanding) WinRate() float64 {
	total := s.Wins + s.Losses
	if total == 0 {
		return 0
	}
	return float64(s.Wins) / float64(total)
}

// byMatch groups participations by match, each group sorted for determinism.
func byMatch(ps []Participation) map[string][]Participation {
	out := make(map[string][]Participation)
	for _, p := range ps {
		out[p.MatchID] = append(out[p.MatchID], p)
	}
	return out
}

// forPlayer returns one player's participations, oldest first.
func forPlayer(ps []Participation, userID string) []Participation {
	var mine []Participation
	for _, p := range ps {
		if p.UserID == userID {
			mine = append(mine, p)
		}
	}
	sortBySeq(mine)
	return mine
}

func sortBySeq(ps []Participation) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].Seq != ps[j].Seq {
			return ps[i].Seq < ps[j].Seq
		}
		return ps[i].MatchID < ps[j].MatchID
	})
}

// streak counts the current run at the end of an oldest-first sequence:
// positive for wins, negative for losses, zero for an empty history.
func streak(ordered []Participation) int32 {
	if len(ordered) == 0 {
		return 0
	}
	last := ordered[len(ordered)-1].won()
	var n int32
	for i := len(ordered) - 1; i >= 0; i-- {
		if ordered[i].won() != last {
			break
		}
		n++
	}
	if !last {
		return -n
	}
	return n
}

// longestWinStreak returns the best run of wins anywhere in the history.
func longestWinStreak(ordered []Participation) int32 {
	var best, run int32
	for _, p := range ordered {
		if p.won() {
			run++
			if run > best {
				best = run
			}
		} else {
			run = 0
		}
	}
	return best
}

// Standings ranks every player who appears in ps. Order is wins desc, then win
// rate desc, then fewer losses, then user id so the result is stable.
//
// Contested results do not rank anybody. A player who appears only in contested
// matches still gets a row — they played, and their contested count says so —
// but at 0–0, which is the honest reading of a record nobody agrees about.
func Standings(all []Participation) []PlayerStanding {
	ps, contested := Split(all)

	agg := map[string]*PlayerStanding{}
	for _, p := range contested {
		row, ok := agg[p.UserID]
		if !ok {
			row = &PlayerStanding{UserID: p.UserID}
			agg[p.UserID] = row
		}
		if p.won() {
			row.ContestedWins++
		} else {
			row.ContestedLosses++
		}
	}
	for _, p := range ps {
		row, ok := agg[p.UserID]
		if !ok {
			row = &PlayerStanding{UserID: p.UserID}
			agg[p.UserID] = row
		}
		if p.won() {
			row.Wins++
		} else {
			row.Losses++
		}
	}

	out := make([]PlayerStanding, 0, len(agg))
	for id, row := range agg {
		row.Streak = streak(forPlayer(ps, id))
		out = append(out, *row)
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Wins != b.Wins {
			return a.Wins > b.Wins
		}
		if ar, br := a.WinRate(), b.WinRate(); ar != br {
			return ar > br
		}
		if a.Losses != b.Losses {
			return a.Losses < b.Losses
		}
		return a.UserID < b.UserID
	})
	return out
}

// H2H is one player's record against one opponent.
type H2H struct {
	// Meetings on opposing teams, from the subject's point of view.
	Wins   int32
	Losses int32
	// Meetings as teammates.
	TogetherWins   int32
	TogetherLosses int32
	// Per-game breakdown of the opposing meetings, most-played first.
	Games []GameRecord
	// Positive for an active winning streak against this opponent.
	Streak int32
	// Opposing meetings, newest first. Clean ones only — a contested match is
	// not offered as evidence of a record it is not part of.
	MatchIDs []string
	// Opposing meetings somebody is objecting to. In none of the above.
	ContestedWins   int32
	ContestedLosses int32
}

// GameRecord is a record within a single game.
type GameRecord struct {
	GameID string
	Record
}

// HeadToHead computes userID's record against opponentID. This is the query the
// whole app exists for: "how many times have I beaten Dad at Rook."
func HeadToHead(all []Participation, userID, opponentID string) H2H {
	ps, contested := Split(all)
	out := headToHead(ps, userID, opponentID)
	if len(contested) > 0 {
		c := headToHead(contested, userID, opponentID)
		out.ContestedWins, out.ContestedLosses = c.Wins, c.Losses
	}
	return out
}

// headToHead is the tally itself, over a slice already filtered by Split. The
// contested count is the same computation run again over the other half, so
// "which meetings are meetings" — the free-for-all rule below in particular —
// cannot be answered one way for the clean numbers and another for the marked
// ones.
func headToHead(ps []Participation, userID, opponentID string) H2H {
	var out H2H
	games := map[string]*Record{}
	var opposing []Participation

	for _, group := range byMatch(ps) {
		var mine, theirs *Participation
		for i := range group {
			switch group[i].UserID {
			case userID:
				mine = &group[i]
			case opponentID:
				theirs = &group[i]
			}
		}
		if mine == nil || theirs == nil {
			continue
		}

		if mine.TeamID == theirs.TeamID {
			if mine.won() {
				out.TogetherWins++
			} else {
				out.TogetherLosses++
			}
			continue
		}

		// Opposing teams, and both lost: a free-for-all somebody else won.
		//
		// Every player gets a team of one in a free-for-all, so "on opposing
		// teams" is true of everybody who was not on the winning side — and
		// counting that as a defeat would record Kaylee as having beaten Brent
		// in a game she also lost. Neither of them beat the other, so this is
		// not a meeting between them at all: not a result, not a streak, and
		// not a row in their recent meetings.
		if !mine.won() && !theirs.won() {
			continue
		}

		if mine.won() {
			out.Wins++
		} else {
			out.Losses++
		}
		rec, ok := games[mine.GameID]
		if !ok {
			rec = &Record{}
			games[mine.GameID] = rec
		}
		rec.add(mine.won())
		opposing = append(opposing, *mine)
	}

	sortBySeq(opposing)
	out.Streak = streak(opposing)
	for i := len(opposing) - 1; i >= 0; i-- {
		out.MatchIDs = append(out.MatchIDs, opposing[i].MatchID)
	}
	out.Games = rankGames(games)
	return out
}

// Profile is the bragging-rights summary for one player.
type Profile struct {
	Wins          int32
	Losses        int32
	CurrentStreak int32
	LongestWin    int32

	// The opponent who has beaten this player the most.
	NemesisID     string
	NemesisLosses int32
	NemesisWins   int32

	// The opponent this player has beaten the most.
	VictimID     string
	VictimWins   int32
	VictimLosses int32

	// Wins in a row needed to draw level with the nemesis.
	NemesisDebt int32
	// The opponent it has been longest since beating, and the closest rivalry
	// still worth arguing about. Both are absent until earned — a player two
	// matches into their history has no droughts and no unfinished business,
	// and inventing one would be worse than showing nothing.
	Drought       Rivalry
	HasDrought    bool
	Unfinished    Rivalry
	HasUnfinished bool

	Games []GameRecord

	// Settled matches somebody is objecting to. In none of the above — not the
	// totals, not the streaks, not the nemesis, not the drought.
	ContestedWins   int32
	ContestedLosses int32
}

// PlayerProfile summarizes one player's whole record, including the two stats
// that make this fun: your nemesis and your favorite victim.
func PlayerProfile(all []Participation, userID string) Profile {
	ps, contested := Split(all)

	var out Profile
	c := tally(contested, userID)
	out.ContestedWins, out.ContestedLosses = c.Wins, c.Losses

	mine := forPlayer(ps, userID)
	for _, p := range mine {
		if p.won() {
			out.Wins++
		} else {
			out.Losses++
		}
	}
	out.CurrentStreak = streak(mine)
	out.LongestWin = longestWinStreak(mine)

	games := map[string]*Record{}
	for _, p := range mine {
		rec, ok := games[p.GameID]
		if !ok {
			rec = &Record{}
			games[p.GameID] = rec
		}
		rec.add(p.won())
	}
	out.Games = rankGames(games)

	// Every pairing this player has, computed once. The nemesis, the favorite
	// victim, the drought and the unfinished business are four questions asked
	// of the same map.
	versus := rivalries(ps, userID)

	// Nemesis: most losses to. Ties broken by fewer wins against them (a
	// rivalry you are losing more lopsidedly is the better story), then by id.
	for id, rec := range versus {
		if rec.Losses == 0 {
			continue
		}
		if out.NemesisID == "" || rec.Losses > out.NemesisLosses ||
			(rec.Losses == out.NemesisLosses && rec.Wins < out.NemesisWins) ||
			(rec.Losses == out.NemesisLosses && rec.Wins == out.NemesisWins && id < out.NemesisID) {
			out.NemesisID, out.NemesisLosses, out.NemesisWins = id, rec.Losses, rec.Wins
		}
	}
	for id, rec := range versus {
		if rec.Wins == 0 {
			continue
		}
		if out.VictimID == "" || rec.Wins > out.VictimWins ||
			(rec.Wins == out.VictimWins && rec.Losses < out.VictimLosses) ||
			(rec.Wins == out.VictimWins && rec.Losses == out.VictimLosses && id < out.VictimID) {
			out.VictimID, out.VictimWins, out.VictimLosses = id, rec.Wins, rec.Losses
		}
	}

	if out.NemesisID != "" {
		out.NemesisDebt = versus[out.NemesisID].Debt()
	}
	out.Drought, out.HasDrought = Drought(versus)
	out.Unfinished, out.HasUnfinished = UnfinishedBusiness(versus)

	return out
}

// rankGames orders a game tally by most played, then most wins, then id.
func rankGames(m map[string]*Record) []GameRecord {
	out := make([]GameRecord, 0, len(m))
	for id, rec := range m {
		out = append(out, GameRecord{GameID: id, Record: *rec})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.total() != b.total() {
			return a.total() > b.total()
		}
		if a.Wins != b.Wins {
			return a.Wins > b.Wins
		}
		return a.GameID < b.GameID
	})
	return out
}
