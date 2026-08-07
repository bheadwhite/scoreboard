import { Runner, Signal } from "@tcn/state/core";

import type { Domain } from "../../lib/domain/domain.js";
import type { Match, MatchParticipant } from "../../gen/scoreboard/v1/common_pb.js";
import { MatchStatus } from "../../gen/scoreboard/v1/common_pb.js";

/** One player's standing position on a claim. Every one of them is changeable. */
export type Answer = "claimant" | "conceded" | "disputed" | "silent";

/**
 * MatchPresenter drives one match all the way through: lobby, teams, contract,
 * play, the claim, and the concessions that ratify it.
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

  private _settled = false;
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

  get match(): Match | undefined {
    return this._match.get();
  }

  load(): Promise<Match | undefined> {
    return this._match.execute(async () => {
      const res = await this._domain.api.matches.getMatch({ matchId: this._matchId });
      this._afterUpdate(res.match);
      return res.match;
    });
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
   * Reacts to a new view of the match. Once it is settled there is nothing left
   * to poll for, and the scoreboard behind it needs to be refreshed.
   */
  private _afterUpdate(match: Match | undefined): void {
    if (!match) return;

    // Anything that changes how this match reads in a list — it started, it was
    // claimed, somebody joined — makes the scoreboard behind us wrong. Only
    // finalizing used to invalidate anything, so a started match kept showing
    // as "Not started" with its original headcount until a full reload.
    const signature = `${match.status}:${match.joined.length}:${match.invited.length}`;
    if (signature !== this._seen) {
      this._seen = signature;
      this._domain.store.invalidateMatches(match.familyId);
    }

    const settled =
      match.status === MatchStatus.FINAL || match.status === MatchStatus.CANCELED;
    if (settled && !this._settled) {
      this._settled = true;
      this.stop();
      this._onSettled();
    }
  }

  // ── The lobby ──────────────────────────────────────────────────────────────

  setTeams = (teams: { name: string; userIds: string[] }[]): Promise<void> =>
    this._run(() => this._domain.api.matches.setTeams({ matchId: this._matchId, teams }));

  setContract = (kind: number, target: number, detail: string): Promise<void> =>
    this._run(() =>
      this._domain.api.matches.setContract({
        matchId: this._matchId,
        contract: { kind, target, detail },
      }),
    );

  join = (): Promise<void> =>
    this._run(() => this._domain.api.matches.joinMatch({ matchId: this._matchId }));

  /** Turn down an invitation. The match carries on without you. */
  decline = (): Promise<void> =>
    this._run(() => this._domain.api.matches.declineMatch({ matchId: this._matchId }));

  invitePlayers = (userIds: string[]): Promise<void> =>
    this._run(() =>
      this._domain.api.matches.invitePlayers({ matchId: this._matchId, userIds }),
    );

  leave = (): Promise<void> =>
    this._run(() => this._domain.api.matches.leaveMatch({ matchId: this._matchId }));

  /** Affirm, or withdraw, that you are ready under the current contract. */
  setReady = (ready: boolean): Promise<void> =>
    this._run(() => this._domain.api.matches.setReady({ matchId: this._matchId, ready }));

  start_ = (): Promise<void> =>
    this._run(() => this._domain.api.matches.startMatch({ matchId: this._matchId }));

  cancel = (): Promise<void> =>
    this._run(() => this._domain.api.matches.cancelMatch({ matchId: this._matchId }));

  // ── The concession protocol ────────────────────────────────────────────────

  /**
   * Claims victory for a team. This does not record a win — it opens the claim
   * for the other side to answer.
   */
  claimVictory = (teamId: string): Promise<void> =>
    this._run(() =>
      this._domain.api.matches.claimVictory({ matchId: this._matchId, teamId }),
    );

  /**
   * Takes back a claim your own team made. The match goes back to being in play
   * with no winner — this is "never mind", not "you won". Handing the win over
   * is not something a claimant can do: only the other side can claim it.
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

  resolveDispute = (winningTeamId: string, voidMatch = false): Promise<void> =>
    this._run(() =>
      this._domain.api.matches.resolveDispute({
        matchId: this._matchId,
        winningTeamId,
        voidMatch,
      }),
    );

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
   * "silent" covers two cases the viewer usually cannot tell apart: nobody home,
   * and an objection the server redacted. Who called foul is sent only to the
   * organizer, so this deliberately does not guess — the view says so rather
   * than reporting someone as unanswered when they have in fact answered.
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
              : "silent";
        return { user: p.user, state };
      })
      .sort((a, b) => Number(a.state === "claimant") - Number(b.state === "claimant"));
  }

  /**
   * Whether the viewer's concession would be the one that ends it. The last
   * concession finalizes in the same transaction, and finalizing is the only
   * step in this whole protocol that cannot be taken back — so it is worth
   * saying out loud before they tap.
   *
   * Only sound when no objection stands, which is exactly when it is asked:
   * an objection stops finalization regardless of who else concedes.
   */
  concessionWouldSettleIt(match: Match): boolean {
    return match.status === MatchStatus.CLAIMED && match.awaitingConcession.length <= 1;
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
   * Before it starts, whoever set it up can drop it — that is their own game, so
   * it is an ordinary lobby action. Once play begins only the family organizer
   * can, and that is deliberate: a player who is losing must not be able to
   * erase the game rather than concede it. That would undo the entire point of
   * the app. A finalized match cannot be cancelled by anyone — disputing it is
   * the route, and the organizer resolves that.
   *
   * The two are separated because the organizer deleting somebody else's game is
   * not the same act as the host dropping their own, and it should not be
   * offered by the same one-tap button sitting next to Join.
   */
  cancelAuthority(match: Match): "none" | "creator" | "organizer" {
    const me = this._domain.session.user?.id;
    if (!me) return "none";
    if (match.status === MatchStatus.FINAL || match.status === MatchStatus.CANCELED) return "none";
    if (match.status === MatchStatus.LOBBY && match.createdBy === me) return "creator";
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
