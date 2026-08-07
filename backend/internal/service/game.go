package service

import (
	"context"
	"strings"

	"connectrpc.com/connect"

	scoreboardv1 "github.com/bheadwhite/scoreboard/backend/gen/scoreboard/v1"
	"github.com/bheadwhite/scoreboard/backend/internal/events"
	"github.com/bheadwhite/scoreboard/backend/internal/model"
	"github.com/bheadwhite/scoreboard/backend/internal/store"
)

type GameService struct {
	base
}

func NewGameService(s *store.Store, hub *events.Hub) *GameService {
	return &GameService{base: base{store: s, hub: hub}}
}

func (svc *GameService) ListGames(
	ctx context.Context, req *connect.Request[scoreboardv1.ListGamesRequest],
) (*connect.Response[scoreboardv1.ListGamesResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	if _, err := svc.requireMember(ctx, familyID, uid); err != nil {
		return nil, err
	}

	games, err := svc.store.ListGames(ctx, familyID)
	if err != nil {
		return nil, wrapErr(err)
	}

	out := make([]*scoreboardv1.Game, 0, len(games))
	for _, g := range games {
		out = append(out, gamePB(g))
	}
	return connect.NewResponse(&scoreboardv1.ListGamesResponse{Games: out}), nil
}

const maxGameName = 60

func (svc *GameService) CreateGame(
	ctx context.Context, req *connect.Request[scoreboardv1.CreateGameRequest],
) (*connect.Response[scoreboardv1.CreateGameResponse], error) {
	uid, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	familyID := req.Msg.GetFamilyId()
	if _, err := svc.requireMember(ctx, familyID, uid); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Msg.GetName())
	if name == "" {
		return nil, invalid("give the game a name")
	}
	if len([]rune(name)) > maxGameName {
		return nil, invalid("that name is too long")
	}

	minP, maxP := req.Msg.GetMinPlayers(), req.Msg.GetMaxPlayers()
	if minP <= 0 {
		minP = 2
	}
	if maxP <= 0 {
		maxP = 8
	}
	if minP < 2 {
		return nil, invalid("a game needs at least two players")
	}
	if maxP < minP {
		return nil, invalid("the maximum number of players cannot be below the minimum")
	}

	g, err := svc.store.CreateGame(ctx, familyID, uid, model.Game{
		Name:       name,
		Emoji:      strings.TrimSpace(req.Msg.GetEmoji()),
		TeamBased:  req.Msg.GetTeamBased(),
		MinPlayers: minP,
		MaxPlayers: maxP,
	})
	if err != nil {
		return nil, wrapErr(err)
	}
	svc.publish(familyID, events.KindGames, "")
	return connect.NewResponse(&scoreboardv1.CreateGameResponse{Game: gamePB(g)}), nil
}
