import { useEffect, useState } from "react";
import { useSignalValue } from "@tcn/state/react";
import { useCollection } from "../../lib/domain/use_collection.js";

import type { AppPresenter } from "../../app/app_presenter.js";
import type { ScoreboardPresenter } from "./scoreboard_presenter.js";
import type { FamilyMember, Match } from "../../gen/scoreboard/v1/common_pb.js";
import { ContractKind, MatchStatus } from "../../gen/scoreboard/v1/common_pb.js";
import { StatsRange } from "../../gen/scoreboard/v1/stats_pb.js";
import { AsyncBoundary, Avatar, Empty, Record, Sheet } from "../../components/ui.js";
import {
  contractText,
  displayName,
  gameLabel,
  streakLabel,
  timeAgo,
  winRateText,
  winners,
  winningTeamName,
} from "../../lib/format.js";

const RANGES: { value: StatsRange; label: string }[] = [
  { value: StatsRange.ALL_TIME, label: "All time" },
  { value: StatsRange.DAY, label: "Last 24 hours" },
  { value: StatsRange.YEAR, label: "This year" },
  { value: StatsRange.MONTH, label: "This month" },
];

/**
 * The family's trophy case, and the landing place. Anything demanding a
 * response — a fresh result you have not looked at — is pulled to the very
 * top, above the standings.
 */
