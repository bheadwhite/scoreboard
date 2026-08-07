import { Signal } from "@tcn/state/core";

import type { Domain } from "../../lib/domain/domain.js";
import { key } from "../../lib/domain/store.js";

/**
 * HeadToHeadPresenter drives the money screen: you versus one specific person.
 * The whole app exists so this number can be looked at.
 *
 * A dynamic child, created on navigation and disposed on the way out.
 */
export class HeadToHeadPresenter {
  private readonly _domain: Domain;
  private readonly _familyId: string;
  private readonly _userId: string;
  private readonly _opponentId: string;
  private readonly _gameId = new Signal<string>("");

  constructor(domain: Domain, familyId: string, userId: string, opponentId: string) {
    this._domain = domain;
    this._familyId = familyId;
    this._userId = userId;
    this._opponentId = opponentId;
  }

  get gameIdBroadcast() {
    return this._gameId.broadcast;
  }

  get record() {
    return this._domain.store.headToHead.for(
      key.headToHead(this._familyId, this._userId, this._opponentId, this._gameId.get()),
    );
  }

  get games() {
    return this._domain.store.games.for(this._familyId);
  }

  setGame = (gameId: string): void => {
    this._gameId.set(gameId);
    void this.record.load();
  };

  load(): Promise<unknown> {
    return Promise.all([this.record.load(), this.games.load()]);
  }

  dispose(): void {
    this._gameId.dispose();
  }
}
