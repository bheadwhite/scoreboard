import { Runner, Signal } from "@tcn/state/core";

import { createDomain, type Domain } from "../lib/domain/domain.js";
import { FamiliesPresenter } from "../features/families/families_presenter.js";
import { HeadToHeadPresenter } from "../features/head_to_head/head_to_head_presenter.js";
import { MatchPresenter } from "../features/match/match_presenter.js";
import { NewMatchPresenter } from "../features/match/new_match_presenter.js";
import { ProfilePresenter } from "../features/profile/profile_presenter.js";
import { ScoreboardPresenter } from "../features/scoreboard/scoreboard_presenter.js";

export type Route =
  | { name: "scoreboard" }
  | { name: "new_match" }
  | { name: "match"; matchId: string }
  | { name: "player"; userId: string }
  | { name: "head_to_head"; userId: string; opponentId: string }
  | { name: "family" };

/** Where the last chosen family is remembered between visits. */
const FAMILY_KEY = "scoreboard.familyId";

/**
 * Reads an invite code out of a /join/CODE link.
 *
 * The code lives in the URL rather than storage so it survives being opened in
 * any browser, and so the sign-in round trip can carry it back verbatim.
 */
function inviteCodeFromLocation(): string {
  const match = window.location.pathname.match(/^\/join\/([A-Za-z0-9]{4,12})\/?$/);
  return match?.[1]?.toUpperCase() ?? "";
}

/**
 * AppPresenter is the root of the presenter tree. It creates the domain, owns
 * every feature presenter, holds navigation and the current family, and
 * coordinates the refreshes that cross screens — finalizing a match changes the
 * standings, every head-to-head record and several profiles at once.
 *
 * Permanent children are created up front. Drill-in screens (a match, a player,
 * a rivalry) are dynamic: created on navigation and disposed on the way out.
 */
export class AppPresenter {
  readonly domain: Domain;
  readonly families: FamiliesPresenter;
  readonly scoreboard: ScoreboardPresenter;

  private readonly _route = new Signal<Route>({ name: "scoreboard" });
  private readonly _familyId = new Signal<string>("");
  private readonly _boot = new Runner<void>(undefined);
  private readonly _error = new Signal<string | null>(null);
  /** An invite code from a /join/CODE link, pending the user accepting it. */
  private readonly _invite = new Signal<string>("");

  // Dynamic children, kept in sync with the route.
  private readonly _match = new Signal<MatchPresenter | null>(null);
  private readonly _newMatch = new Signal<NewMatchPresenter | null>(null);
  private readonly _profile = new Signal<ProfilePresenter | null>(null);
  private readonly _headToHead = new Signal<HeadToHeadPresenter | null>(null);

  constructor(domain: Domain = createDomain()) {
    this.domain = domain;
    this.families = new FamiliesPresenter(domain, (familyId) => this.selectFamily(familyId));
    this.scoreboard = new ScoreboardPresenter(domain);
  }

  get session() {
    return this.domain.session;
  }

  get routeBroadcast() {
    return this._route.broadcast;
  }

  get familyIdBroadcast() {
    return this._familyId.broadcast;
  }

  get bootBroadcast() {
    return this._boot.broadcast;
  }

  get errorBroadcast() {
    return this._error.broadcast;
  }

  get inviteBroadcast() {
    return this._invite.broadcast;
  }

  get matchBroadcast() {
    return this._match.broadcast;
  }

  get newMatchBroadcast() {
    return this._newMatch.broadcast;
  }

  get profileBroadcast() {
    return this._profile.broadcast;
  }

  get headToHeadBroadcast() {
    return this._headToHead.broadcast;
  }

  get familyId(): string {
    return this._familyId.get();
  }

  /**
   * Boots the app: establish who is signed in, load their profile and their
   * families, and land on a family if they have one.
   */
  init(): Promise<void> {
    return this._boot.execute(async () => {
      this._error.set(null);
      this.domain.session.init();
      // Captured before anything else touches the URL.
      this._invite.set(inviteCodeFromLocation());

      // The session is an HttpOnly cookie, so the only way to know whether we
      // are signed in is to ask. A rejected GetMe means no valid session.
      try {
        const me = await this.domain.api.users.getMe({});
        this.domain.session.setUser(me.user ?? null);
      } catch {
        this.domain.session.setSignedOut();
        return;
      }

      const families = await this.domain.store.families.load();
      if (families.length === 0) return;

      const remembered = localStorage.getItem(FAMILY_KEY);
      const pick = families.find((f) => f.id === remembered) ?? families[0];
      if (pick) this.selectFamily(pick.id);
    });
  }

