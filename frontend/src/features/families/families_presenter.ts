import { Runner, Signal } from "@tcn/state/core";

import type { Domain } from "../../lib/domain/domain.js";
import { messageOf } from "../match/match_presenter.js";

/**
 * FamiliesPresenter handles everything about belonging to a family: the
 * onboarding choice between creating one and joining one, the member list, and
 * the invite code the organizer reads out loud.
 */
export class FamiliesPresenter {
  private readonly _domain: Domain;
  private readonly _onJoined: (familyId: string) => void;

  private readonly _name = new Signal<string>("");
  private readonly _code = new Signal<string>("");
  private readonly _mode = new Signal<"create" | "join">("create");
  private readonly _action = new Runner<void>(undefined);
  private readonly _error = new Signal<string | null>(null);
  private readonly _copied = new Signal<boolean>(false);

  constructor(domain: Domain, onJoined: (familyId: string) => void) {
    this._domain = domain;
    this._onJoined = onJoined;
  }

  get families() {
    return this._domain.store.families;
  }

  get nameBroadcast() {
    return this._name.broadcast;
  }

  get codeBroadcast() {
    return this._code.broadcast;
  }

  get modeBroadcast() {
    return this._mode.broadcast;
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

  setName = (name: string): void => {
    this._name.set(name);
  };

  setCode = (code: string): void => {
    // Invite codes are always upper case, so typing lower case still works.
    this._code.set(code.toUpperCase());
  };

  setMode = (mode: "create" | "join"): void => {
    this._mode.set(mode);
    this._error.set(null);
  };

  create = async (): Promise<void> => {
    const name = this._name.get().trim();
    if (!name) {
      this._error.set("Give your family a name.");
      return;
    }
    await this._act(async () => {
      const res = await this._domain.api.families.createFamily({ name });
      // Force the reload and wait for it. The app decides between onboarding
      // and the scoreboard by looking at this list, so handing over before it
      // contains the new family strands the user on the onboarding screen.
      await this._domain.store.families.load(true);
      this._name.set("");
      if (res.family) this._onJoined(res.family.id);
    });
  };

  join = async (): Promise<void> => {
    const code = this._code.get().trim();
    if (!code) {
      this._error.set("Enter the invite code.");
      return;
    }
    await this._act(async () => {
      const res = await this._domain.api.families.joinFamily({ inviteCode: code });
      await this._domain.store.families.load(true);
      if (res.family) {
        // The roster this player just joined is stale by definition.
        this._domain.store.members.invalidate(res.family.id);
      }
      this._code.set("");
      if (res.family) this._onJoined(res.family.id);
    });
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
    this._name.dispose();
    this._code.dispose();
    this._mode.dispose();
    this._action.dispose();
    this._error.dispose();
    this._copied.dispose();
  }
}
