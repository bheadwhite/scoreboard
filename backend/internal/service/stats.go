package service

import (
	"context"
	"time"

	"connectrpc.com/connect"

	scoreboardv1 "github.com/bheadwhite/scoreboard/backend/gen/scoreboard/v1"
	"github.com/bheadwhite/scoreboard/backend/internal/model"
	"github.com/bheadwhite/scoreboard/backend/internal/stats"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

type StatsService struct {
	base
}

func NewStatsService(s *store.Store) *StatsService {
	return &StatsService{base: base{store: s}}
}

// How many recent meetings the head-to-head screen shows.
const recentMeetings = 10

func (svc *StatsService) GetStandings(
	ctx context.Context, req *connect.Request[scoreboardv1.GetStandingsRequest],
) (*connect.Response[scoreboardv1.GetStandingsResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	if _, err := svc.requireMember(ctx, familyID, uid); err != nil {
		return nil, err
	}

	ps, users, err := svc.history(ctx, familyID, req.Msg.GetGameId(), req.Msg.GetRange())
	if err != nil {
		return nil, err
	}

	rows := stats.Standings(ps)
	out := make([]*scoreboardv1.StandingsRow, 0, len(rows))
	for i, r := range rows {
		u, ok := users[r.UserID]
		if !ok {
			continue
		}
		out = append(out, &scoreboardv1.StandingsRow{
			User:    userPB(u),
			Wins:    r.Wins,
			Losses:  r.Losses,
			WinRate: r.WinRate(),
			Streak:  r.Streak,
			Rank:    int32(i + 1),
		})
	}

	// Every match contributes one participation per player, so the distinct
	// match count is what "matches played" means here.
	seen := map[string]struct{}{}
	for _, p := range ps {
		seen[p.MatchID] = struct{}{}
	}

	return connect.NewResponse(&scoreboardv1.GetStandingsResponse{
		Rows:         out,
		TotalMatches: int32(len(seen)),
	}), nil
}

func (svc *StatsService) GetHeadToHead(
	ctx context.Context, req *connect.Request[scoreboardv1.GetHeadToHeadRequest],
) (*connect.Response[scoreboardv1.GetHeadToHeadResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	if _, err := svc.requireMember(ctx, familyID, uid); err != nil {
		return nil, err
	}

	subject := req.Msg.GetUserId()
	if subject == "" {
		subject = uid
	}
	opponent := req.Msg.GetOpponentId()
	if opponent == "" {
		return nil, invalid("pick someone to compare against")
	}
	if subject == opponent {
		return nil, invalid("you cannot play yourself")
	}

	ps, users, err := svc.history(ctx, familyID, req.Msg.GetGameId(), req.Msg.GetRange())
	if err != nil {
		return nil, err
	}

	h := stats.HeadToHead(ps, subject, opponent)

	ids := h.MatchIDs
	if len(ids) > recentMeetings {
		ids = ids[:recentMeetings]
	}
	recent, err := svc.store.GetMatchesByIDs(ctx, ids)
	if err != nil {
		return nil, wrapErr(err)
	}

	games, err := svc.gameRecords(ctx, familyID, h.Games)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&scoreboardv1.GetHeadToHeadResponse{
		User:           userPB(users[subject]),
		Opponent:       userPB(users[opponent]),
		Wins:           h.Wins,
		Losses:         h.Losses,
		TogetherWins:   h.TogetherWins,
		TogetherLosses: h.TogetherLosses,
		Games:          games,
		RecentMatches:  matchesPB(recent, uid),
		Streak:         h.Streak,
	}), nil
}

func (svc *StatsService) GetPlayerProfile(
	ctx context.Context, req *connect.Request[scoreboardv1.GetPlayerProfileRequest],
) (*connect.Response[scoreboardv1.GetPlayerProfileResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	if _, err := svc.requireMember(ctx, familyID, uid); err != nil {
		return nil, err
	}

	subject := req.Msg.GetUserId()
	if subject == "" {
		subject = uid
	}

	ps, users, err := svc.history(ctx, familyID, "", req.Msg.GetRange())
	if err != nil {
		return nil, err
	}

	p := stats.PlayerProfile(ps, subject)

	games, err := svc.gameRecords(ctx, familyID, p.Games)
	if err != nil {
		return nil, err
	}

	res := &scoreboardv1.GetPlayerProfileResponse{
		User:             userPB(users[subject]),
		Wins:             p.Wins,
		Losses:           p.Losses,
		CurrentStreak:    p.CurrentStreak,
		LongestWinStreak: p.LongestWin,
		Games:            games,
	}
	total := p.Wins + p.Losses
	if total > 0 {
		res.WinRate = float64(p.Wins) / float64(total)
	}
	if u, ok := users[p.NemesisID]; ok {
		res.Nemesis = userPB(u)
		res.NemesisLosses = p.NemesisLosses
		res.NemesisWins = p.NemesisWins
	}
	if u, ok := users[p.VictimID]; ok {
		res.FavoriteVictim = userPB(u)
		res.VictimWins = p.VictimWins
		res.VictimLosses = p.VictimLosses
	}

	return connect.NewResponse(res), nil
}

// history loads a family's finalized participations along with a lookup of the
// family's members, which is what turns ids in the stats output back into
// people.
func (svc *StatsService) history(
	ctx context.Context, familyID, gameID string, r scoreboardv1.StatsRange,
) ([]stats.Participation, map[string]model.User, error) {
	ps, err := svc.store.LoadParticipations(ctx, familyID, gameID, rangeStart(r, time.Now()))
	if err != nil {
		return nil, nil, wrapErr(err)
	}

	members, err := svc.store.ListMembers(ctx, familyID)
	if err != nil {
		return nil, nil, wrapErr(err)
	}

	users := make(map[string]model.User, len(members))
	for _, m := range members {
		users[m.User.ID] = m.User
	}

	// Someone who has since left the family still appears in old matches, so
	// fill in anyone the member list missed rather than dropping their games.
	for _, p := range ps {
		if _, ok := users[p.UserID]; ok {
			continue
		}
		u, err := svc.store.GetUser(ctx, p.UserID)
		if err != nil {
			continue
		}
		users[p.UserID] = u
	}

	return ps, users, nil
}

// gameRecords resolves a per-game tally into proto, dropping any game that has
// since been deleted.
func (svc *StatsService) gameRecords(
	ctx context.Context, familyID string, recs []stats.GameRecord,
) ([]*scoreboardv1.HeadToHeadGame, error) {
	if len(recs) == 0 {
		return nil, nil
	}

	games, err := svc.store.ListGames(ctx, familyID)
	if err != nil {
		return nil, wrapErr(err)
	}
	byID := make(map[string]model.Game, len(games))
	for _, g := range games {
		byID[g.ID] = g
	}

	out := make([]*scoreboardv1.HeadToHeadGame, 0, len(recs))
	for _, r := range recs {
		g, ok := byID[r.GameID]
		if !ok {
			continue
		}
		out = append(out, &scoreboardv1.HeadToHeadGame{
			Game:   gamePB(g),
			Wins:   r.Wins,
			Losses: r.Losses,
		})
	}
	return out, nil
}