  /**
   * Accepts a pending invite. Clears the code and the /join/ URL either way, so
   * a refresh does not re-run a join that already happened.
   */
  acceptInvite = async (): Promise<void> => {
    const code = this._invite.get();
    if (!code) return;

    const familyId = await this.families.joinWithCode(code);
    this._invite.set("");
    window.history.replaceState({}, "", "/");
    if (familyId) this.selectFamily(familyId);
  };

  dismissInvite = (): void => {
    this._invite.set("");
    window.history.replaceState({}, "", "/");
  };

  selectFamily(familyId: string): void {
    if (!familyId) return;
    this._familyId.set(familyId);
    localStorage.setItem(FAMILY_KEY, familyId);
    this.scoreboard.setFamily(familyId);
    this.navigate({ name: "scoreboard" });
  }

  // ── Navigation ─────────────────────────────────────────────────────────────

  navigate = (route: Route): void => {
    this._leaveDynamic();

    switch (route.name) {
      case "match": {
        const presenter = new MatchPresenter(this.domain, route.matchId, () => this.afterMatch());
        this._match.set(presenter);
        void presenter.load();
        presenter.start();
        break;
      }
      case "new_match": {
        const presenter = new NewMatchPresenter(this.domain, this.familyId, (matchId) =>
          this.navigate({ name: "match", matchId }),
        );
        this._newMatch.set(presenter);
        void presenter.load();
        break;
      }
      case "player": {
        const presenter = new ProfilePresenter(this.domain, this.familyId, route.userId);
        this._profile.set(presenter);
        void presenter.load();
        break;
      }
      case "head_to_head": {
        const presenter = new HeadToHeadPresenter(
          this.domain,
          this.familyId,
          route.userId,
          route.opponentId,
        );
        this._headToHead.set(presenter);
        void presenter.load();
        break;
      }
      default:
        break;
    }

    this._route.set(route);
    window.scrollTo({ top: 0 });
  };

  goHome = (): void => {
    this.navigate({ name: "scoreboard" });
  };

  openMatch = (matchId: string): void => {
    this.navigate({ name: "match", matchId });
  };

  openPlayer = (userId: string): void => {
    this.navigate({ name: "player", userId });
  };

  openRivalry = (opponentId: string): void => {
    const me = this.domain.session.user?.id;
    if (!me) return;
    this.navigate({ name: "head_to_head", userId: me, opponentId });
  };

  /**
   * Called when a match settles. Everything downstream of the record changes at
   * once, so the whole cache goes stale and the scoreboard reloads.
   */
  afterMatch(): void {
    this.domain.store.invalidateAll();
    void this.scoreboard.load();
  }

  signOut = async (): Promise<void> => {
    await this.domain.session.signOut();
    this._familyId.set("");
    localStorage.removeItem(FAMILY_KEY);
    this.navigate({ name: "scoreboard" });
  };

  /** Retries the boot sequence after a sign-in or a transient failure. */
  retry = (): Promise<void> => this.init();

  private _leaveDynamic(): void {
    const match = this._match.get();
    if (match) {
      match.dispose();
      this._match.set(null);
    }
    const newMatch = this._newMatch.get();
    if (newMatch) {
      newMatch.dispose();
      this._newMatch.set(null);
    }
    const profile = this._profile.get();
    if (profile) {
      profile.dispose();
      this._profile.set(null);
    }
    const h2h = this._headToHead.get();
    if (h2h) {
      h2h.dispose();
      this._headToHead.set(null);
    }
  }

  dispose(): void {
    this._leaveDynamic();
    this.families.dispose();
    this.scoreboard.dispose();
    this._route.dispose();
    this._familyId.dispose();
    this._boot.dispose();
    this._error.dispose();
    this._invite.dispose();
    this._match.dispose();
    this._newMatch.dispose();
    this._profile.dispose();
    this._headToHead.dispose();
    this.domain.session.dispose();
    this.domain.store.dispose();
  }
}
