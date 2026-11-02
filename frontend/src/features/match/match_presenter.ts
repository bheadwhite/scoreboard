import { Code, ConnectError } from "@connectrpc/connect";
import { Runner, Signal } from "@tcn/state/core";

import type { Domain } from "../../lib/domain/domain.js";
import type {
  FamilyMember,
  Match,
  MatchParticipant,
} from "../../gen/scoreboard/v1/common_pb.js";
import { MatchStatus } from "../../gen/scoreboard/v1/common_pb.js";

/**
 * One player's standing position on a result. Every one of them is changeable
 * at any point, forever — except "absent", which is not a position at all.
 *
 * "absent" is a player with no account. They are not sitting on a question they
 * have not got round to, they have no way to answer one, and showing them as
 * silent would report a wait that is never going to end.
 */
export type Answer = "claimant" | "conceded" | "disputed" | "silent" | "absent";

/**
 * MatchPresenter drives one match all the way through: joining, teams, naming
 * the winner, and the answers people take to the result afterward.
 *
 * Two stages, not five. There is no lobby and no ready check — the contract is
 * declared when the match is posted, so joining is the only commitment anybody
 * makes and the match is in play from the moment it exists. And there is no
 * pending claim: naming a winner records the result, and everything that used
 * to happen while a claim ripened now happens to a settled one instead.
 *
 * This is a dynamic child — the app presenter creates it when a match is opened
 * and disposes it on the way out.
 */
export class MatchPresenter {
  private readonly _domain: Domain;
  private readonly _matchId: string;
  private readonly _onSettled: () => void;

  private readonly _match = new Runner<Match | undefined>(undefined);
  private readonly _action = new Runner<void>(undefined);
  private readonly _error = new Signal<string | null>(null);
  /** Set while the "the result is right" confirmation is up. */
  private readonly _confirmingConcede = new Signal<boolean>(false);
  private readonly _disputing = new Signal<boolean>(false);
  /** Set when the match no longer exists — an administrator voided it. */
  private readonly _gone = new Signal<boolean>(false);

  private _settled = false;
  private _unsubscribe: (() => void) | null = null;
  /** Last status and headcount seen, to notice when the lists behind us go stale. */
  private _seen = "";
  /**
   * Last seen answer to "does this match count, and for whom" — status, the
   * side credited, and whether an objection stands. Empty until the first view
   * of the match arrives.
   */
  private _counts = "";

  constructor(domain: Domain, matchId: string, onSettled: () => void) {
    this._domain = domain;
    this._matchId = matchId;
    this._onSettled = onSettled;
  }

  get matchId(): string {
    return this._matchId;
  }

  get matchBroadcast() {
    return this._match.broadcast;
  }

  /** Status of the last mutation, for disabling buttons and showing errors. */
  get actionBroadcast() {
    return this._action.broadcast;
  }

  get errorBroadcast() {
    return this._error.broadcast;
  }

  get confirmingConcedeBroadcast() {
    return this._confirmingConcede.broadcast;
  }

  get disputingBroadcast() {
    return this._disputing.broadcast;
  }

  get goneBroadcast() {
    return this._gone.broadcast;
  }

  get match(): Match | undefined {
    return this._match.get();
  }

  load(): Promise<Match | undefined> {
    return this._match.execute(async () => {
      try {
        const res = await this._domain.api.matches.getMatch({ matchId: this._matchId });
        this._afterUpdate(res.match);
        return res.match;
      } catch (e) {
        // A voided match stops existing rather than changing status, so the
        // reload triggered by the change event is how anybody else's browser
        // finds out. Without this the rejection lands in the runner and the
        // screen keeps rendering a match that is no longer there.
        if (ConnectError.from(e).code !== Code.NotFound) throw e;
        this._markGone();
        return undefined;
      }
    });
  }

  /**
   * Records that the match is gone: stop listening, and tell the app, which
   * drops every cache that might still be counting it.
   */
  private _markGone(): void {
    if (this._gone.get()) return;
    this._gone.set(true);
    this._settled = true;
    this.stop();
    this._onSettled();
  }

