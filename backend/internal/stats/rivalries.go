package stats

import "sort"

// The rivalry layer: the same finalized matches as everything else, read as
// stories rather than tallies.
//
// A record of 3–7 tells you who is better. It does not tell you that the last
// six meetings all went the wrong way, that four straight wins would be needed
// to draw level, or that somebody has never been beaten at all. Those are the
// lines people screenshot, and they are all one pass over the pairings.

// Rivalry is one pairing from a subject player's point of view. Only meetings
// on opposing teams count — being on Dad's team is not playing Dad.
type Rivalry struct {
	OpponentID string
	Record

	// Meetings since the subject last beat this opponent. Zero when the most
	// recent meeting was a win, which is the only comfortable answer.
	MeetingsSinceLastWin int32
	// True when the subject has never beaten them at all. No count of meetings
	// says that, and it is the more damning line.
	NeverBeaten bool
	// Finalization order of that last win; zero when NeverBeaten.
	LastWinSeq int64
}

// Meetings is how many times these two have faced each other.
func (r Rivalry) Meetings() int32 { return r.Wins + r.Losses }

// Margin is positive when the subject is ahead.
func (r Rivalry) Margin() int32 { return r.Wins - r.Losses }

// Debt is how many wins in a row it would take to draw level. Zero when the
// subject is already level or ahead.
func (r Rivalry) Debt() int32 {
	if r.Losses <= r.Wins {
		return 0
	}
	return r.Losses - r.Wins
}

// Rivalries tallies userID against everyone they have faced, keyed by opponent.
//
// One pass, shared by the nemesis, the favorite victim, the drought and the
// unfinished business — four stats that are four different questions asked of
// the same pairings.
//
// Clean results only. A drought counted off matches somebody is objecting to
// would be a needling sentence built on a result the person being needled does
// not accept, which is the worst possible place to be confidently wrong.
func Rivalries(all []Participation, userID string) map[string]Rivalry {
	clean, _ := Split(all)
	return rivalries(clean, userID)
}

// rivalries is the pass itself, over a slice already filtered by Split.
func rivalries(ps []Participation, userID string) map[string]Rivalry {
	// Opposing meetings per opponent, in finalization order.
	meetings := map[string][]Participation{}

	for _, group := range byMatch(ps) {
		var me *Participation
		for i := range group {
			if group[i].UserID == userID {
				me = &group[i]
				break
			}
		}
		if me == nil {
			continue
		}
		for i := range group {
			other := group[i]
			if other.UserID == userID || other.TeamID == me.TeamID {
				continue
			}
			// Both on the losing side of a free-for-all somebody else won.
			// Neither beat the other, so it is not a meeting between them —
			// see HeadToHead, which has to apply the same rule to agree.
			if !me.won() && !other.won() {
				continue
			}
			meetings[other.UserID] = append(meetings[other.UserID], *me)
		}
	}

	out := make(map[string]Rivalry, len(meetings))
	for opponent, ms := range meetings {
		sortBySeq(ms)

		r := Rivalry{OpponentID: opponent, NeverBeaten: true}
		for _, m := range ms {
			r.add(m.won())
		}

		// Walk back from the most recent meeting to the last win. What is
		// wanted is how many meetings have happened *since* it, so the win
		// itself does not count.
		for i := len(ms) - 1; i >= 0; i-- {
			if ms[i].won() {
				r.NeverBeaten = false
				r.LastWinSeq = ms[i].Seq
				r.MeetingsSinceLastWin = int32(len(ms) - 1 - i)
				break
			}
		}
		if r.NeverBeaten {
			r.MeetingsSinceLastWin = int32(len(ms))
		}

		out[opponent] = r
	}
	return out
}

// Enough meetings for a line about the pairing to be worth printing. Below
// this, every stat here is noise dressed up as a story: one loss is not a
// drought and a 1–0 lead is not a rivalry.
const minMeetings = 2

// Drought is the opponent it has gone longest without beating, measured in
// meetings rather than days — "six meetings and counting" lands harder than a
// date, and it cannot be gamed by not playing.
//
// Never having beaten them at all wins ties: it is the worse position to be in,
// and the better sentence.
func Drought(rs map[string]Rivalry) (Rivalry, bool) {
	var best Rivalry
	var found bool

	for _, r := range rs {
		if r.MeetingsSinceLastWin < minMeetings {
			continue
		}
		if !found || better(r, best) {
			best, found = r, true
		}
	}
	return best, found
}

