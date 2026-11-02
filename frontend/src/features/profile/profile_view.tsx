import { useEffect, useState } from "react";
import { useRunnerStatus, useSignalValue } from "@tcn/state/react";
import { Status } from "@tcn/state/core";
import { useCollection } from "../../lib/domain/use_collection.js";

import type { AppPresenter } from "../../app/app_presenter.js";
import type { ProfilePresenter } from "./profile_presenter.js";
import type { GetPlayerProfileResponse } from "../../gen/scoreboard/v1/stats_pb.js";
import { Alert, AsyncBoundary, Avatar, Empty, Record, Sheet } from "../../components/ui.js";
import {
  contestedLine,
  debtLine,
  displayName,
  droughtVerdict,
  gameLabel,
  streakLabel,
  unfinishedLine,
  winRateText,
} from "../../lib/format.js";

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

  useEffect(() => {
    void presenter.load();
  }, [presenter]);

  const who = profile?.user ? displayName(profile.user) : "";
  useEffect(() => {
    if (who) app.setScreenTitle(who);
  }, [app, who]);

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
                {/* Games somebody is objecting to. Held out of every number on
                    this screen, and said out loud rather than silently dropped
                    — a record that quietly disagrees with the games you
                    remember playing is worse than one with an asterisk. */}
                {profile.contestedWins + profile.contestedLosses > 0 && (
                  <div style={{ marginTop: 6, fontSize: "0.82rem", color: "var(--ink-faint)" }}>
                    ⚑ plus {contestedLine(profile.contestedWins, profile.contestedLosses)}{" "}
                    contested, counting for nothing
                  </div>
                )}
                {profile.user?.placeholder && (
                  <div style={{ marginTop: 6, fontSize: "0.82rem", color: "var(--ink-faint)" }}>
                    Not on the app — added by another player so their games could be recorded
                  </div>
                )}
              </div>
            </div>
          </div>

          <LinkPlaceholder presenter={presenter} profile={profile} />

          <div className="tiles">
            <div className="tile">
              <div className="tile__label">Win rate</div>
              <div className="tile__value num">{winRateText(profile.wins, profile.losses)}</div>
            </div>
            <div className="tile">
              <div className="tile__label">Best run</div>
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
                  {profile.nemesisDebt > 0 && (
                    <div className="tile__note">
                      {debtLine(profile.nemesisDebt, displayName(profile.nemesis), presenter.isMe)}
                    </div>
                  )}
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

          {profile.drought?.opponent && (
            <button
              className="tile"
              style={{ textAlign: "left", cursor: "pointer" }}
              onClick={() =>
                profile.drought?.opponent && app.openRivalry(profile.drought.opponent.id)
              }
            >
              <div className="tile__label">🌵 The drought</div>
              <div className="row" style={{ marginTop: 6 }}>
                <Avatar user={profile.drought.opponent} size="sm" />
                <div style={{ minWidth: 0 }}>
                  <div className="tile__value" style={{ fontSize: "1.2rem" }}>
                    {displayName(profile.drought.opponent)}
                  </div>
                  <div className="tile__note">
                    {droughtVerdict(
                      profile.drought.neverBeaten,
                      profile.drought.meetingsSinceLastWin,
                      displayName(profile.drought.opponent),
                      presenter.isMe,
                    )}
                  </div>
                </div>
              </div>
            </button>
          )}

          {profile.unfinishedBusiness?.opponent && (
            <button
              className="tile"
              style={{ textAlign: "left", cursor: "pointer" }}
              onClick={() =>
                profile.unfinishedBusiness?.opponent &&
                app.openRivalry(profile.unfinishedBusiness.opponent.id)
              }
            >
              <div className="tile__label">⚖️ Unfinished business</div>
              <div className="row" style={{ marginTop: 6 }}>
                <Avatar user={profile.unfinishedBusiness.opponent} size="sm" />
                <div style={{ minWidth: 0 }}>
                  <div className="tile__value" style={{ fontSize: "1.2rem" }}>
                    {displayName(profile.unfinishedBusiness.opponent)}
                  </div>
                  <div className="tile__note">
                    {unfinishedLine(
                      profile.unfinishedBusiness.wins,
                      profile.unfinishedBusiness.losses,
                      displayName(profile.unfinishedBusiness.opponent),
                    )}
                  </div>
                </div>
              </div>
            </button>
          )}

          {profile.games.length > 0 && (
            <section className="stack">
              <span className="section-label">Game by game</span>
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

          {/* Only people they have actually played with, most games first. A list of
              everybody on the board says nothing about who they play with, and
              sends you to head-to-heads that are empty. */}
          {profile.playedWith.length > 0 && (
            <section className="stack">
              <span className="section-label">Played with</span>
              <div className="card card--flush">
                {profile.playedWith.map((c) => (
                  <button
                    key={c.user?.id}
                    className="rowbtn"
                    onClick={() =>
                      c.user &&
                      app.navigate({
                        name: "head_to_head",
                        userId: presenter.userId,
                        opponentId: c.user.id,
                      })
                    }
                  >
                    <Avatar user={c.user} size="sm" />
                    <span className="rowbtn__main">
                      <span className="rowbtn__name">{displayName(c.user)}</span>
                    </span>
                    <span className="num" style={{ color: "var(--ink-faint)" }}>
                      {c.games} {c.games === 1 ? "game" : "games"} ›
                    </span>
                  </button>
                ))}
              </div>
            </section>
          )}

          {profile.wins + profile.losses === 0 && (
            <Empty emoji="🫥">
              {profile.contestedWins + profile.contestedLosses > 0 ? (
                <>
                  <strong>Nothing that counts, yet.</strong>
                  <br />
                  Every game so far is contested. They start counting when whoever objected takes
                  it back.
                </>
              ) : (
                <>
                  <strong>No settled games yet.</strong>
                  <br />
                  The record starts when somebody calls a win.
                </>
              )}
            </Empty>
          )}
        </div>
      )}
    </AsyncBoundary>
  );
}