  /**
   * Starts listening for changes to this match.
   *
   * No polling: the server pushes when anything about the match changes, and a
   * reconnect re-reads in case something was missed while the connection was
   * down.
   */
  start(): void {
    if (this._settled || this._unsubscribe) return;

    this._unsubscribe = this._domain.realtime.onChange((change) => {
      // Ignore other games happening in the same family.
      if (change.kind !== "match") return;
      if (change.matchId && change.matchId !== this._matchId) return;
      void this.load();
    });
  }

  stop(): void {
    this._unsubscribe?.();
    this._unsubscribe = null;
  }

  /**
   * Reacts to a new view of the match: refresh what is behind it, and notice
   * when the result lands.
   */
  private _afterUpdate(match: Match | undefined): void {
    if (!match) return;

    // Anything that changes how this match reads in a list — somebody joined, a
    // winner was named, somebody objected — makes the scoreboard behind us
    // wrong. Contested is in the signature because it moves the result between
    // the clean numbers and the contested ones, which is exactly the kind of
    // change a list showing those numbers has to be told about.
    const signature =
      `${match.status}:${match.joined.length}:${match.invited.length}:${match.contested}`;
    if (signature !== this._seen) {
      this._seen = signature;
      this._domain.store.invalidateMatches(match.familyId);
    }

    // Whether this match counts, and for whom. When that changes, everything
    // downstream of the record — standings, every head-to-head, several
    // profiles — changed with it, so the whole cache goes stale.
    //
    // A change rather than a one-shot latch on FINAL, which is what this was
    // when finalizing happened once and never came undone. Now a result lands
    // the instant somebody names it, comes off again if the side it credits
    // refuses it, and moves between the clean numbers and the contested ones
    // every time somebody objects or takes an objection back. Every one of
    // those is a standings change, and a latch would report only the first.
    const counts = `${match.status}:${match.claimedByTeamId}:${match.contested}`;
    if (this._counts !== "" && counts !== this._counts) this._onSettled();
    this._counts = counts;

    if (match.status === MatchStatus.CANCELED && !this._settled) {
      this._settled = true;
      this.stop();
      this._onSettled();
    }
  }

  // ── The roster ─────────────────────────────────────────────────────────────

  setTeams = (teams: { name: string; userIds: string[] }[]): Promise<void> =>
    this._run(() => this._domain.api.matches.setTeams({ matchId: this._matchId, teams }));

  join = (): Promise<void> =>
    this._run(() => this._domain.api.matches.joinMatch({ matchId: this._matchId }));

  /**
   * Puts somebody another player invited into the game on their behalf.
   *
   * Only an unanswered invitation, never a refusal, and the server enforces both.
   * Anybody the host adds themselves is already in and never needs this, and
   * anyone put in either way can still object to the result.
   */
  joinFor = (userId: string): Promise<void> =>
    this._run(() => this._domain.api.matches.joinMatch({ matchId: this._matchId, userId }));

  /** Takes somebody else out of the game. The host's alone, until a winner is named. */
  removePlayer = (userId: string): Promise<void> =>
    this._run(() => this._domain.api.matches.leaveMatch({ matchId: this._matchId, userId }));

  /** Turn down an invitation. The match carries on without you. */
  decline = (): Promise<void> =>
    this._run(() => this._domain.api.matches.declineMatch({ matchId: this._matchId }));

  /** Adds players: straight in when the host does it, as invitations otherwise. */
  invitePlayers = (userIds: string[]): Promise<void> =>
    this._run(() =>
      this._domain.api.matches.invitePlayers({ matchId: this._matchId, userIds }),
    );

  /**
   * The family roster, so the match screen can ask more people in.
   *
   * Keyed on the match's own family rather than the app's current one: this
   * presenter is handed a match id and nothing else, and reading a family id
   * from somewhere else is how a screen ends up offering the wrong household's
   * players.
   */
  members(match: Match) {
    return this._domain.store.members.for(match.familyId);
  }

