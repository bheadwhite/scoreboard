import { Runner, Signal } from "@tcn/state/core";

import { createDomain, type Domain } from "../lib/domain/domain.js";
import { fetchDeployedBuild, RUNNING_BUILD } from "../lib/runtime_config.js";
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

/**
 * How often a tab somebody is actually looking at asks whether it has been left
 * behind by a deploy.
 *
 * Only while visible — see watchForUpdates — which is what lets this be a
 * minute rather than the fifteen it used to be. The old interval ran whether or
 * not anybody was there, so it had to be slow enough not to poll all night on a
 * kitchen counter, and the cost of that was landing in the worst place: too
 * frequent to be free, too rare to feel live. Gating on visibility separates
 * the two questions. Nobody watching costs nothing; somebody watching gets an
 * answer inside a minute.
 *
 * A minute is affordable because version.json is about forty bytes and is the
 * one request in the app with nothing to compute — no database, no session, a
 * static file off disk.
 */
const UPDATE_POLL_MS = 60 * 1000;

/**
 * Strips a /join/CODE link back to the app.
 *
 * Those links still work — they are how somebody is told the app exists — but
 * the code in them has already done its job by the time this runs: signing in
 * puts you in the one family, so there is nothing left to accept. Clearing the
 * path stops a refresh from looking like a pending invitation.
 */