// better ranks two droughts: more meetings since a win, then never-beaten,
// then more losses, then id so the answer is stable.
func better(a, b Rivalry) bool {
	if a.MeetingsSinceLastWin != b.MeetingsSinceLastWin {
		return a.MeetingsSinceLastWin > b.MeetingsSinceLastWin
	}
	if a.NeverBeaten != b.NeverBeaten {
		return a.NeverBeaten
	}
	if a.Losses != b.Losses {
		return a.Losses > b.Losses
	}
	return a.OpponentID < b.OpponentID
}

// UnfinishedBusiness is the closest rivalry with enough meetings to argue
// about: level, or one match from it. This is the pairing that should be
// played again, which is the point of surfacing it.
func UnfinishedBusiness(rs map[string]Rivalry) (Rivalry, bool) {
	var best Rivalry
	var found bool

	for _, r := range rs {
		if r.Meetings() < minMeetings {
			continue
		}
		if abs(r.Margin()) > 1 {
			continue
		}
		if !found || closer(r, best) {
			best, found = r, true
		}
	}
	return best, found
}

// closer ranks two candidates: dead even beats one apart, then more meetings —
// a 6–6 is a better argument than a 1–1 — then id.
func closer(a, b Rivalry) bool {
	if abs(a.Margin()) != abs(b.Margin()) {
		return abs(a.Margin()) < abs(b.Margin())
	}
	if a.Meetings() != b.Meetings() {
		return a.Meetings() > b.Meetings()
	}
	return a.OpponentID < b.OpponentID
}

func abs(n int32) int32 {
	if n < 0 {
		return -n
	}
	return n
}

// GameKing is who owns one game.
type GameKing struct {
	GameID string
	UserID string
	Record
}

// GameKings returns the best record in each game that has been played, ordered
// by how much that game gets played.
//
// Deliberately per game rather than overall: "Lisa owns Catan" is a claim
// somebody can do something about, where a single overall champion is just the
// top of the table again. Which is also why contested matches are dropped
// first: a crown is the loudest thing on the scoreboard, and one handed over on
// a result its loser is publicly objecting to is the exact claim nobody can do
// anything about.
func GameKings(all []Participation) []GameKing {
	ps, _ := Split(all)

	// game → player → record
	byGame := map[string]map[string]*Record{}
	played := map[string]int32{}

	for _, p := range ps {
		players, ok := byGame[p.GameID]
		if !ok {
			players = map[string]*Record{}
			byGame[p.GameID] = players
		}
		rec, ok := players[p.UserID]
		if !ok {
			rec = &Record{}
			players[p.UserID] = rec
		}
		rec.add(p.won())
		played[p.GameID]++
	}

	var out []GameKing
	for gameID, players := range byGame {
		var king GameKing
		var found bool
		for userID, rec := range players {
			// Somebody has to have won a game to own it. A table where
			// everybody has only lost has no king, which happens in team games
			// where the winning side is a team nobody is on twice.
			if rec.Wins == 0 {
				continue
			}
			candidate := GameKing{GameID: gameID, UserID: userID, Record: *rec}
			if !found || crowns(candidate, king) {
				king, found = candidate, true
			}
		}
		if found {
			out = append(out, king)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if played[out[i].GameID] != played[out[j].GameID] {
			return played[out[i].GameID] > played[out[j].GameID]
		}
		return out[i].GameID < out[j].GameID
	})
	return out
}

// crowns ranks two claims on a game: more wins, then a better rate, then fewer
// losses, then id.
func crowns(a, b GameKing) bool {
	if a.Wins != b.Wins {
		return a.Wins > b.Wins
	}
	ar := float64(a.Wins) / float64(a.total())
	br := float64(b.Wins) / float64(b.total())
	if ar != br {
		return ar > br
	}
	if a.Losses != b.Losses {
		return a.Losses < b.Losses
	}
	return a.UserID < b.UserID
}
