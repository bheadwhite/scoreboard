import { Runner, Signal } from "@tcn/state/core";

import type { Domain } from "../../lib/domain/domain.js";
import { messageOf } from "../match/match_presenter.js";

/**
 * FamiliesPresenter is everything about the one family: who is in it, the
 * invite link the organizer sends round, and the organizer's two controls.
 *
 * There is no creating, joining or switching. The app is a single scoreboard —
 * signing in puts you in it, which is the whole of the onboarding this used to
 * have a screen for.
 */
export class FamiliesPresenter {
  private readonly _domain: Domain;
  private _watching = "";

  private readonly _action = new Runner<void>(undefined);
  private readonly _error = new Signal<string | null>(null);
  private readonly _copied = new Signal<boolean>(false);

  constructor(domain: Domain) {
    this._domain = domain;
  }

  get families() {
    return this._domain.store.families;
  }

  get actionBroadcast() {
    return this._action.broadcast;
  }

  get errorBroadcast() {
    return this._error.broadcast;
  }

  get copiedBroadcast() {
    return this._copied.broadcast;
  }

  members(familyId: string) {
    return this._domain.store.members.for(familyId);
  }

  /**
   * Remembers which family is on screen so a refresh knows what to re-read.
   * Roster changes arrive over the live connection; there is nothing to poll.
   */
  start(familyId: string): void {
    this._watching = familyId;
  }

  stop(): void {
    this._watching = "";
  }

  async refreshRoster(): Promise<void> {
    const id = this._watching;
    if (!id) return;
    await Promise.all([
      this._domain.store.members.for(id).load(true),
      this._domain.store.families.load(true),
    ]).catch(() => {});
  }

  /** The link you text to a relative. */
  inviteLink(code: string): string {
    return `${window.location.origin}/join/${encodeURIComponent(code)}`;
  }

  /**
   * Hands the invite to the OS share sheet where there is one — on a phone that
   * is the whole point, since the link goes straight into a text message. Falls
   * back to the clipboard on desktop.
   */
  shareInvite = async (code: string): Promise<void> => {
    const url = this.inviteLink(code);
    const text = "Join us on Gauntlet";

    if (typeof navigator.share === "function") {
      try {
        await navigator.share({ title: "Gauntlet", text, url });
        return;
      } catch {
        // Dismissing the share sheet throws; fall through to copying.
      }
    }
    try {
      await navigator.clipboard.writeText(url);
      this._copied.set(true);
      setTimeout(() => this._copied.set(false), 2000);
    } catch {
      // Clipboard access can be refused; the code is on screen anyway.
    }
  };

  regenerateCode = (familyId: string): Promise<void> =>
    this._act(async () => {
      await this._domain.api.families.regenerateInviteCode({ familyId });
      this._domain.store.families.invalidate();
    });

  removeMember = (familyId: string, userId: string): Promise<void> =>
    this._act(async () => {
      await this._domain.api.families.removeMember({ familyId, userId });
      this._domain.store.members.invalidate(familyId);
      this._domain.store.families.invalidate();
    });

  /** Copies the invite code and flashes confirmation for a couple of seconds. */
  copyCode = async (code: string): Promise<void> => {
    try {
      await navigator.clipboard.writeText(code);
      this._copied.set(true);
      setTimeout(() => this._copied.set(false), 2000);
    } catch {
      // Clipboard access can be refused; the code is on screen anyway.
    }
  };

  load(): Promise<unknown> {
    return this._domain.store.families.load();
  }

  private async _act(fn: () => Promise<void>): Promise<void> {
    this._error.set(null);
    try {
      await this._action.execute(fn);
    } catch (e) {
      this._error.set(messageOf(e));
    }
  }

  dispose(): void {
    this.stop();
    this._action.dispose();
    this._error.dispose();
    this._copied.dispose();
  }
}
