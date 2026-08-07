import { useEffect } from "react";
import { useSignalValue } from "@tcn/state/react";
import { useCollection } from "../../lib/domain/use_collection.js";

import type { AppPresenter } from "../../app/app_presenter.js";
import type { ScoreboardPresenter } from "./scoreboard_presenter.js";
import type { Match } from "../../gen/scoreboard/v1/common_pb.js";
import { MatchStatus } from "../../gen/scoreboard/v1/common_pb.js";
import { StatsRange } from "../../gen/scoreboard/v1/stats_pb.js";
import { AsyncBoundary, Avatar, Empty, Record } from "../../components/ui.js";
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
  { value: StatsRange.YEAR, label: "This year" },
  { value: StatsRange.MONTH, label: "This month" },
];

/**
 * The family's trophy case, and the landing place. Anything demanding a
 * response — a claim waiting on you — is pulled to the very top, above the
 * standings.
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
  const standings = useCollection(presenter.standings);
  const live = useCollection(presenter.liveMatches);
  const recent = useCollection(presenter.recentMatches);
  const games = useCollection(presenter.games);

  useEffect(() => {
    void presenter.load();
  }, [presenter]);

  const needsYou = live.filter((m) => m.viewerOwesConcession);
  const otherLive = live.filter((m) => !m.viewerOwesConcession);

  return (
    <div className="stack stack--lg">
      {needsYou.length > 0 && (
        <section className="stack">
          {needsYou.map((match) => (
            <ClaimCallout key={match.id} match={match} onOpen={() => app.openMatch(match.id)} />
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
        <div className="chips" role="group" aria-label="Filter by game">
          <button
            className="chip"
            aria-pressed={gameId === ""}
            onClick={() => presenter.setGame("")}
          >
            All games
          </button>
          {games.map((game) => (
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

        <AsyncBoundary runner={presenter.standings.broadcast}>
          <div className="card card--flush">
            {!standings || standings.rows.length === 0 ? (
              <Empty emoji="🎲">
                <strong>No finalized games yet.</strong>
                <br />
                Play something. The record starts when someone concedes.
              </Empty>
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

        {standings && standings.totalMatches > 0 && (
          <p style={{ margin: 0, textAlign: "center", color: "var(--ink-faint)", fontSize: "0.82rem" }}>
            <span className="num">{standings.totalMatches}</span> game
            {standings.totalMatches === 1 ? "" : "s"} on the record
          </p>
        )}
      </section>

      {recent.length > 0 && (
        <section className="stack">
          <span className="section-label">Recently settled</span>
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

function winnerLine(match: Match): string {
  const players = winners(match);
  if (players.length === 0) return winningTeamName(match) || "Someone";
  if (players.length === 1) return displayName(players[0]);
  if (players.length === 2) return `${displayName(players[0])} & ${displayName(players[1])}`;
  return winningTeamName(match) || `${displayName(players[0])} + ${players.length - 1}`;
}

/**
 * A claim that is waiting on *you*. This is the single most important thing the
 * app ever shows, so it interrupts the scoreboard rather than sitting in a list.
 */
function ClaimCallout({ match, onOpen }: { match: Match; onOpen: () => void }) {
  const players = winners(match);
  const who = players.length === 1 ? displayName(players[0]) : winningTeamName(match);

  return (
    <div className="claim">
      <div className="claim__eyebrow">Awaiting your concession</div>
      <div className="claim__who">{who}</div>
      <div className="claim__says">
        claims the win at {gameLabel(match.game)} — {contractText(match.contract)}
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

function LiveRow({ match, onOpen }: { match: Match; onOpen: () => void }) {
  const claimed = match.status === MatchStatus.CLAIMED;
  const disputed = match.status === MatchStatus.DISPUTED;

  return (
    <button className="rowbtn" onClick={onOpen}>
      <span className="rowbtn__main">
        <span className="rowbtn__name">{gameLabel(match.game)}</span>
        <span className="rowbtn__meta">
          {match.participants.length} playing · {contractText(match.contract)}
        </span>
      </span>
      {disputed ? (
        <span className="badge badge--wait">⚖️ Disputed</span>
      ) : claimed ? (
        <span className="badge badge--wait">⏳ Claimed</span>
      ) : (
        <span className="badge badge--live">
          <span className="dot" /> Live
        </span>
      )}
    </button>
  );
}