function clearJoinPath(): void {
  if (window.location.pathname.startsWith("/join/")) {
    window.history.replaceState({}, "", "/");
  }
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
  /** Set once the server is serving a build newer than this one. */
  private readonly _updateAvailable = new Signal<boolean>(false);
  /** Cleared by dispose, so a torn-down app stops polling. */
  private _updateTimer: ReturnType<typeof setInterval> | null = null;
  private _onVisible: (() => void) | null = null;

  // Dynamic children, kept in sync with the route.
  private readonly _match = new Signal<MatchPresenter | null>(null);
  private readonly _newMatch = new Signal<NewMatchPresenter | null>(null);
  private readonly _profile = new Signal<ProfilePresenter | null>(null);
  private readonly _headToHead = new Signal<HeadToHeadPresenter | null>(null);

  constructor(domain: Domain = createDomain()) {
    this.domain = domain;
    this.families = new FamiliesPresenter(domain);
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

  get updateAvailableBroadcast() {
    return this._updateAvailable.broadcast;
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
   * Boots the app: establish who is signed in and land on the scoreboard they
   * were joined to when they signed in.
   */
  init(): Promise<void> {
    this.watchForUpdates();
    return this._boot.execute(async () => {
      this._error.set(null);
      await this.domain.session.init();
      clearJoinPath();

      // The session is an HttpOnly cookie, so the only way to know whether we
      // are signed in is to ask. A rejected GetMe means no valid session.
      try {
        const me = await this.domain.api.users.getMe({});
        this.domain.session.setUser(me.user ?? null, me.admin);
      } catch {
        this.domain.session.setSignedOut();
        return;
      }

      // There is one family and signing in joins it, so this is a list of one.
      // Empty means the sign-in that should have joined them did not, which the
      // shell says out loud rather than offering a family to create.
      const [family] = await this.domain.store.families.load();
      if (family) this.selectFamily(family.id);
    });
  }

  selectFamily(familyId: string): void {
    if (!familyId) return;
    this._familyId.set(familyId);
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
          // The derived caches go too, because one of the things this event
          // announces is an administrator linking a placeholder to an account —
          // which moves a whole history from one name to another and changes
          // the standings, both profiles and every head-to-head record touching
          // either of them. Membership changes on their own would not need
          // this, and they are rare enough that paying for it is fine.
          this.domain.store.standings.invalidateAll();
          this.domain.store.headToHead.invalidateAll();
          this.domain.store.profiles.invalidateAll();
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
      // A deploy replaces the container and drops every stream, so a reconnect
      // is the earliest sign one happened. Most reconnects are routine — the
      // server closes each stream before Cloud Run's request timeout — and the
      // check costs one small request, so asking on all of them is fine.
      void this.checkForUpdate();
    });
  }

  // ── Deploys ────────────────────────────────────────────────────────────────

  /**
   * Watches for the app being redeployed underneath an open tab.
   *
   * This is a phone-on-the-counter app: a tab sits open for days, the assets it
   * loaded are long gone from the server, and nothing about ordinary use would
   * ever fetch the new ones. Every RPC keeps working — the API is versioned by
   * the protos, not the bundle — so the symptom is not a crash, it is a family
   * member quietly missing a feature everybody else is talking about.
   *
   * Checked on four occasions, most immediate first:
   *
   *   - on a reconnect of the event stream, which is in watchFamily. A deploy
   *     replaces the container and drops every open stream, so this is the
   *     earliest possible hint that something shipped — usually seconds.
   *   - whenever the app comes back to the front, which is what a phone
   *     actually does between glances at the scoreboard,
   *   - every minute while somebody is looking at it,
   *   - and at boot.
   *
   * The timer is deliberately paused while the app is hidden. A backgrounded
   * tab has its timers throttled to somewhere between "slow" and "never"
   * depending on the browser, so polling there was never reliable enough to be
   * the thing that caught a deploy — and coming back to the front runs a check
   * immediately anyway, which covers everything the background poll was
   * nominally for. Dropping it is what makes a one-minute interval reasonable
   * instead of extravagant.
   *
   * Three events for "back to the front", because one is not enough on a phone.
   * visibilitychange is the correct one and fires almost everywhere;
   * pageshow catches a bfcache restore, where the page is resumed wholesale and
   * visibility never transitions; focus catches a desktop window being clicked
   * back into. They overlap heavily and that is fine — checkForUpdate is a
   * no-op once an update is already on offer, and a duplicate forty-byte fetch
   * is not worth deduplicating.
   */
  private watchForUpdates(): void {
    if (this._updateTimer) return;

    void this.checkForUpdate();

    const poll = () => {
      if (document.visibilityState === "visible") void this.checkForUpdate();
    };
    this._updateTimer = setInterval(poll, UPDATE_POLL_MS);

    this._onVisible = poll;
    document.addEventListener("visibilitychange", this._onVisible);
    window.addEventListener("pageshow", this._onVisible);
    window.addEventListener("focus", this._onVisible);
  }

  /**
   * Asks the server which build it is serving.
   *
   * One-way on purpose: once an update is on offer it stays on offer. A tab that
   * learns it is stale does not become fresh again, and withdrawing the banner
   * because a later check failed would be a worse lie than leaving it up.
   */
  private async checkForUpdate(): Promise<void> {
    if (this._updateAvailable.get()) return;
    if (!RUNNING_BUILD) return; // dev server or tests — nothing to compare against

    const deployed = await fetchDeployedBuild();
    if (deployed && deployed !== RUNNING_BUILD) this._updateAvailable.set(true);
  }

  /**
   * Takes the new build. A plain reload is enough: there is no service worker
   * here, and the server sends index.html with `Cache-Control: no-cache`, so the
   * reload revalidates it and picks up the new hashed assets it names.
   *
   * Note what this deliberately does not do: remember which build it was
   * offering. The banner says "there is something newer", never "there is
   * version B" — so if B ships, the banner appears, and C ships before anybody
   * taps it, tapping still lands on C. Whatever the server is serving at the
   * instant of the reload is what gets installed, which is the only answer that
   * cannot go stale while the banner sits there.
   *
   * That property is worth guarding, because it is not really implemented here
   * — it is implemented by two cache headers. index.html must stay `no-cache`
   * so the reload revalidates it, and the assets it names must stay
   * content-hashed so a new build cannot collide with a cached old one. Serve
   * index.html with a max-age and this silently starts reinstalling whatever
   * the browser already had.
   */
  applyUpdate = (): void => {
    window.location.reload();
  };

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
   * clears it if not.
   *
   * Membership can be taken away while somebody is looking at the scoreboard —
   * the organizer removes them. Their client has the family cached, so without
   * this it keeps rendering a scoreboard they can no longer read and every
   * request behind it quietly fails. There is nowhere else to send them: with
   * one family, being removed from it is being removed from the app.
   */
  reconcileMembership = async (): Promise<void> => {
    const current = this._familyId.get();
    if (!current) return;

    const families = await this.domain.store.families.load(true);
    if (families.some((f) => f.id === current)) return;

    this._error.set("You have been removed from the scoreboard.");
    this._unsubscribeRealtime?.();
    this.domain.realtime.disconnect();
    this._familyId.set("");
    this._leaveDynamic();
    this.scoreboard.setFamily("");
    this._route.set({ name: "scoreboard" });
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
    if (this._updateTimer) clearInterval(this._updateTimer);
    this._updateTimer = null;
    if (this._onVisible) {
      document.removeEventListener("visibilitychange", this._onVisible);
      window.removeEventListener("pageshow", this._onVisible);
      window.removeEventListener("focus", this._onVisible);
    }
    this._onVisible = null;
    this._updateAvailable.dispose();
    this._unsubscribeRealtime?.();
    this.domain.realtime.dispose();
    this._leaveDynamic();
    this.families.dispose();
    this.scoreboard.dispose();
    this._route.dispose();
    this._familyId.dispose();
    this._boot.dispose();
    this._error.dispose();
    this._screenTitle.dispose();
    this._match.dispose();
    this._newMatch.dispose();
    this._profile.dispose();
    this._headToHead.dispose();
    this.domain.session.dispose();
    this.domain.store.dispose();
  }
}
