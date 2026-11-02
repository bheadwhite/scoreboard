import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Domain } from "../../lib/domain/domain.js";
import { StatsRange } from "../../gen/scoreboard/v1/stats_pb.js";
import { key } from "../../lib/domain/store.js";
import { ScoreboardPresenter } from "./scoreboard_presenter.js";

function setup() {
  const load = vi.fn(() => Promise.resolve());
  const cell = () => ({ load });
  const standingsFor = vi.fn(cell);
  const domain = {
    store: {
      standings: { for: standingsFor },
      liveMatches: { for: cell },
      recentMatches: { for: cell },
      games: { for: cell },
      members: { for: cell },
    },
  } as unknown as Domain;
  return { presenter: new ScoreboardPresenter(domain), standingsFor };
}

describe("ScoreboardPresenter players", () => {
  beforeEach(() => localStorage.clear());

  it("asks for the same cell whichever order players were picked in", () => {
    expect(key.standings("f", "", StatsRange.ALL_TIME, ["b", "a"])).toBe(
      key.standings("f", "", StatsRange.ALL_TIME, ["a", "b"]),
    );
  });

  it("toggles players into and out of the standings cell", () => {
    const { presenter, standingsFor } = setup();
    presenter.setFamily("f");

    presenter.togglePlayer("mom");
    presenter.togglePlayer("kaylee");
    expect(standingsFor).toHaveBeenLastCalledWith(
      key.standings("f", "", StatsRange.ALL_TIME, ["mom", "kaylee"]),
    );

    presenter.togglePlayer("mom");
    expect(standingsFor).toHaveBeenLastCalledWith(
      key.standings("f", "", StatsRange.ALL_TIME, ["kaylee"]),
    );
  });

  it("remembers the choice for the next visit, and forgets it when cleared", () => {
    const first = setup().presenter;
    first.setFamily("f");
    first.togglePlayer("mom");

    const second = setup().presenter;
    second.setFamily("f");
    expect(second.playerIdsBroadcast.get()).toEqual(["mom"]);

    second.clearPlayers();
    const third = setup().presenter;
    third.setFamily("f");
    expect(third.playerIdsBroadcast.get()).toEqual([]);
  });

  it("falls back to everybody when what was stored is unusable", () => {
    localStorage.setItem("scoreboard.players.f", "{not json");
    const { presenter } = setup();
    presenter.setFamily("f");
    expect(presenter.playerIdsBroadcast.get()).toEqual([]);
  });
});
