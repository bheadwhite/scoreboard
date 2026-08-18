import type { Contract, Game, Match, User } from "../gen/scoreboard/v1/common_pb.js";
import { ContractKind, MatchStatus } from "../gen/scoreboard/v1/common_pb.js";
import type { Timestamp } from "@bufbuild/protobuf/wkt";

/** Initials for the avatar fallback, at most two letters. */
export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  const first = parts[0]?.[0] ?? "";
  const last = parts.length > 1 ? (parts[parts.length - 1]?.[0] ?? "") : "";
  return (first + last).toUpperCase();
}

export function displayName(user: User | undefined): string {
  return user?.displayName?.trim() || "Someone";
}

export function toDate(ts: Timestamp | undefined): Date | null {
  if (!ts) return null;
  return new Date(Number(ts.seconds) * 1000 + Math.floor(ts.nanos / 1e6));
}

/** "just now", "12m ago", "3d ago" — short enough to sit inside a row. */
export function timeAgo(ts: Timestamp | undefined): string {
  const date = toDate(ts);
  if (!date) return "";

  const seconds = Math.floor((Date.now() - date.getTime()) / 1000);
  if (seconds < 45) return "just now";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  if (seconds < 604800) return `${Math.floor(seconds / 86400)}d ago`;
  return date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

/** How much of the concession window is left, phrased for a person. */
export function timeLeft(ts: Timestamp | undefined): string {
  const date = toDate(ts);
  if (!date) return "";

  const seconds = Math.floor((date.getTime() - Date.now()) / 1000);
  if (seconds <= 0) return "any moment now";
  if (seconds < 3600) return `${Math.max(1, Math.floor(seconds / 60))} min`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} hr`;
  return `${Math.floor(seconds / 86400)} days`;
}

/** The contract in plain words: "First to 500", "Best of 3". */
export function contractText(contract: Contract | undefined): string {
  if (!contract) return "No contract set";

  switch (contract.kind) {
    case ContractKind.FIRST_TO_SCORE:
      return `First to ${contract.target}`;
    case ContractKind.BEST_OF:
      return `Best of ${contract.target}`;
    case ContractKind.LAST_STANDING:
      return "Last one standing";
    case ContractKind.SINGLE_GAME:
      return "Single game";
    case ContractKind.CUSTOM:
      return contract.detail || "Custom rules";
    default:
      return "No contract set";
  }
}

export function gameLabel(game: Game | undefined): string {
  if (!game) return "Game";
  return game.emoji ? `${game.emoji} ${game.name}` : game.name;
}

/**
 * What a match's status is called on screen.
 *
 * Deliberately not where "contested" is expressed. Being objected to is a
 * property that sits alongside the status rather than replacing it — a
 * contested match is still awaiting answers, or still final — so the screens
 * show the mark next to this label instead of instead of it. Collapsing the two
 * would lose the fact that a contested result is a real, settled result.
 */
export function statusLabel(status: MatchStatus): string {
  switch (status) {
    case MatchStatus.IN_PROGRESS:
      return "In play";
    case MatchStatus.CLAIMED:
      return "Awaiting answers";
    case MatchStatus.FINAL:
      return "Final";
    case MatchStatus.CANCELED:
      return "Called off";
    default:
      return "";
  }
}

/** The team credited with the win, once there is one. */
export function winningTeamName(match: Match): string {
  const team = match.teams.find((t) => t.id === match.claimedByTeamId);
  return team?.name ?? "";
}

/** Players on the winning side, for the victory line. */
export function winners(match: Match): User[] {
  return match.participants
    .filter((p) => p.teamId === match.claimedByTeamId)
    .map((p) => p.user)
    .filter((u): u is User => u != null);
}

/**
 * A streak phrased as bragging: "W5" for five straight wins, "L3" for the other
 * kind. Empty when there is no streak worth mentioning.
 */
export function streakLabel(streak: number): string {
  if (streak >= 2) return `W${streak}`;
  if (streak <= -2) return `L${Math.abs(streak)}`;
  return "";
}

export function winRateText(wins: number, losses: number): string {
  const total = wins + losses;
  if (total === 0) return "—";
  return `${Math.round((wins / total) * 100)}%`;
}

/**
 * A one-line verdict on a head-to-head record. This is the sentence people
 * screenshot, so it is deliberately not neutral.
 */
export function rivalryVerdict(wins: number, losses: number, opponent: string): string {
  const total = wins + losses;
  if (total === 0) return `You have never played ${opponent}.`;
  if (wins === losses) return `Dead even with ${opponent}. Someone break the tie.`;
  if (losses === 0) return `Undefeated against ${opponent}.`;
  if (wins === 0) return `${opponent} has never lost to you.`;

  const margin = wins - losses;
  if (margin > 0) {
    return margin >= 5 ? `You own ${opponent}.` : `You are ahead of ${opponent}.`;
  }
  return margin <= -5 ? `${opponent} owns you.` : `${opponent} is ahead of you.`;
}

/**
 * How long it has been, counted in meetings rather than days.
 *
 * Deliberately not a date. "Six meetings and counting" is the sentence that
 * stings, and unlike a calendar it cannot be improved by avoiding somebody.
 */
export function droughtVerdict(
  neverBeaten: boolean,
  meetings: number,
  opponent: string,
  isMe: boolean,
): string {
  if (neverBeaten) {
    return isMe
      ? `You have never beaten ${opponent}.`
      : `Has never beaten ${opponent}.`;
  }
  const since = `${meetings} meeting${meetings === 1 ? "" : "s"}`;
  return isMe
    ? `${since} since you last beat ${opponent}.`
    : `${since} since they last beat ${opponent}.`;
}

/**
 * What it would take to draw level — the number nobody wants read aloud.
 * Empty when the record is level or ahead, because there is no debt to state.
 */
export function debtLine(debt: number, opponent: string, isMe: boolean): string {
  if (debt <= 0) return "";
  const wins = `${debt} straight win${debt === 1 ? "" : "s"}`;
  return isMe
    ? `${wins} to draw level with ${opponent}.`
    : `${wins} behind ${opponent}.`;
}

/** A rivalry close enough that the next game decides something. */
export function unfinishedLine(wins: number, losses: number, opponent: string): string {
  if (wins === losses) return `Dead even with ${opponent} at ${wins}–${losses}.`;
  return `${wins}–${losses} with ${opponent}. One game swings it.`;
}

/**
 * "2 games" or "1 win and 2 losses", for the line that explains why a record is
 * smaller than the games somebody remembers playing.
 *
 * Contested results are held out of every tally, so without a sentence like
 * this the numbers silently disagree with the match list and there is nothing
 * on screen to explain it. Spelled out win-and-loss when both sides have one,
 * because "3 games contested" invites the reading that they were all losses.
 */
export function contestedLine(wins: number, losses: number): string {
  const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;
  if (wins > 0 && losses > 0) {
    return `${plural(wins, "win", "wins")} and ${plural(losses, "loss", "losses")}`;
  }
  if (wins > 0) return plural(wins, "win", "wins");
  return plural(losses, "loss", "losses");
}
