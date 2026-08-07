import { Runner, Signal } from "@tcn/state/core";

import type { Domain } from "../../lib/domain/domain.js";
import type { Game } from "../../gen/scoreboard/v1/common_pb.js";
import { messageOf } from "./match_presenter.js";

/**
 * NewMatchPresenter runs the "start a game" flow: pick the game, pick who is
 * playing, and create the lobby. Teams and the contract are set on the match
 * screen once the lobby exists.
 */
export class NewMatchPresenter {
  private readonly _domain: Domain;
  private readonly _familyId: string;
  private readonly _onCreated: (matchId: string) => void;

  private readonly _gameId = new Signal<string>("");
  private readonly _invited = new Signal<Set<string>>(new Set());
  private readonly _create = new Runner<void>(undefined);
  private readonly _error = new Signal<string | null>(null);
  /** Whether the "add a game" sheet is open. */
  private readonly _addingGame = new Signal<boolean>(false);
  private readonly _addGame = new Runner<void>(undefined);

  constructor(domain: Domain, familyId: string, onCreated: (matchId: string) => void) {
    this._domain = domain;
    this._familyId = familyId;
    this._onCreated = onCreated;
  }

  get gameIdBroadcast() {
    return this._gameId.broadcast;
  }

  get invitedBroadcast() {
    return this._invited.broadcast;
  }

  get createBroadcast() {
    return this._create.broadcast;
  }

  get errorBroadcast() {
    return this._error.broadcast;
  }

  /** The signed-in player, who is always in the match and cannot be deselected. */
  get meId(): string {
    return this._domain.session.user?.id ?? "";
  }

  get games() {
    return this._domain.store.games.for(this._familyId);
  }

  get members() {
    return this._domain.store.members.for(this._familyId);
  }

  load(): Promise<unknown> {
    // Force the roster rather than trusting cache: somebody joining the family
    // is exactly the thing that happens between opening the app and starting a
    // game, and a stale list means you cannot pick the person standing next to
    // you.
    return Promise.all([this.games.load(), this.members.load(true)]);
  }

  selectGame = (gameId: string): void => {
    this._gameId.set(gameId);
  };

  toggleInvite = (userId: string): void => {
    this._invited.transform((prev) => {
      const next = new Set(prev);
      if (next.has(userId)) {
        next.delete(userId);
      } else {
        next.add(userId);
      }
      return next;
    });
  };

  /** The chosen game, so the view can show its player limits. */
  selectedGame(games: Game[]): Game | null {
    return games.find((g) => g.id === this._gameId.get()) ?? null;
  }

  /**
   * The creator counts toward the player total but is not in the invited set,
   * so headcount is invited + 1.
   */
  playerCount(): number {
    return this._invited.get().size + 1;
  }

  canCreate(games: Game[]): boolean {
    const game = this.selectedGame(games);
    if (!game) return false;
    return this.playerCount() >= game.minPlayers && this.playerCount() <= game.maxPlayers;
  }

  get addingGameBroadcast() {
    return this._addingGame.broadcast;
  }

  get addGameBroadcast() {
    return this._addGame.broadcast;
  }

  openAddGame = (): void => {
    this._addingGame.set(true);
    this._error.set(null);
  };

  cancelAddGame = (): void => {
    this._addingGame.set(false);
  };

  /**
   * Adds a game to this family's own list. The built-in catalog is only a
   * starting point — every family plays something nobody thought to seed.
   * The new game is selected straight away, since adding one is almost always
   * the first step of starting it.
   */
  addGame = async (game: {
    name: string;
    emoji: string;
    teamBased: boolean;
    minPlayers: number;
    maxPlayers: number;
  }): Promise<void> => {
    this._error.set(null);
    try {
      await this._addGame.execute(async () => {
        const res = await this._domain.api.games.createGame({
          familyId: this._familyId,
          ...game,
        });
        this._domain.store.games.invalidate(this._familyId);
        await this.games.load(true);
        if (res.game) this._gameId.set(res.game.id);
        this._addingGame.set(false);
      });
    } catch (e) {
      this._error.set(messageOf(e));
    }
  };

  create = async (): Promise<void> => {
    this._error.set(null);
    try {
      await this._create.execute(async () => {
        const res = await this._domain.api.matches.createMatch({
          familyId: this._familyId,
          gameId: this._gameId.get(),
          inviteUserIds: [...this._invited.get()],
        });
        this._domain.store.invalidateMatches(this._familyId);
        if (res.match) this._onCreated(res.match.id);
      });
    } catch (e) {
      this._error.set(messageOf(e));
    }
  };

  dispose(): void {
    this._gameId.dispose();
    this._invited.dispose();
    this._create.dispose();
    this._error.dispose();
    this._addingGame.dispose();
    this._addGame.dispose();
  }
}
