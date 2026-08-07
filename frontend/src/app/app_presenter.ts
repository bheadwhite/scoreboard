import { Runner, Signal } from "@tcn/state/core";

import { createDomain, type Domain } from "../lib/domain/domain.js";
import type { PreviewInviteResponse } from "../gen/scoreboard/v1/family_pb.js";
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
  /**
   * A title supplied by whichever drill-in screen is open, so the header can
   * say "Catan" rather than "Match". Cleared on every navigation, so a stale
   * title can never outlive the screen that set it.
   */
  private readonly _screenTitle = new Signal<string>("");
  /** Undoes the realtime subscription when the family changes. */
  private _unsubscribeRealtime: (() => void) | null = null;
  /** An invite code from a /join/CODE link, pending the user accepting it. */
  private readonly _invite = new Signal<string>("");
  /** What that code turns out to refer to, once looked up. */
  private readonly _invitePreview = new Runner<PreviewInviteResponse | undefined>(undefined);

  // Dynamic children, kept in sync with the route.
  private readonly _match = new Signal<MatchPresenter | null>(null);
  private readonly _newMatch = new Signal<NewMatchPresenter | null>(null);
  private readonly _profile = new Signal<ProfilePresenter | null>(null);
  private readonly _headToHead = new Signal<HeadToHeadPresenter | null>(null);

  constructor(domain: Domain = createDomain()) {
    this.domain = domain;
    this.families = new FamiliesPresenter(domain, (familyId) => this.selectFamily(familyId));
    this.scoreboard = new ScoreboardPresenter(domain, () => this.reconcileMembership());
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

  get screenTitleBroadcast() {
    return this._screenTitle.broadcast;
  }

  /** Called by a screen once it knows what it is showing. */
  setScreenTitle = (title: string): void => {
    if (this._screenTitle.get() !== title) this._screenTitle.set(title);
  };

  get inviteBroadcast() {
    return this._invite.broadcast;
  }

  get invitePreviewBroadcast() {
    return this._invitePreview.broadcast;
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
      await this.domain.session.init();
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

      // Resolve the invite to a family name so the prompt can say what it is,
      // rather than showing a bare code.
      const code = this._invite.get();
      if (code) {
        try {
          const preview = await this.domain.api.families.previewInvite({ inviteCode: code });
          this._invitePreview.set(preview);
          // Following your own family's link should just take you there.
          if (preview.alreadyMember) this._invite.set("");
        } catch {
          // A dead code still reaches the invite screen, which explains it
          // better than a blank page would.
          this._invitePreview.set(undefined);
        }
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
    this.watchFamily(familyId);
    this.navigate({ name: "scoreboard" });
  }

  /**
   * Opens the live connection for a family and turns what it reports into cache
   * invalidation. Every screen then updates through the machinery it already
   * uses, so nothing needs its own subscription or its own poller.
   */
  private watchFamily(familyId: string): void {
    this._unsubscribeRealtime?.();

    this._unsubscribeRealtime = this.domain.realtime.onChange((change) => {
      switch (change.kind) {
        case "match":
          this.domain.store.invalidateMatches(familyId);
          // A finished match moves the standings, every head-to-head record and
          // several profiles at once, and this event does not say whether it
          // finished. Dropping the derived caches is cheaper than working it out.
          this.domain.store.standings.invalidateAll();
          this.domain.store.headToHead.invalidateAll();
          this.domain.store.profiles.invalidateAll();
          break;
        case "family":
          this.domain.store.members.invalidate(familyId);
          this.domain.store.families.invalidate();
          break;
        case "games":
          this.domain.store.games.invalidate(familyId);
          break;
      }
    });

    // Anything could have changed while the connection was down.
    this.domain.realtime.connect(familyId, () => {
      void this.scoreboard.refreshLive();
      this.domain.store.members.invalidate(familyId);
    });
  }

  // ── Navigation ─────────────────────────────────────────────────────────────

  navigate = (route: Route): void => {
    this._leaveDynamic();
    this._screenTitle.set("");

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
   * Re-checks that the signed-in player is still in the family on screen, and
   * moves them somewhere valid if not.
   *
   * Membership can be taken away while somebody is looking at the scoreboard —
   * the organizer removes them, or they leave from another device. Their client
   * has the family cached, so without this it keeps rendering a scoreboard they
   * can no longer read and every request behind it quietly fails.
   */
  reconcileMembership = async (): Promise<void> => {
    const current = this._familyId.get();
    if (!current) return;

    const families = await this.domain.store.families.load(true);
    if (families.some((f) => f.id === current)) return;

    this._error.set("You are no longer a member of that family.");
    this._unsubscribeRealtime?.();
    this.domain.realtime.disconnect();
    this._familyId.set("");
    localStorage.removeItem(FAMILY_KEY);
    this._leaveDynamic();

    const next = families[0];
    if (next) {
      this.selectFamily(next.id);
    } else {
      // No families left: the shell falls back to onboarding on its own.
      this.scoreboard.setFamily("");
      this._route.set({ name: "scoreboard" });
    }
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
    this._unsubscribeRealtime?.();
    this.domain.realtime.dispose();
    this._leaveDynamic();
    this.families.dispose();
    this.scoreboard.dispose();
    this._route.dispose();
    this._familyId.dispose();
    this._boot.dispose();
    this._error.dispose();
    this._invite.dispose();
    this._invitePreview.dispose();
    this._screenTitle.dispose();
    this._match.dispose();
    this._newMatch.dispose();
    this._profile.dispose();
    this._headToHead.dispose();
    this.domain.session.dispose();
    this.domain.store.dispose();
  }
}