export function ScoreboardView({
  presenter,
  app,
}: {
  presenter: ScoreboardPresenter;
  app: AppPresenter;
}) {
  const gameId = useSignalValue(presenter.gameIdBroadcast);
  const range = useSignalValue(presenter.rangeBroadcast);
  const playerIds = useSignalValue(presenter.playerIdsBroadcast);
  const standings = useCollection(presenter.standings);
  const live = useCollection(presenter.liveMatches);
  const recent = useCollection(presenter.recentMatches);
  const members = useCollection(presenter.members);
  // The filter offers games with a finished match behind them, not the catalog.
  const playedGames = standings?.playedGames ?? [];

  useEffect(() => {
    void presenter.load();
    presenter.start();
    return () => presenter.stop();
  }, [presenter]);

  const me = app.domain.session.user?.id;
  const mine = (m: Match) => m.joined.some((p) => p.user?.id === me);

  const invitations = live.filter((m) => m.viewerIsInvited);
  const otherLive = live.filter((m) => !m.viewerIsInvited);

  // Results you are in that you have not said anything about, while they are
  // still fresh.
  //
  // This is what took the place of the claim queue, and it is doing a different
  // job. The old list was a set of debts — claims that could not settle until
  // you answered — and it was the only thing putting a result in front of the
  // people it was recorded against. Nothing waits on anybody now, so the
  // debts are gone, but the need to see the result is not: an objection is what
  // keeps the scoreboard honest, and nobody objects to a result they never
  // noticed. So this is a prompt rather than a queue.
  //
  // It decays on its own. Two days, then it drops off, because a list that
  // never clears stops being read — and the result stays objectable forever
  // from the match screen regardless, which is the guarantee that matters.
  const unanswered = recent.filter(
    (m) => mine(m) && !m.viewerIsClaimant && !answered(m, me) && isRecent(m),
  );

  return (
    <div className="stack stack--lg">
      {/* One result gets the whole hero treatment; several get a list, because
          five heroes in a row is a wall nobody reads. */}
      {unanswered.length === 1 && unanswered[0] && (
        <section className="stack">
          <ResultCallout
            match={unanswered[0]}
            onOpen={() => app.openMatch(unanswered[0]!.id)}
          />
        </section>
      )}

      {unanswered.length > 1 && (
        <section className="stack">
          <span className="section-label">New results — say if any of these is wrong</span>
          <div className="card card--flush">
            {unanswered.map((match) => (
              <ResultRow
                key={match.id}
                match={match}
                onOpen={() => app.openMatch(match.id)}
              />
            ))}
          </div>
        </section>
      )}

      {invitations.length > 0 && (
        <section className="stack">
          {invitations.map((match) => (
            <InviteCallout key={match.id} match={match} onOpen={() => app.openMatch(match.id)} />
          ))}
        </section>
      )}

      {otherLive.length > 0 && (
        <section className="stack">
          <span className="section-label">Happening now</span>
          <div className="card card--flush">
            {otherLive.map((match) => (
              <LiveRow key={match.id} match={match} onOpen={() => app.openMatch(match.id)} />
            ))}
          </div>
        </section>
      )}

      <section className="stack">
        {(standings?.totalMatches ?? 0) > 0 && (
          <span className="section-label">The pecking order</span>
        )}

        {/* Everybody picked has to have been in a game for it to count here, so
            the people on this board are all measured over the same games. With
            two members or fewer it would only ever show the same table. */}
        {members.length > 2 && (
          <PlayerSelect presenter={presenter} members={members} playerIds={playerIds} />
        )}

        {/* Only what this family has actually finished. Offering the whole
            catalog meant a dozen chips that every filtered to an empty table.
            With one game played there is nothing to choose between, so the row
            does not appear at all. */}
        {playedGames.length > 1 && (
          <div className="chips" role="group" aria-label="Filter by game">
            <button
              className="chip"
              aria-pressed={gameId === ""}
              onClick={() => presenter.setGame("")}
            >
              All games
            </button>
            {playedGames.map((game) => (
              <button
                key={game.id}
                className="chip"
                aria-pressed={gameId === game.id}
                onClick={() => presenter.setGame(game.id)}
              >
                {gameLabel(game)}
              </button>
            ))}
          </div>
        )}

        {/* Nothing on the record yet means nothing to slice by time either. A
            quiet range is different: the total is for that range, and hiding the
            chips would leave nowhere to go back to. */}
        {((standings?.totalMatches ?? 0) > 0 ||
          range !== StatsRange.ALL_TIME ||
          playerIds.length > 0) && (
        <div className="chips" role="group" aria-label="Time range">
          {RANGES.map((r) => (
            <button
              key={r.value}
              className="chip"
              aria-pressed={range === r.value}
              onClick={() => presenter.setRange(r.value)}
            >
              {r.label}
            </button>
          ))}
        </div>
        )}

        <AsyncBoundary runner={presenter.standings.broadcast}>
          <div className="card card--flush">
            {!standings || standings.rows.length === 0 ? (
              playerIds.length > 0 ? (
                <Empty emoji="🎲">
                  <strong>No finished games with everybody you picked.</strong>
                </Empty>
              ) : range === StatsRange.ALL_TIME ? (
                <Empty emoji="🎲">
                  <strong>Nothing on the record yet.</strong>
                  <br />
                  Play something. The record starts when somebody names a winner.
                </Empty>
              ) : (
                <Empty emoji="🎲">
                  <strong>Nothing that counts in this stretch.</strong>
                </Empty>
              )
            ) : (
              standings.rows.map((row) => (
                <button
                  key={row.user?.id}
                  className="rowbtn"
                  onClick={() => row.user && app.openPlayer(row.user.id)}
                >
                  <span className={`rank num${row.rank === 1 ? " rank--1" : ""}`}>
                    {row.rank === 1 ? "👑" : row.rank}
                  </span>
                  <Avatar user={row.user} size="sm" />
                  <span className="rowbtn__main">
                    <span className="rowbtn__name">{displayName(row.user)}</span>
                    <span className="rowbtn__meta num">
                      {winRateText(row.wins, row.losses)}
                      {streakLabel(row.streak) && (
                        <>
                          {" · "}
                          <span
                            className={`badge ${row.streak > 0 ? "badge--hot" : "badge--cold"}`}
                            style={{ padding: "1px 7px" }}
                          >
                            {row.streak > 0 ? "🔥" : "🧊"} {streakLabel(row.streak)}
                          </span>
                        </>
                      )}
                    </span>
                  </span>
                  <Record wins={row.wins} losses={row.losses} />
                </button>
              ))
            )}
          </div>
        </AsyncBoundary>

        {standings && standings.totalMatches + standings.contestedMatches > 0 && (
          <p style={{ margin: 0, textAlign: "center", color: "var(--ink-faint)", fontSize: "0.82rem" }}>
            <span className="num">{standings.totalMatches}</span> game
            {standings.totalMatches === 1 ? "" : "s"}{" "}
            {playerIds.length > 1 ? "together" : "on the record"}
            {/* Contested games are held out of every number above. Saying so
                here is the only thing standing between "the table is wrong" and
                "the table is right and two results are being argued about". */}
            {standings.contestedMatches > 0 && (
              <>
                {" · "}
                <span className="num">{standings.contestedMatches}</span> contested and not counted
              </>
            )}
          </p>
        )}
      </section>

      {/* A crown per game, not one overall. "Lisa owns Catan" is a claim
          somebody can go and do something about tonight; a single champion is
          just the top of the table restated. */}
      {standings && standings.gameKings.length > 0 && (
        <section className="stack">
          <span className="section-label">Who owns what</span>
          <div className="card card--flush">
            {standings.gameKings.map((king) => (
              <button
                key={king.game?.id}
                className="rowbtn"
                onClick={() => king.user && app.openPlayer(king.user.id)}
              >
                <span className="rank">👑</span>
                <Avatar user={king.user} size="sm" />
                <span className="rowbtn__main">
                  <span className="rowbtn__name">
                    {displayName(king.user)}{" "}
                    <span style={{ color: "var(--ink-faint)" }}>owns</span>{" "}
                    {gameLabel(king.game)}
                  </span>
                  <span className="rowbtn__meta num">
                    {winRateText(king.wins, king.losses)} at it
                  </span>
                </span>
                <Record wins={king.wins} losses={king.losses} />
              </button>
            ))}
          </div>
        </section>
      )}

      {recent.length > 0 && (
        <section className="stack">
          <span className="section-label">On the record</span>
          <div className="card card--flush">
            {recent.slice(0, 8).map((match) => (
              <button
                key={match.id}
                className="rowbtn"
                onClick={() => app.openMatch(match.id)}
              >
                <span className="rowbtn__main">
                  <span className="rowbtn__name">
                    {winnerLine(match)} <span style={{ color: "var(--ink-faint)" }}>won</span>
                  </span>
                  <span className="rowbtn__meta">
                    {gameLabel(match.game)} · {timeAgo(match.finalizedAt)}
                  </span>
                </span>
                <div className="avatar-stack">
                  {winners(match)
                    .slice(0, 3)
                    .map((u) => (
                      <Avatar key={u.id} user={u} size="sm" />
                    ))}
                </div>
              </button>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}

/** Who the board is narrowed to, in the fewest words that still say it. */
function playersLabel(members: FamilyMember[], playerIds: string[]): string {
  if (playerIds.length === 0) return "Everyone";
  const names = members
    .filter((m) => m.user && playerIds.includes(m.user.id))
    .map((m) => displayName(m.user).split(" ")[0]);
  // Somebody picked on this device may have since been linked to an account, and
  // their old id names nobody. Do not read as fewer people than are being asked for.
  if (names.length < playerIds.length) return `${playerIds.length} players`;
  return names.join(", ");
}

/**
 * One control rather than a chip per person, which ran off the side of the
 * screen. It applies as you tick, so the board behind the sheet is already right
 * by the time you close it.
 */
function PlayerSelect({
  presenter,
  members,
  playerIds,
}: {
  presenter: ScoreboardPresenter;
  members: FamilyMember[];
  playerIds: string[];
}) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <button
        className="select select--button"
        aria-haspopup="dialog"
        aria-label="Filter by players"
        onClick={() => setOpen(true)}
      >
        <span className="select__value">{playersLabel(members, playerIds)}</span>
        <span aria-hidden="true">▾</span>
      </button>

      {open && (
        <Sheet title="Whose games?" onDismiss={() => setOpen(false)}>
          <h2>Whose games?</h2>
          <p style={{ color: "var(--ink-dim)", fontSize: "0.9rem" }}>
            Only games everybody you pick played in count, so all of them are measured over the same
            games.
          </p>
          <div className="stack">
            <div className="card card--flush sheet__list">
              {members.map((member) => (
                <label key={member.user?.id} className="rowbtn">
                  <Avatar user={member.user} size="sm" />
                  <span className="rowbtn__main">
                    <span className="rowbtn__name">{displayName(member.user)}</span>
                  </span>
                  <input
                    type="checkbox"
                    className="check"
                    checked={playerIds.includes(member.user?.id ?? "")}
                    onChange={() => member.user && presenter.togglePlayer(member.user.id)}
                  />
                </label>
              ))}
            </div>
            <button className="btn btn--primary btn--block" onClick={() => setOpen(false)}>
              Done
            </button>
            <button
              className="btn btn--ghost btn--block"
              disabled={playerIds.length === 0}
              onClick={presenter.clearPlayers}
            >
              Back to everyone
            </button>
          </div>
        </Sheet>
      )}
    </>
  );
}

function winnerLine(match: Match): string {
  const players = winners(match);
  if (players.length === 0) return winningTeamName(match) || "Someone";
  if (players.length === 1) return displayName(players[0]);
  if (players.length === 2) return `${displayName(players[0])} & ${displayName(players[1])}`;
  return winningTeamName(match) || `${displayName(players[0])} + ${players.length - 1}`;
}

/**
 * A result you are in and have not said anything about. This is the single most
 * important thing the app shows, so it interrupts the scoreboard rather than
 * sitting in a list.
 *
 * It used to be a claim that could not settle without you, and the urgency was
 * borrowed from that: answer or the game hangs. Nothing hangs now — the result
 * is already on the board — so the urgency has to come from what is actually
 * true, which is that a wrong result stays on the scoreboard until somebody who
 * was there says otherwise. That is a better reason to look, not a weaker one.
 */
function ResultCallout({ match, onOpen }: { match: Match; onOpen: () => void }) {
  const players = winners(match);
  const who = players.length === 1 ? displayName(players[0]) : winningTeamName(match);

  return (
    <div className="claim">
      <div className="claim__eyebrow">On the board</div>
      <div className="claim__who">{who}</div>
      <div className="claim__says">
        won at {gameLabel(match.game)} — {contractText(match.contract)}
      </div>
      <button
        className="btn btn--primary btn--block btn--claim"
        style={{ marginTop: 16 }}
        onClick={onOpen}
      >
        {match.viewerObjected ? "You are objecting to this" : "Is that what happened?"}
      </button>
    </div>
  );
}

/** The same thing in a row, for when several landed at once. */
function ResultRow({ match, onOpen }: { match: Match; onOpen: () => void }) {
  return (
    <button className="rowbtn" onClick={onOpen}>
      <span className="rowbtn__main">
        <span className="rowbtn__name">
          {winnerLine(match)} <span style={{ color: "var(--ink-faint)" }}>won</span>
        </span>
        <span className="rowbtn__meta">
          {gameLabel(match.game)} · {timeAgo(match.finalizedAt)}
        </span>
      </span>
      {match.contested ? (
        <span className="badge badge--wait">⚑ Contested</span>
      ) : (
        <span className="badge">say if it is wrong</span>
      )}
    </button>
  );
}

/** An unanswered invitation, surfaced where it will actually be seen. */
function InviteCallout({ match, onOpen }: { match: Match; onOpen: () => void }) {
  return (
    <div className="claim">
      <div className="claim__eyebrow">You are invited</div>
      <div className="claim__who">{gameLabel(match.game)}</div>
      <div className="claim__says">
        {match.joined.length} {match.joined.length === 1 ? "player is" : "players are"} in
      </div>
      <button
        className="btn btn--primary btn--block btn--claim"
        style={{ marginTop: 16 }}
        onClick={onOpen}
      >
        Respond
      </button>
    </div>
  );
}

/**
 * Whether the signed-in player has taken any position on this result. Mirrors
 * MatchPresenter.myAnswer — the list has no match presenter to ask, and one
 * `.find` is cheaper than constructing one per row.
 */
function answered(match: Match, me: string | undefined): boolean {
  const p = match.joined.find((x) => x.user?.id === me);
  return p?.disputedAt != null || p?.concededAt != null;
}

/** Within the couple of days a new result is still worth interrupting for. */
const PROMPT_WINDOW_MS = 48 * 60 * 60 * 1000;

function isRecent(match: Match): boolean {
  const at = match.finalizedAt;
  if (!at) return false;
  return Date.now() - Number(at.seconds) * 1000 < PROMPT_WINDOW_MS;
}

function LiveRow({ match, onOpen }: { match: Match; onOpen: () => void }) {
  const contractSet = match.contract && match.contract.kind !== ContractKind.UNSPECIFIED;
  // Posted, but nobody has joined yet. Not a status — every match is in play
  // from the moment it exists — but it is the difference between a game running
  // and a game waiting for someone to show up, and the row is telling you
  // whether to go and do something about it.
  const waiting = match.status === MatchStatus.IN_PROGRESS && match.joined.length < 2;

  const badge = () =>
    waiting ? (
      <span className="badge">Waiting for players</span>
    ) : (
      <span className="badge badge--live">
        <span className="dot" /> Live
      </span>
    );

  const players = `${match.joined.length} ${match.joined.length === 1 ? "player" : "players"}`;
  const pending = match.invited.length > 0 ? ` · ${match.invited.length} invited` : "";

  return (
    <button className="rowbtn" onClick={onOpen}>
      <span className="rowbtn__main">
        <span className="rowbtn__name">{gameLabel(match.game)}</span>
        <span className="rowbtn__meta">
          {waiting
            ? `${players}${pending}`
            : `${players} · ${contractSet ? contractText(match.contract) : "no contract"}`}
        </span>
      </span>
      {badge()}
    </button>
  );
}