/**
 * Handing a placeholder's history to the account of the person it turns out to
 * be. Administrators only, and the server says so — `viewerMayLink` is computed
 * there and enforced there; this only decides whether to draw anything.
 *
 * A placeholder's profile is where this belongs because it is where somebody
 * actually looks at one: a whole record sitting under a name that cannot log in
 * to see it. The screen is already asking "who is this?", so it may as well
 * offer the answer.
 *
 * Deliberately wordy on the confirmation. There is no undo, and the failure mode
 * is not a broken button — it is one person quietly inheriting another person's
 * wins, which nothing downstream would flag as wrong.
 */
function LinkPlaceholder({
  presenter,
  profile,
}: {
  presenter: ProfilePresenter;
  profile: GetPlayerProfileResponse;
}) {
  const open = useSignalValue(presenter.linkingBroadcast);
  const error = useSignalValue(presenter.errorBroadcast);
  const moved = useSignalValue(presenter.linkedBroadcast);
  const status = useRunnerStatus(presenter.linkBroadcast);
  const members = useCollection(presenter.members);
  const busy = status === Status.PENDING;
  const [chosen, setChosen] = useState<string>("");

  // Already done, and the store has been dropped, so this screen is now showing
  // somebody else's profile under the old route. Say what happened rather than
  // leave the link control sitting there offering to do it again.
  if (moved != null) {
    return (
      <div className="card">
        <p style={{ margin: 0, color: "var(--ink-dim)", fontSize: "0.9rem" }}>
          ✓ Linked. {moved} {moved === 1 ? "game" : "games"} moved across, and the placeholder is
          gone.
        </p>
      </div>
    );
  }

  if (!profile.viewerMayLink) return null;

  const candidates = presenter.candidates(members);
  const target = candidates.find((m) => m.user?.id === chosen);

  return (
    <>
      <div className="card stack">
        <span className="section-label">This player has no account</span>
        <p style={{ margin: 0, color: "var(--ink-dim)", fontSize: "0.9rem" }}>
          If they have since signed up, join the two together. Everything here — every game, every
          win, every rivalry — becomes theirs, and they can object to any of it they disagree with.
        </p>
        <button className="btn btn--block" onClick={presenter.openLink}>
          This is somebody who signed up
        </button>
      </div>

      {open && (
        <Sheet title="Link player" onDismiss={presenter.cancelLink}>
          <h2>Who is {displayName(profile.user)}?</h2>
          <p style={{ color: "var(--ink-dim)", fontSize: "0.9rem" }}>
            Pick the account that turns out to be them.
          </p>
          {error && <Alert message={error} />}
          <div className="stack" style={{ gap: 6 }}>
            {candidates.length === 0 && (
              <p style={{ margin: 0, color: "var(--ink-dim)", fontSize: "0.9rem" }}>
                Nobody in this family has signed up yet.
              </p>
            )}
            {candidates.map((m) => {
              const id = m.user?.id ?? "";
              return (
                <button
                  key={id}
                  className="rowbtn"
                  aria-pressed={chosen === id}
                  style={{
                    border: "1px solid",
                    borderColor: chosen === id ? "var(--gold)" : "var(--line)",
                    borderRadius: "var(--radius)",
                  }}
                  onClick={() => setChosen(id)}
                >
                  <Avatar user={m.user} size="sm" />
                  <span className="rowbtn__main">
                    <span className="rowbtn__name">{displayName(m.user)}</span>
                  </span>
                  {chosen === id && <span style={{ color: "var(--gold)" }}>✓</span>}
                </button>
              );
            })}
          </div>

          {target && (
            <p style={{ fontSize: "0.82rem", color: "var(--ink-faint)" }}>
              Every game {displayName(profile.user)} played becomes{" "}
              {displayName(target.user)}&rsquo;s, and {displayName(profile.user)} stops existing.
              This cannot be undone. If they are both in the same game, sort that game out first —
              it will refuse rather than enter one person twice.
            </p>
          )}

          <div className="stack">
            <button
              className="btn btn--primary btn--block"
              disabled={busy || !target}
              onClick={() => target?.user && void presenter.link(target.user.id)}
            >
              {target ? `Yes — this is ${displayName(target.user)}` : "Pick an account"}
            </button>
            <button className="btn btn--ghost btn--block" onClick={presenter.cancelLink}>
              Never mind
            </button>
          </div>
        </Sheet>
      )}
    </>
  );
}
