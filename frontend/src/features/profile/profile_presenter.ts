import { Runner, Signal } from "@tcn/state/core";

import type { Domain } from "../../lib/domain/domain.js";
import type { FamilyMember } from "../../gen/scoreboard/v1/common_pb.js";
import { key } from "../../lib/domain/store.js";
import { messageOf } from "../match/match_presenter.js";

/**
 * ProfilePresenter shows one player's record inside a family: their W/L,
 * streaks, the games they play, and the two stats that make this fun — their
 * nemesis and their favorite victim.
 *
 * A dynamic child, created when a player is opened and disposed on the way out.
 * The profile cell belongs to the store; the linking state below belongs here.
 *
 * This is also where an administrator links a placeholder to the account of the
 * person it turns out to be, because a placeholder's profile is where anybody
 * actually looks at one — a screen full of a record belonging to somebody who
 * cannot log in to see it.
 */
export class ProfilePresenter {
  private readonly _domain: Domain;
  private readonly _familyId: string;
  private readonly _userId: string;

  /** Set while the "who is this?" sheet is open. */
  private readonly _linking = new Signal<boolean>(false);
  private readonly _link = new Runner<void>(undefined);
  private readonly _error = new Signal<string | null>(null);
  /** How many participations the last link moved, for the confirmation. */
  private readonly _linked = new Signal<number | null>(null);

  constructor(domain: Domain, familyId: string, userId: string) {
    this._domain = domain;
    this._familyId = familyId;
    this._userId = userId;
  }

  get linkingBroadcast() {
    return this._linking.broadcast;
  }

  get linkBroadcast() {
    return this._link.broadcast;
  }

  get errorBroadcast() {
    return this._error.broadcast;
  }

  get linkedBroadcast() {
    return this._linked.broadcast;
  }

  openLink = (): void => {
    this._linking.set(true);
    this._error.set(null);
  };

  cancelLink = (): void => {
    this._linking.set(false);
  };

  /**
   * Accounts this placeholder could be. Everyone in the family who has actually
   * signed up — a placeholder can only ever be linked to a real account, and
   * linking one to another placeholder would just move the problem.
   */
  candidates(members: FamilyMember[]): FamilyMember[] {
    return members.filter((m) => m.user && !m.user.placeholder);
  }

  /**
   * Declares that this placeholder is that account, and hands over everything
   * they did.
   *
   * There is no undo, so the whole store is dropped afterward rather than
   * reasoned about: standings, both profiles and every head-to-head record
   * involving either name changed at once, and working out which cells to
   * invalidate would be a longer list than "all of them" with more ways to be
   * wrong.
   */
  link = async (userId: string): Promise<void> => {
    this._error.set(null);
    try {
      await this._link.execute(async () => {
        const res = await this._domain.api.users.linkPlaceholder({
          placeholderId: this._userId,
          userId,
        });
        this._domain.store.invalidateAll();
        this._linked.set(res.matchesMoved);
        this._linking.set(false);
      });
    } catch (e) {
      this._error.set(messageOf(e));
    }
  };

  get userId(): string {
    return this._userId;
  }

  get familyId(): string {
    return this._familyId;
  }

  get profile() {
    return this._domain.store.profiles.for(key.profile(this._familyId, this._userId));
  }

  /** Every finished game this player was in, newest first. */
  get matches() {
    return this._domain.store.playerMatches.for(key.playerMatches(this._familyId, this._userId));
  }

  get members() {
    return this._domain.store.members.for(this._familyId);
  }

  /** True when this is the signed-in player's own profile. */
  get isMe(): boolean {
    return this._domain.session.user?.id === this._userId;
  }

  load(): Promise<unknown> {
    return Promise.all([this.profile.load(), this.matches.load(), this.members.load()]);
  }

  dispose(): void {
    // The profile cell is owned by the store, not this presenter. These are.
    this._linking.dispose();
    this._link.dispose();
    this._error.dispose();
    this._linked.dispose();
  }
}
