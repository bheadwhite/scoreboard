import { Runner, Signal } from "@tcn/state/core";

import type { Domain } from "../../lib/domain/domain.js";
import type { Game } from "../../gen/scoreboard/v1/common_pb.js";
import { ContractKind } from "../../gen/scoreboard/v1/common_pb.js";
import { messageOf } from "./match_presenter.js";

/**
 * NewMatchPresenter runs the "start a game" flow: pick the game, say how it is
 * won, pick who is being asked, and post it.
 *
 * The contract is here rather than on the match screen because it is the term
 * everybody who joins is agreeing to, and it is fixed once the match exists.
 * That is what makes joining a complete commitment and why there is nothing to
 * confirm afterward: the terms are on screen before anybody taps, and nobody
 * can edit them out from under the people who already said yes. Teams are still
 * arranged on the match screen, where the people who joined actually are.
 */
export class NewMatchPresenter {
  private readonly _domain: Domain;
  private readonly _familyId: string;
  private readonly _onCreated: (matchId: string) => void;

  private readonly _gameId = new Signal<string>("");
  private readonly _invited = new Signal<Set<string>>(new Set());
  private readonly _contractKind = new Signal<ContractKind>(ContractKind.SINGLE_GAME);
  /**
   * Held as text, not a number. Number("") is 0, so a numeric signal renders a
   * "0" the instant the field is cleared and you can never type a value without
   * selecting the old one first.
   */
  private readonly _contractTarget = new Signal<string>("");
  private readonly _contractDetail = new Signal<string>("");
  /** Whether the "add somebody who is not on the app" sheet is open. */
  private readonly _addingPlayer = new Signal<boolean>(false);
  private readonly _addPlayer = new Runner<void>(undefined);
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

  get contractKindBroadcast() {
    return this._contractKind.broadcast;
  }

  get contractTargetBroadcast() {
    return this._contractTarget.broadcast;
  }

  get contractDetailBroadcast() {
    return this._contractDetail.broadcast;
  }

  get addingPlayerBroadcast() {
    return this._addingPlayer.broadcast;
  }

  get addPlayerBroadcast() {
    return this._addPlayer.broadcast;
  }

  openAddPlayer = (): void => {
    this._addingPlayer.set(true);
    this._error.set(null);
  };

  cancelAddPlayer = (): void => {
    this._addingPlayer.set(false);
  };

  /**
   * Adds somebody who has not signed up, and selects them.
   *
   * They become a real player on the scoreboard — that is the point, since the
   * alternative is entering a four-handed game as three players or not entering
   * it at all. What they cannot do is answer a claim, so adding them to a match
   * joins them outright rather than inviting them; the server does that, since
   * an invitation nobody can answer would sit on the match forever.
   *
   * The roster is reloaded rather than patched, because the new player has to
   * appear in the same list as everybody else — they are not a different kind of
   * row anywhere else in the app and this is not the place to start.
   */
  addPlaceholder = async (displayName: string): Promise<void> => {
    this._error.set(null);
    try {
      await this._addPlayer.execute(async () => {
        const res = await this._domain.api.families.addPlaceholderPlayer({
          familyId: this._familyId,
          displayName,
        });
        this._domain.store.members.invalidate(this._familyId);
        await this.members.load(true);
        if (res.user) {
          const id = res.user.id;
          this._invited.transform((prev) => new Set(prev).add(id));
        }
        this._addingPlayer.set(false);
      });
    } catch (e) {
      this._error.set(messageOf(e));
    }
  };

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

  /** Picking a shape of contract also seeds its number, since the two go together. */
  selectContract = (kind: ContractKind, target: number): void => {
    this._contractKind.set(kind);
    this._contractTarget.set(target > 0 ? String(target) : "");
  };

  setContractTarget = (value: string): void => {
    this._contractTarget.set(value);
  };

  setContractDetail = (value: string): void => {
    this._contractDetail.set(value);
  };

  /** Whether the chosen contract still needs a number or a description. */
  private get contractComplete(): boolean {
    const kind = this._contractKind.get();
    if (kind === ContractKind.FIRST_TO_SCORE || kind === ContractKind.BEST_OF) {
      const n = Number(this._contractTarget.get());
      return Number.isFinite(n) && n > 0;
    }
    if (kind === ContractKind.CUSTOM) return this._contractDetail.get().trim().length > 0;
    return true;
  }

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
   * Everyone invited, plus you. These are the people who *could* play — each
   * still decides for themselves, so this is a ceiling rather than a roster.
   */
  playerCount(): number {
    return this._invited.get().size + 1;
  }

  /**
   * A match needs a game and terms. It does not need players yet: the
   * minimum-players rule belongs at the moment somebody claims a win, once
   * people have actually joined — blocking here would mean you could not even
   * send the invitations.
   */
  canCreate(games: Game[]): boolean {
    const game = this.selectedGame(games);
    if (!game) return false;
    if (!this.contractComplete) return false;
    return this.playerCount() <= game.maxPlayers;
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
          contract: {
            kind: this._contractKind.get(),
            target: Number(this._contractTarget.get()) || 0,
            detail: this._contractDetail.get().trim(),
          },
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
    this._contractKind.dispose();
    this._contractTarget.dispose();
    this._contractDetail.dispose();
    this._addingPlayer.dispose();
    this._addPlayer.dispose();
    this._create.dispose();
    this._error.dispose();
    this._addingGame.dispose();
    this._addGame.dispose();
  }
}
