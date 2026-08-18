import { useEffect } from "react";
import { useSignalValue } from "@tcn/state/react";
import { useCollection } from "../../lib/domain/use_collection.js";

import type { AppPresenter } from "../../app/app_presenter.js";
import type { ScoreboardPresenter } from "./scoreboard_presenter.js";
import type { Match } from "../../gen/scoreboard/v1/common_pb.js";
import { ContractKind, MatchStatus } from "../../gen/scoreboard/v1/common_pb.js";
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
  // The filter offers games with a finished match behind them, not the catalog.
  const playedGames = standings?.playedGames ?? [];

  useEffect(() => {
    void presenter.load();
    presenter.start();
    return () => presenter.stop();
  }, [presenter]);

  const me = app.domain.session.user?.id;
  const mine = (m: Match) => m.joined.some((p) => p.user?.id === me);
  const unsettled = (m: Match) => m.status === MatchStatus.CLAIMED;

  const needsYou = live.filter((m) => m.viewerOwesConcession);
  const invitations = live.filter((m) => m.viewerIsInvited && !m.viewerOwesConcession);
  // Answering is not the same as being finished with it. A claim you have
  // already conceded or objected to stays in front of you, with the answer you
  // gave on it, until the match actually settles — you can still change it, and
  // a screen that quietly dropped the match the moment you tapped made that
  // look impossible.
  const settling = live.filter(
    (m) => !m.viewerOwesConcession && !m.viewerIsInvited && unsettled(m) && mine(m),
  );
  const otherLive = live.filter(
    (m) =>
      !m.viewerOwesConcession &&
      !m.viewerIsInvited &&
      !(unsettled(m) && mine(m)),
  );

  return (
    <div className="stack stack--lg">
      {needsYou.length > 0 && (
        <section className="stack">
          {needsYou.map((match) => (
            <ClaimCallout key={match.id} match={match} onOpen={() => app.openMatch(match.id)} />
          ))}
        </section>
      )}

      {invitations.length > 0 && (
        <section className="stack">
          {invitations.map((match) => (
            <InviteCallout key={match.id} match={match} onOpen={() => app.openMatch(match.id)} />
          ))}
        </section>
      )}

      {settling.length > 0 && (
        <section className="stack">
          <span className="section-label">Waiting on a concession</span>
          <div className="card card--flush">
            {settling.map((match) => (
              <LiveRow
                key={match.id}
                match={match}
                yourAnswer={answerOf(match, me)}
                onOpen={() => app.openMatch(match.id)}
              />
            ))}
          </div>
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

        {/* Nothing on the record yet means nothing to slice by time either. */}
        {(standings?.totalMatches ?? 0) > 0 && (
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
              <Empty emoji="🎲">
                <strong>Nothing on the record yet.</strong>
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

        {standings && standings.totalMatches + standings.contestedMatches > 0 && (
          <p style={{ margin: 0, textAlign: "center", color: "var(--ink-faint)", fontSize: "0.82rem" }}>
            <span className="num">{standings.totalMatches}</span> game
            {standings.totalMatches === 1 ? "" : "s"} on the record
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
 * The answer the signed-in player has standing on a claim, for the list. Mirrors
 * MatchPresenter.myAnswer — the list has no match presenter to ask, and one
 * `.find` is cheaper than constructing one per row.
 */
function answerOf(match: Match, me: string | undefined): string | null {
  if (match.viewerIsClaimant) return "you claimed this";
  const p = match.joined.find((x) => x.user?.id === me);
  if (p?.disputedAt) return "you disputed it";
  if (p?.concededAt) return "you conceded";
  return null;
}

function LiveRow({
  match,
  onOpen,
  yourAnswer,
}: {
  match: Match;
  onOpen: () => void;
  yourAnswer?: string | null;
}) {
  const contractSet = match.contract && match.contract.kind !== ContractKind.UNSPECIFIED;
  // Posted, but nobody has joined yet. Not a status — every match is in play
  // from the moment it exists — but it is the difference between a game running
  // and a game waiting for someone to show up, and the row is telling you
  // whether to go and do something about it.
  const waiting = match.status === MatchStatus.IN_PROGRESS && match.joined.length < 2;

  const badge = () => {
    if (match.contested) return <span className="badge badge--wait">⚑ Contested</span>;
    switch (match.status) {
      case MatchStatus.CLAIMED:
        return <span className="badge badge--wait">⏳ Claimed</span>;
      default:
        return waiting ? (
          <span className="badge">Waiting for players</span>
        ) : (
          <span className="badge badge--live">
            <span className="dot" /> Live
          </span>
        );
    }
  };

  const players = `${match.joined.length} ${match.joined.length === 1 ? "player" : "players"}`;
  const pending = match.invited.length > 0 ? ` · ${match.invited.length} invited` : "";

  return (
    <button className="rowbtn" onClick={onOpen}>
      <span className="rowbtn__main">
        <span className="rowbtn__name">{gameLabel(match.game)}</span>
        <span className="rowbtn__meta">
          {yourAnswer
            ? `${yourAnswer} · tap to change it`
            : waiting
              ? `${players}${pending}`
              : `${players} · ${contractSet ? contractText(match.contract) : "no contract"}`}
        </span>
      </span>
      {badge()}
    </button>
  );
}
