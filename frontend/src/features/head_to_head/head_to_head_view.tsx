import { useEffect } from "react";
import { useSignalValue } from "@tcn/state/react";
import { useCollection } from "../../lib/domain/use_collection.js";

import type { AppPresenter } from "../../app/app_presenter.js";
import type { HeadToHeadPresenter } from "./head_to_head_presenter.js";
import { AsyncBoundary, Avatar, Empty, Record } from "../../components/ui.js";
import {
  contestedLine,
  displayName,
  gameLabel,
  playerNames,
  debtLine,
  droughtVerdict,
  rivalryVerdict,
  streakLabel,
  timeAgo,
  winners,
  winningTeamName,
} from "../../lib/format.js";

/**
 * You versus one specific person. This is the money screen — the whole app
 * exists so this number can be looked at — so the number is the biggest thing
 * on it and everything else is supporting detail.
 */
export function HeadToHeadView({
  presenter,
  app,
}: {
  presenter: HeadToHeadPresenter;
  app: AppPresenter;
}) {
  const gameId = useSignalValue(presenter.gameIdBroadcast);
  const record = useCollection(presenter.record);
  const games = useCollection(presenter.games);

  useEffect(() => {
    void presenter.load();
  }, [presenter]);

  const versus =
    record?.user && record?.opponent
      ? `${displayName(record.user)} vs ${displayName(record.opponent)}`
      : "";
  useEffect(() => {
    if (versus) app.setScreenTitle(versus);
  }, [app, versus]);

  return (
    <AsyncBoundary runner={presenter.record.broadcast} hideChildrenWhilePending={!record}>
      {record && (
        <div className="stack stack--lg">
          <div className="card card--flush">
            <div className="tally">
              <div>
                <div className="tally__num tally__num--win num">{record.wins}</div>
                <div className="tally__who">{displayName(record.user)}</div>
              </div>
              <div className="tally__dash">–</div>
              <div>
                <div className="tally__num tally__num--loss num">{record.losses}</div>
                <div className="tally__who">{displayName(record.opponent)}</div>
              </div>
            </div>

            <div className="verdict">
              {rivalryVerdict(record.wins, record.losses, displayName(record.opponent))}
              {streakLabel(record.streak) && (
                <>
                  {" "}
                  <span className={`badge ${record.streak > 0 ? "badge--hot" : "badge--cold"}`}>
                    {record.streak > 0 ? "🔥" : "🧊"} {streakLabel(record.streak)}
                  </span>
                </>
              )}
            </div>

            {/* Meetings one of them is still objecting to. Said out loud
                rather than silently dropped: the alternative is a record that
                does not match the games people remember playing, with no
                explanation on the screen for why. */}
            {record.contestedWins + record.contestedLosses > 0 && (
              <div className="verdict__note">
                <div>
                  ⚑ {contestedLine(record.contestedWins, record.contestedLosses)} contested, and
                  not counted above.
                </div>
              </div>
            )}

            {/* The verdict says who is ahead. These two say how long it has
                been and what it would cost to fix — the part that decides
                whether somebody sets the board back up tonight. */}
            {record.rivalry && record.wins + record.losses > 0 && (
              <div className="verdict__note">
                {record.rivalry.meetingsSinceLastWin > 0 && (
                  <div>
                    {droughtVerdict(
                      record.rivalry.neverBeaten,
                      record.rivalry.meetingsSinceLastWin,
                      displayName(record.opponent),
                      true,
                    )}
                  </div>
                )}
                {record.rivalry.debt > 0 && (
                  <div>{debtLine(record.rivalry.debt, displayName(record.opponent), true)}</div>
                )}
              </div>
            )}
          </div>

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

          {record.togetherWins + record.togetherLosses > 0 && (
            <div className="tile">
              <div className="tile__label">As partners</div>
              <div className="tile__value num">
                {record.togetherWins}–{record.togetherLosses}
              </div>
              <div className="tile__note">
                Games you played on the same side. These do not count either way above.
              </div>
            </div>
          )}

          {record.games.length > 0 && (
            <section className="stack">
              <span className="section-label">Game by game</span>
              <div className="card card--flush">
                {record.games.map((g) => (
                  <div key={g.game?.id} className="rowbtn" style={{ cursor: "default" }}>
                    <span className="rowbtn__main">
                      <span className="rowbtn__name">{gameLabel(g.game)}</span>
                    </span>
                    <Record wins={g.wins} losses={g.losses} />
                  </div>
                ))}
              </div>
            </section>
          )}

          <section className="stack">
            <span className="section-label">Every meeting</span>
            <div className="card card--flush">
              {record.recentMatches.length === 0 ? (
                <Empty emoji="🤝">You two have never faced off. Somebody has to start it.</Empty>
              ) : (
                record.recentMatches.map((match) => {
                  const players = winners(match);
                  const wonByViewer = players.some((p) => p.id === record.user?.id);
                  // The two of them are the whole point of this screen.
                  const others = playerNames(match, [record.user?.id, record.opponent?.id]);
                  return (
                    <button
                      key={match.id}
                      className="rowbtn"
                      onClick={() => app.openMatch(match.id)}
                    >
                      <Avatar user={players[0]} size="sm" />
                      <span className="rowbtn__main">
                        <span className="rowbtn__name">
                          {players.length === 1
                            ? displayName(players[0])
                            : winningTeamName(match)}{" "}
                          <span style={{ color: "var(--ink-faint)" }}>won</span>
                        </span>
                        <span className="rowbtn__meta">
                          {gameLabel(match.game)} · {timeAgo(match.finalizedAt)}
                          {others && ` · with ${others}`}
                        </span>
                      </span>
                      <span
                        className={`badge ${wonByViewer ? "badge--live" : "badge--cold"}`}
                      >
                        {wonByViewer ? "W" : "L"}
                      </span>
                    </button>
                  );
                })
              )}
            </div>
          </section>
        </div>
      )}
    </AsyncBoundary>
  );
}
