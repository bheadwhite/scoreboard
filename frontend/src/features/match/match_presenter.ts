import { PollAsync, Runner, Signal } from "@tcn/state/core";

import type { Domain } from "../../lib/domain/domain.js";
import type { Match, MatchParticipant } from "../../gen/scoreboard/v1/common_pb.js";
import { MatchStatus } from "../../gen/scoreboard/v1/common_pb.js";

/**
 * How often a live match refreshes, by what the match is doing.
 *
 * Concession is inherently multi-device — you claim victory and three other
 * phones need to light up. Polling handles that at family scale for free, and
 * survives the flaky wifi at somebody's parents' house better than a held-open
 * stream would.
 *
 * The rate is deliberately uneven. The only moment latency is felt is a
 * standing claim, when people are staring at the screen waiting to concede;
 * a match that has been in progress for an hour changes nothing until somebody
 * claims. Polling a flat interval spends most of its requests on the state
 * where nobody is watching.
 */
const POLL_MS: Partial<Record<MatchStatus, number>> = {
  [MatchStatus.CLAIMED]: 2000,
  [MatchStatus.DISPUTED]: 5000,
  [MatchStatus.LOBBY]: 5000,
  [MatchStatus.IN_PROGRESS]: 20000,
};

/** Used before the first load tells us what the match is doing. */
const POLL_DEFAULT_MS = 5000;

function pollInterval(status: MatchStatus | undefined): number {
  return (status !== undefined && POLL_MS[status]) || POLL_DEFAULT_MS;
}

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

  private _poller: PollAsync<Match | undefined> | null = null;
  private _settled = false;
  private _onVisibility: (() => void) | null = null;

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

  /** Begins polling. Called once when the screen mounts. */
  start(): void {
    if (this._poller) return;

    this._poller = new PollAsync<Match | undefined>(
      undefined,
      async () => {
        const res = await this._domain.api.matches.getMatch({ matchId: this._matchId });
        if (res.match) {
          this._match.set(res.match);
          this._afterUpdate(res.match);
        }
        return res.match;
      },
      pollInterval(this._match.get()?.status),
      { startImmediately: true, delayFirstTick: true },
    );

    // Nobody is waiting on a screen they cannot see. Pausing on a hidden tab
    // also stops a backgrounded phone from polling all night.
    this._onVisibility = () => {
      if (this._settled) return;
      if (document.visibilityState === "hidden") {
        this._poller?.stop();
      } else {
        this._poller?.start();
        void this.load();
      }
    };
    document.addEventListener("visibilitychange", this._onVisibility);
  }

  stop(): void {
    this._poller?.stop();
    if (this._onVisibility) {
      document.removeEventListener("visibilitychange", this._onVisibility);
      this._onVisibility = null;
    }
  }

  /**
   * Reacts to a new view of the match. Once it is settled there is nothing left
   * to poll for, and the scoreboard behind it needs to be refreshed.
   */
  private _afterUpdate(match: Match | undefined): void {
    if (!match) return;

    const settled =
      match.status === MatchStatus.FINAL || match.status === MatchStatus.CANCELED;
    if (settled && !this._settled) {
      this._settled = true;
      this.stop();
      this._onSettled();
      return;
    }

    // Re-pace as the match moves between states — most importantly, speed up
    // the moment a claim lands.
    const next = pollInterval(match.status);
    if (this._poller && this._poller.delay !== next) {
      this._poller.setDelay(next);
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

  leave = (): Promise<void> =>
    this._run(() => this._domain.api.matches.leaveMatch({ matchId: this._matchId }));

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

  /** Participants grouped by team, in team order. */
  teamsWithPlayers(match: Match): { id: string; name: string; players: MatchParticipant[] }[] {
    return match.teams.map((team) => ({
      id: team.id,
      name: team.name,
      players: match.participants.filter((p) => p.teamId === team.id),
    }));
  }

  /** The team the signed-in player is on, if any. */
  myTeamId(match: Match): string | null {
    const me = this._domain.session.user?.id;
    if (!me) return null;
    return match.participants.find((p) => p.user?.id === me)?.teamId ?? null;
  }

  amIn(match: Match): boolean {
    return this.myTeamId(match) !== null;
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
    this._poller?.dispose();
    this._poller = null;
    this._match.dispose();
    this._action.dispose();
    this._error.dispose();
    this._confirmingConcede.dispose();
    this._disputing.dispose();
  }
}

/**
 * Connect errors carry a readable message; anything else falls back to
 * something a person can act on.
 */
export function messageOf(e: unknown): string {
  if (e instanceof Error && e.message) {
    // Connect prefixes messages with the code, e.g. "failed_precondition: …".
    const stripped = e.message.replace(/^\[?[a-z_]+\]?:\s*/i, "");
    return stripped || e.message;
  }
  return "Something went wrong. Try again.";
}
