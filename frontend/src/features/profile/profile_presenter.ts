import type { Domain } from "../../lib/domain/domain.js";
import { key } from "../../lib/domain/store.js";

/**
 * ProfilePresenter shows one player's record inside a family: their W/L,
 * streaks, the games they play, and the two stats that make this fun — their
 * nemesis and their favorite victim.
 *
 * A dynamic child, created when a player is opened and disposed on the way out.
 * It owns nothing; the profile cell belongs to the store.
 */
export class ProfilePresenter {
  private readonly _domain: Domain;
  private readonly _familyId: string;
  private readonly _userId: string;

  constructor(domain: Domain, familyId: string, userId: string) {
    this._domain = domain;
    this._familyId = familyId;
    this._userId = userId;
  }

  get userId(): string {
    return this._userId;
  }

  get familyId(): string {
    return this._familyId;
  }

  get profile() {
    return this._domain.store.profiles.for(key.profile(this._familyId, this._userId));
  }

  get members() {
    return this._domain.store.members.for(this._familyId);
  }

  /** True when this is the signed-in player's own profile. */
  get isMe(): boolean {
    return this._domain.session.user?.id === this._userId;
  }

  load(): Promise<unknown> {
    return Promise.all([this.profile.load(), this.members.load()]);
  }

  dispose(): void {
    // The profile cell is owned by the store, not this presenter.
  }
}