  /**
   * Who is left to ask: everybody in the family who is not already playing,
   * already invited, or already said no.
   *
   * Declining is remembered on purpose — somebody who turned this game down
   * should not reappear in the list of people to pester, which is the whole
   * reason a declined invitation is kept as a row rather than deleted.
   */
  invitable(match: Match, members: FamilyMember[]): FamilyMember[] {
    const spoken = new Set(match.participants.map((p) => p.user?.id));
    return members.filter((m) => m.user && !spoken.has(m.user.id));
  }

  /**
   * Adds somebody who has not signed up, straight into this match.
   *
   * They are joined rather than invited — the server does that, because an
   * invitation to somebody with no device would sit unanswered forever — so
   * this is one call to mint them and one to put them in.
   */
  addPlaceholder = async (match: Match, displayName: string): Promise<void> => {
    this._error.set(null);
    try {
      await this._action.execute(async () => {
        const res = await this._domain.api.families.addPlaceholderPlayer({
          familyId: match.familyId,
          displayName,
        });
        if (!res.user) return;
        this._domain.store.members.invalidate(match.familyId);
        const out = await this._domain.api.matches.invitePlayers({
          matchId: this._matchId,
          userIds: [res.user.id],
        });
        if (out.match) {
          this._match.set(out.match);
          this._afterUpdate(out.match);
        }
      });
    } catch (e) {
      this._error.set(messageOf(e));
    }
  };

  leave = (): Promise<void> =>
    this._run(() => this._domain.api.matches.leaveMatch({ matchId: this._matchId }));

  cancel = (): Promise<void> =>
    this._run(() => this._domain.api.matches.cancelMatch({ matchId: this._matchId }));

  // ── Naming the winner, and answering the result ────────────────────────────

  /**
   * Names a team as the winner, which records the result. There is nothing in
   * between: no pending claim, no window, no waiting on anybody.
   *
   * Any player may name their own side; only whoever posted the match may name
   * anybody else's. The server enforces both, and `canClaimForOthers` mirrors
   * the rule so the UI knows whether to offer the choice.
   */
  claimVictory = (teamId: string): Promise<void> =>
    this._run(() =>
      this._domain.api.matches.claimVictory({ matchId: this._matchId, teamId }),
    );

  /**
   * Refuses the win: the result comes off the board and the match goes back to
   * being in play with nobody credited. This is "never mind", not "you won" —
   * handing the win to the other side is not something this can do, because
   * only somebody entitled to name that side may put it there.
   *
   * Open to the side the result credits, and to nobody else — not to whoever
   * called it. In a match you posted you can name any side, so an author who
   * could retract would be able to take his own loss back off the board. A win
   * can always be refused; a loss can never be erased. `canWithdraw` mirrors
   * the server's rule.
   */
  withdrawClaim = (): Promise<void> =>
    this._run(() => this._domain.api.matches.withdrawClaim({ matchId: this._matchId }));

  askToConcede = (): void => {
    this._confirmingConcede.set(true);
  };

  cancelConcede = (): void => {
    this._confirmingConcede.set(false);
  };

  concede = async (): Promise<void> => {
    await this._run(() => this._domain.api.matches.concede({ matchId: this._matchId }));
    this._confirmingConcede.set(false);
  };

  openDispute = (): void => {
    this._disputing.set(true);
  };

  cancelDispute = (): void => {
    this._disputing.set(false);
  };

  dispute = async (reason: string): Promise<void> => {
    await this._run(() =>
      this._domain.api.matches.disputeMatch({ matchId: this._matchId, reason }),
    );
    this._disputing.set(false);
  };

  /**
   * Erases the match. Administrators only, and the server enforces it — this is
   * gated in the UI on `match.viewerMayVoid`, which the server computes.
   *
   * Unlike every other mutation here there is no updated match to write back:
   * the match is gone, so this reports whether it worked and leaves the caller
   * to navigate away.
   */
  voidMatch = async (reason: string): Promise<boolean> => {
    this._error.set(null);
    try {
      await this._action.execute(async () => {
        await this._domain.api.matches.voidMatch({ matchId: this._matchId, reason });
      });
    } catch (e) {
      this._error.set(messageOf(e));
      return false;
    }
    this._markGone();
    return true;
  };

