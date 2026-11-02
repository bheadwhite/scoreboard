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
 *
 * The chosen players are the one filter remembered between visits, per device.
 * Somebody who only plays with two other people should open onto that board
 * rather than having to go and find it, past the one they are behind on.
 */
export class ScoreboardPresenter {
  private readonly _domain: Domain;
  private readonly _onAccessLost: () => void;
  private readonly _familyId = new Signal<string>("");
  private readonly _gameId = new Signal<string>("");
  private readonly _range = new Signal<StatsRange>(StatsRange.ALL_TIME);
  private readonly _playerIds = new Signal<string[]>([]);

  constructor(domain: Domain, onAccessLost: () => void = () => {}) {
    this._domain = domain;
    this._onAccessLost = onAccessLost;
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

  get playerIdsBroadcast() {
    return this._playerIds.broadcast;
  }

  /** The standings cell for the current family, game filter, range and players. */
  get standings() {
    return this._domain.store.standings.for(
      key.standings(
        this._familyId.get(),
        this._gameId.get(),
        this._range.get(),
        this._playerIds.get(),
      ),
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
    if (!familyId) {
      this._familyId.set("");
      this._gameId.set("");
      this._playerIds.set([]);
      return;
    }
    this._familyId.set(familyId);
    // A game filter from the previous family means nothing here.
    this._gameId.set("");
    this._playerIds.set(readPlayers(familyId));
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

  togglePlayer = (userId: string): void => {
    const current = this._playerIds.get();
    this.setPlayers(
      current.includes(userId) ? current.filter((id) => id !== userId) : [...current, userId],
    );
  };

  /** Back to everybody. */
  clearPlayers = (): void => {
    this.setPlayers([]);
  };

  private setPlayers(playerIds: string[]): void {
    this._playerIds.set(playerIds);
    writePlayers(this._familyId.get(), playerIds);
    void this.standings.load();
  }

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
      // Forced: what is happening now is the one thing on this screen that is
      // routinely out of date by the time you look at it.
      this.liveMatches.load(true),
      this.recentMatches.load(),
      this.games.load(),
      this.members.load(),
    ]);
  }

  /**
   * Kept so the view's lifecycle reads the same as before. There is nothing to
   * start: changes arrive over the live connection and land as cache
   * invalidation, which every screen already reacts to.
   */
  start(): void {}

  stop(): void {}

  /** Re-read what is happening right now, ignoring the cache. */
  async refreshLive(): Promise<void> {
    const familyId = this._familyId.get();
    if (!familyId) return;
    try {
      await this._domain.store.liveMatches.for(familyId).load(true);
    } catch {
      this._onAccessLost();
    }
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
    this._playerIds.dispose();
  }
}

const playersKey = (familyId: string) => `scoreboard.players.${familyId}`;

// Storage can be missing or throw (private windows, blocked site data), and
// holds whatever an older build left there. Neither is worth more than falling
// back to everybody.
function readPlayers(familyId: string): string[] {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(playersKey(familyId)) ?? "[]");
    return Array.isArray(parsed) ? parsed.filter((id) => typeof id === "string") : [];
  } catch {
    return [];
  }
}

function writePlayers(familyId: string, playerIds: string[]): void {
  try {
    localStorage.setItem(playersKey(familyId), JSON.stringify(playerIds));
  } catch {
    // Still filtered for this visit; it just will not be remembered.
  }
}
