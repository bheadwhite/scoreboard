import { useEffect } from "react";
import { useCollection } from "../../lib/domain/use_collection.js";

import type { AppPresenter } from "../../app/app_presenter.js";
import type { ProfilePresenter } from "./profile_presenter.js";
import { AsyncBoundary, Avatar, Empty, Record } from "../../components/ui.js";
import { displayName, gameLabel, streakLabel, winRateText } from "../../lib/format.js";

/**
 * One player's record inside a family. The nemesis and favorite-victim tiles
 * are the point of this screen — a win count is a statistic, but "Dad has beaten
 * you 9 times" is a grudge.
 */
export function ProfileView({
  presenter,
  app,
}: {
  presenter: ProfilePresenter;
  app: AppPresenter;
}) {
  const profile = useCollection(presenter.profile);
  const members = useCollection(presenter.members);

  useEffect(() => {
    void presenter.load();
  }, [presenter]);

  const others = members.filter((m) => m.user?.id !== presenter.userId);

  return (
    <AsyncBoundary runner={presenter.profile.broadcast} hideChildrenWhilePending={!profile}>
      {profile && (
        <div className="stack stack--lg">
          <div className="card">
            <div className="row" style={{ gap: 14 }}>
              <Avatar user={profile.user} size="lg" />
              <div style={{ minWidth: 0 }}>
                <h1 style={{ fontSize: "1.5rem" }}>{displayName(profile.user)}</h1>
                <div className="row" style={{ gap: 8, marginTop: 4 }}>
                  <Record wins={profile.wins} losses={profile.losses} />
                  {streakLabel(profile.currentStreak) && (
                    <span
                      className={`badge ${profile.currentStreak > 0 ? "badge--hot" : "badge--cold"}`}
                    >
                      {profile.currentStreak > 0 ? "🔥" : "🧊"}{" "}
                      {streakLabel(profile.currentStreak)}
                    </span>
                  )}
                </div>
              </div>
            </div>
          </div>

          <div className="tiles">
            <div className="tile">
              <div className="tile__label">Win rate</div>
              <div className="tile__value num">{winRateText(profile.wins, profile.losses)}</div>
            </div>
            <div className="tile">
              <div className="tile__label">Best streak</div>
              <div className="tile__value num">{profile.longestWinStreak || "—"}</div>
            </div>
          </div>

          {profile.nemesis && (
            <button
              className="tile"
              style={{ textAlign: "left", cursor: "pointer" }}
              onClick={() => profile.nemesis && app.openRivalry(profile.nemesis.id)}
            >
              <div className="tile__label">😤 Nemesis</div>
              <div className="row" style={{ marginTop: 6 }}>
                <Avatar user={profile.nemesis} size="sm" />
                <div style={{ minWidth: 0 }}>
                  <div className="tile__value" style={{ fontSize: "1.2rem" }}>
                    {displayName(profile.nemesis)}
                  </div>
                  <div className="tile__note num">
                    has beaten {presenter.isMe ? "you" : "them"} {profile.nemesisLosses} time
                    {profile.nemesisLosses === 1 ? "" : "s"}
                    {profile.nemesisWins > 0 && ` (${profile.nemesisWins} the other way)`}
                  </div>
                </div>
              </div>
            </button>
          )}

          {profile.favoriteVictim && (
            <button
              className="tile"
              style={{ textAlign: "left", cursor: "pointer" }}
              onClick={() => profile.favoriteVictim && app.openRivalry(profile.favoriteVictim.id)}
            >
              <div className="tile__label">🎯 Favorite victim</div>
              <div className="row" style={{ marginTop: 6 }}>
                <Avatar user={profile.favoriteVictim} size="sm" />
                <div style={{ minWidth: 0 }}>
                  <div className="tile__value" style={{ fontSize: "1.2rem" }}>
                    {displayName(profile.favoriteVictim)}
                  </div>
                  <div className="tile__note num">
                    beaten {profile.victimWins} time{profile.victimWins === 1 ? "" : "s"}
                    {profile.victimLosses > 0 && ` (${profile.victimLosses} the other way)`}
                  </div>
                </div>
              </div>
            </button>
          )}

          {profile.games.length > 0 && (
            <section className="stack">
              <span className="section-label">By game</span>
              <div className="card card--flush">
                {profile.games.map((g) => (
                  <div key={g.game?.id} className="rowbtn" style={{ cursor: "default" }}>
                    <span className="rowbtn__main">
                      <span className="rowbtn__name">{gameLabel(g.game)}</span>
                      <span className="rowbtn__meta num">{winRateText(g.wins, g.losses)}</span>
                    </span>
                    <Record wins={g.wins} losses={g.losses} />
                  </div>
                ))}
              </div>
            </section>
          )}

          <section className="stack">
            <span className="section-label">
              {presenter.isMe ? "Your rivalries" : "Head to head"}
            </span>
            <div className="card card--flush">
              {others.length === 0 ? (
                <Empty emoji="👥">Nobody else in this family yet.</Empty>
              ) : (
                others.map((m) => (
                  <button
                    key={m.user?.id}
                    className="rowbtn"
                    onClick={() =>
                      m.user &&
                      app.navigate({
                        name: "head_to_head",
                        userId: presenter.userId,
                        opponentId: m.user.id,
                      })
                    }
                  >
                    <Avatar user={m.user} size="sm" />
                    <span className="rowbtn__main">
                      <span className="rowbtn__name">{displayName(m.user)}</span>
                    </span>
                    <span style={{ color: "var(--ink-faint)" }}>›</span>
                  </button>
                ))
              )}
            </div>
          </section>

          {profile.wins + profile.losses === 0 && (
            <Empty emoji="🫥">
              <strong>No finalized games yet.</strong>
              <br />
              The record starts when someone concedes.
            </Empty>
          )}
        </div>
      )}
    </AsyncBoundary>
  );
}