  // ── Read helpers the views need ────────────────────────────────────────────

  /**
   * The answer the signed-in player currently has on the table.
   *
   * "none" is a position too, and an honest one: it means they have not said
   * anything about this result, which is neither agreement nor objection. So
   * this is shown back to them rather than the controls being hidden once they
   * have answered — every one of these is changeable, in any direction, for as
   * long as the record exists.
   */
  myAnswer(match: Match): "none" | "conceded" | "disputed" {
    const me = this._domain.session.user?.id;
    const p = match.joined.find((x) => x.user?.id === me);
    if (p?.disputedAt) return "disputed";
    if (p?.concededAt) return "conceded";
    return "none";
  }

  /**
   * Where every player stands on the result, the losing side first.
   *
   * "silent" means exactly what it says now. Objections used to be redacted to
   * everyone but the organizer, so this list had to hedge about whether a quiet
   * player was quiet or merely censored; nobody adjudicates an objection any
   * more, so nothing is hidden and the board is simply true.
   */
  answers(match: Match): { user: MatchParticipant["user"]; state: Answer }[] {
    return match.joined
      .map((p) => {
        const claimant = match.claimedByTeamId !== "" && p.teamId === match.claimedByTeamId;
        const state: Answer = claimant
          ? "claimant"
          : p.disputedAt
            ? "disputed"
            : p.concededAt
              ? "conceded"
              : p.user?.placeholder
                ? "absent"
                : "silent";
        return { user: p.user, state };
      })
      .sort((a, b) => Number(a.state === "claimant") - Number(b.state === "claimant"));
  }

  /** Whether anybody the result was recorded against has no way to answer. */
  hasAbsentPlayers(match: Match): boolean {
    return match.joined.some(
      (p) => p.user?.placeholder && p.teamId !== match.claimedByTeamId,
    );
  }

  /**
   * Whether the viewer may name somebody else the winner — the short circuit
   * for a game that is plainly over and where the winner is not the one holding
   * the phone. Mirrors the server rule: creator only.
   */
  canClaimForOthers(match: Match): boolean {
    return match.viewerIsCreator && this.amIn(match);
  }

  /**
   * Whether the viewer may take a position on this result — anybody in the
   * match, at any point in its life, for as long as the record exists.
   *
   * That includes the person credited with the win, and it is not a loophole:
   * whoever posts a match can name any side, so a result can be on the board
   * before the player it credits has looked at their phone. Shutting them out
   * would leave them the only person in the match unable to say it did not
   * happen.
   *
   * With nothing pending in front of a result any more, this is the whole of
   * what protects the record — so it is deliberately as wide as it can be.
   */
  canAnswer(match: Match): boolean {
    return match.status === MatchStatus.FINAL && this.amIn(match);
  }

  /**
   * Whether the viewer may take this result off the board altogether.
   *
   * The side it credits, and nobody else — mirrors the server. A win can always
   * be refused; a loss can never be erased, which is the rule that survives
   * finalization becoming reversible.
   */
  canWithdraw(match: Match): boolean {
    return match.status === MatchStatus.FINAL && match.viewerIsClaimant;
  }

  /** The objections standing against the result, with who made them and why. */
  objections(match: Match): MatchParticipant[] {
    return match.joined.filter((p) => p.disputedAt != null);
  }

  /** Participants grouped by team, in team order. */
  teamsWithPlayers(match: Match): { id: string; name: string; players: MatchParticipant[] }[] {
    return match.teams.map((team) => ({
      id: team.id,
      name: team.name,
      players: match.joined.filter((p) => p.teamId === team.id),
    }));
  }

  /** The team the signed-in player is on, if any. */
  myTeamId(match: Match): string | null {
    const me = this._domain.session.user?.id;
    if (!me) return null;
    return match.joined.find((p) => p.user?.id === me)?.teamId ?? null;
  }

