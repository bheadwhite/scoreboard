import { Signal } from "@tcn/state/core";

import type { Domain } from "../../lib/domain/domain.js";
import { key } from "../../lib/domain/store.js";
import type { Game } from "../../gen/scoreboard/v1/common_pb.js";
import { StatsRange } from "../../gen/scoreboard/v1/stats_pb.js";

/**
 * ScoreboardPresenter drives the family's trophy case: the standings table, the
 * game filter above it, and the matches happening right now.
 *
 * It owns no data of its own — standings, games and matches all live in the
 * store, shared with every other screen. What it owns is which slice of them is
 * on screen.
 */
export class ScoreboardPresenter {
  private readonly _domain: Domain;
  private readonly _familyId = new Signal<string>("");
  private readonly _gameId = new Signal<string>("");
  private readonly _range = new Signal<StatsRange>(StatsRange.ALL_TIME);

  constructor(domain: Domain) {
    this._domain = domain;
  }

  get familyIdBroadcast() {
    return this._familyId.broadcast;
  }

  get gameIdBroadcast() {
    return this._gameId.broadcast;
  }

  get rangeBroadcast() {
    return this._range.broadcast;
  }

  /** The standings cell for the current family, game filter and range. */
  get standings() {
    return this._domain.store.standings.for(
      key.standings(this._familyId.get(), this._gameId.get(), this._range.get()),
    );
  }

  get liveMatches() {
    return this._domain.store.liveMatches.for(this._familyId.get());
  }

  get recentMatches() {
    return this._domain.store.recentMatches.for(this._familyId.get());
  }

  get games() {
    return this._domain.store.games.for(this._familyId.get());
  }

  get members() {
    return this._domain.store.members.for(this._familyId.get());
  }

  setFamily(familyId: string): void {
    if (this._familyId.get() === familyId) return;
    this._familyId.set(familyId);
    // A game filter from the previous family means nothing here.
    this._gameId.set("");
    void this.load();
  }

  setGame = (gameId: string): void => {
    this._gameId.set(gameId);
    void this.standings.load();
  };

  setRange = (range: StatsRange): void => {
    this._range.set(range);
    void this.standings.load();
  };

  /** The currently filtered game, or null for "all games". */
  currentGame(games: Game[]): Game | null {
    const id = this._gameId.get();
    return games.find((g) => g.id === id) ?? null;
  }

  load(): Promise<unknown> {
    const familyId = this._familyId.get();
    if (!familyId) return Promise.resolve();

    return Promise.all([
      this.standings.load(),
      this.liveMatches.load(),
      this.recentMatches.load(),
      this.games.load(),
      this.members.load(),
    ]);
  }

  /** Called after anything that could change the record. */
  refresh(): Promise<unknown> {
    const familyId = this._familyId.get();
    if (!familyId) return Promise.resolve();

    this._domain.store.standings.invalidateAll();
    this._domain.store.invalidateMatches(familyId);
    return this.load();
  }

  dispose(): void {
    // The collections behind this screen are owned by the store, not here.
    this._familyId.dispose();
    this._gameId.dispose();
    this._range.dispose();
  }
}
