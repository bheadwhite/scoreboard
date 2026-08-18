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
 * One player's standing position on a claim. Every one of them is changeable —
 * except "absent", which is not a position at all.
 *
 * "absent" is a player with no account. They are not sitting on an unanswered
 * question, they have no way to answer one, and showing them as silent would
 * report a wait that is never going to end. The claim does not wait for them.
 */
export type Answer = "claimant" | "conceded" | "disputed" | "silent" | "absent";

/**
 * MatchPresenter drives one match all the way through: joining, teams, the
 * claim, and the answers that settle it.
 *
 * There is no lobby stage and no ready stage. The contract is declared when the
 * match is posted, so joining is the only commitment anybody makes, and the
 * match is in play from the moment it exists.
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
  /** Set while the concede confirmation is up. */
  private readonly _confirmingConcede = new Signal<boolean>(false);
  private readonly _disputing = new Signal<boolean>(false);
  /** Set when the match no longer exists — an administrator voided it. */
  private readonly _gone = new Signal<boolean>(false);

  private _settled = false;
  /** Whether the scoreboard has already been told this one landed. */
  private _announced = false;
  private _unsubscribe: (() => void) | null = null;
  /** Last status and headcount seen, to notice when the lists behind us go stale. */
  private _seen = "";

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

    // Anything that changes how this match reads in a list — somebody joined,
    // it was claimed, somebody objected — makes the scoreboard behind us wrong.
    // Contested is in the signature because it moves the result between the
    // clean numbers and the contested ones, which is exactly the kind of change
    // a list showing those numbers has to be told about.
    const signature =
      `${match.status}:${match.joined.length}:${match.invited.length}:${match.contested}`;
    if (signature !== this._seen) {
      this._seen = signature;
      this._domain.store.invalidateMatches(match.familyId);
    }

    // Settling is worth announcing once — the standings behind this screen just
    // changed. It is not worth stopping for: a settled match can still be
    // objected to, and the objection can be taken back again, so the screen
    // keeps listening until the match is called off or erased.
    if (match.status === MatchStatus.FINAL && !this._announced) {
      this._announced = true;
      this._onSettled();
    }
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

  /** Turn down an invitation. The match carries on without you. */
  decline = (): Promise<void> =>
    this._run(() => this._domain.api.matches.declineMatch({ matchId: this._matchId }));

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

  // ── The concession protocol ────────────────────────────────────────────────

  /**
   * Names a team as the winner. This does not record a win — it opens the claim
   * for everybody else to answer.
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
   * Takes the claim back off the table. The match goes back to being in play
   * with no winner — this is "never mind", not "you won". Handing the win over
   * is not something a claimant can do: only somebody entitled to name that
   * side can.
   *
   * Open to whoever made the claim and to anyone on the side it named, so being
   * handed a win you did not earn can be refused outright rather than only
   * objected to.
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
   * "none" is a position too — it is the one the clock eventually reads as a
   * concession — so this is what the screen shows back to you, rather than
   * hiding the controls once you have answered. Every one of these is
   * changeable until the match finalizes.
   */
  myAnswer(match: Match): "none" | "conceded" | "disputed" {
    const me = this._domain.session.user?.id;
    const p = match.joined.find((x) => x.user?.id === me);
    if (p?.disputedAt) return "disputed";
    if (p?.concededAt) return "conceded";
    return "none";
  }

  /**
   * Where every player stands on the claim, opponents first.
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

  /** Whether anybody in this match has no way to answer. */
  hasAbsentPlayers(match: Match): boolean {
    return match.joined.some(
      (p) => p.user?.placeholder && p.teamId !== match.claimedByTeamId,
    );
  }

  /**
   * Whether the viewer's answer would be the last one outstanding. The final
   * answer settles the match in the same transaction, and settling is the only
   * step in this whole protocol that cannot be taken back — so it is worth
   * saying out loud before they tap.
   *
   * True of an objection as well as a concession, which is the change that
   * follows from objections no longer blocking: a last objection settles the
   * match exactly as promptly, contested.
   */
  answerWouldSettleIt(match: Match): boolean {
    return match.status === MatchStatus.CLAIMED && match.awaitingConcession.length <= 1;
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
   * Whether the viewer may still answer this result — at any point in the
   * match's life, including long after it settled, which is when most people
   * first see a result they disagree with.
   *
   * Once it has settled that includes the named winner. While the claim is live
   * they have a better door (`withdrawClaim`, which takes the whole thing off
   * the table rather than flagging a result that then stands anyway); after it
   * settles that door is shut, and they would otherwise be the only player in
   * the match unable to say the result is wrong — which matters, because the
   * creator can hand somebody a win they never asked for.
   */
  canAnswer(match: Match): boolean {
    if (match.status === MatchStatus.FINAL) return this.amIn(match);
    if (match.status !== MatchStatus.CLAIMED) return false;
    return this.amIn(match) && !match.viewerIsClaimant;
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