  /** Whether this user id is the signed-in player. */
  isMe(userId: string): boolean {
    return userId === this._domain.session.user?.id;
  }

  amIn(match: Match): boolean {
    return this.myTeamId(match) !== null;
  }

  /** How many players share the signed-in player's side. */
  myTeamSize(match: Match): number {
    const team = this.myTeamId(match);
    if (!team) return 0;
    return match.joined.filter((p) => p.teamId === team).length;
  }

  /**
   * True when the signed-in player is a team of one — which is every
   * free-for-all game, and the reason claiming cannot just say "we".
   */
  amSolo(match: Match): boolean {
    return this.myTeamSize(match) === 1;
  }

  /** Whether the signed-in player runs this family. */
  private get isOrganizer(): boolean {
    const me = this._domain.session.user?.id;
    if (!me) return false;
    const match = this._match.get();
    if (!match) return false;
    const family = this._domain.store.families.peek().find((f) => f.id === match.familyId);
    return family?.ownerId === me;
  }

  /**
   * On what footing, if any, the viewer may call this match off. Mirrors the
   * server rule exactly.
   *
   * It used to split on the lobby: your own game before it started, the
   * organizer's call afterward. There is no lobby to split on now, so it splits
   * on whether anybody else has actually joined — which is the thing the old
   * rule was really about. A match nobody has joined is a post the host can
   * delete. Past that, calling it off takes the game away from other people,
   * and only the organizer does that: a player who is losing must never be able
   * to erase a game rather than lose it, which would undo the entire point of
   * the app. A settled match cannot be called off by anyone — objecting to it
   * is the route, and nobody overrules that.
   *
   * The two are separated because the organizer deleting somebody else's game
   * is not the same act as the host dropping their own, and it should not be
   * offered by the same one-tap button sitting next to Join.
   */
  cancelAuthority(match: Match): "none" | "creator" | "organizer" {
    const me = this._domain.session.user?.id;
    if (!me) return "none";
    if (match.status === MatchStatus.FINAL || match.status === MatchStatus.CANCELED) return "none";
    if (match.joined.length <= 1 && match.createdBy === me) return "creator";
    return this.isOrganizer ? "organizer" : "none";
  }

  canCancel(match: Match): boolean {
    return this.cancelAuthority(match) !== "none";
  }

  /**
   * Every mutation returns the updated match, so the response is written
   * straight into the runner and the UI moves without waiting for the next
   * poll.
   */
  private async _run(call: () => Promise<{ match?: Match }>): Promise<void> {
    this._error.set(null);
    try {
      await this._action.execute(async () => {
        const res = await call();
        if (res.match) {
          this._match.set(res.match);
          this._afterUpdate(res.match);
        }
      });
    } catch (e) {
      this._error.set(messageOf(e));
    }
  }

  dispose(): void {
    this.stop();
    this._match.dispose();
    this._action.dispose();
    this._error.dispose();
    this._confirmingConcede.dispose();
    this._disputing.dispose();
    this._gone.dispose();
  }
}

/**
 * Connect prefixes every error with its code, so the raw message reads
 * "not found: that invite code does not match any family". The code is noise to
 * a player, but only the known codes are stripped — a blanket "text before the
 * first colon" rule would eat legitimate content like "Points to win: 500".
 */
const CONNECT_CODE = new RegExp(
  "^\\[?(" +
    [
      "canceled",
      "unknown",
      "invalid[ _]argument",
      "deadline[ _]exceeded",
      "not[ _]found",
      "already[ _]exists",
      "permission[ _]denied",
      "resource[ _]exhausted",
      "failed[ _]precondition",
      "aborted",
      "out[ _]of[ _]range",
      "unimplemented",
      "internal",
      "unavailable",
      "data[ _]loss",
      "unauthenticated",
    ].join("|") +
    ")\\]?:\\s*",
  "i",
);

export function messageOf(e: unknown): string {
  if (e instanceof Error && e.message) {
    const stripped = e.message.replace(CONNECT_CODE, "").trim();
    return stripped || e.message;
  }
  return "Something went wrong. Try again.";
}
