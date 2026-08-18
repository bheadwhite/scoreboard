import { useEffect, useState } from "react";
import { useRunnerStatus, useSignalValue } from "@tcn/state/react";
import { Status } from "@tcn/state/core";

import type { AppPresenter } from "../../app/app_presenter.js";
import type { Answer, MatchPresenter } from "./match_presenter.js";
import type { Match, MatchParticipant } from "../../gen/scoreboard/v1/common_pb.js";
import { MatchStatus } from "../../gen/scoreboard/v1/common_pb.js";
import { Alert, AsyncBoundary, Avatar, Sheet } from "../../components/ui.js";
import {
  contractText,
  displayName,
  gameLabel,
  timeAgo,
  timeLeft,
  winners,
  winningTeamName,
} from "../../lib/format.js";

/**
 * One match, from posted to permanent record. The screen changes shape at each
 * stage rather than showing every control at once, because at any given moment
 * there is really only one thing to do.
 *
 * Three stages, not five. There is no lobby — a match is in play from the
 * moment it is posted, and joining is the whole of the commitment — and there
 * is no disputed screen, because an objection marks the result rather than
 * moving it somewhere else. A contested match is an ordinary claimed or settled
 * one wearing a flag.
 */
export function MatchView({
  presenter,
  app,
}: {
  presenter: MatchPresenter;
  app: AppPresenter;
}) {
  const match = useSignalValue(presenter.matchBroadcast);
  const error = useSignalValue(presenter.errorBroadcast);
  const gone = useSignalValue(presenter.goneBroadcast);
  const actionStatus = useRunnerStatus(presenter.actionBroadcast);
  const busy = actionStatus === Status.PENDING;

  useEffect(() => {
    presenter.start();
    return () => presenter.stop();
  }, [presenter]);

  // "Match" tells you nothing you did not already know. Say what is being
  // played.
  const gameName = match?.game ? gameLabel(match.game) : "";
  useEffect(() => {
    if (gameName) app.setScreenTitle(gameName);
  }, [app, gameName]);

  // Voided out from under whoever is looking at it. Says so rather than
  // failing, because from here it is indistinguishable from a broken link.
  if (gone) {
    return (
      <div className="card">
        <div className="empty">
          <span className="empty__emoji">🫥</span>
          This game was voided. It is gone from the record entirely — no win, no
          loss, for anybody.
        </div>
        <button className="btn btn--block" onClick={app.goHome}>
          Back to the scoreboard
        </button>
      </div>
    );
  }

  if (!match) {
    return (
      <AsyncBoundary runner={presenter.matchBroadcast} hideChildrenWhilePending>
        <div />
      </AsyncBoundary>
    );
  }

  return (
    <div className="stack stack--lg">
      {error && <Alert message={error} />}

      {match.status === MatchStatus.IN_PROGRESS && match.viewerIsInvited && (
        <InvitationStage presenter={presenter} match={match} busy={busy} />
      )}

      {match.status === MatchStatus.IN_PROGRESS && !match.viewerIsInvited && (
        <InPlayStage presenter={presenter} match={match} busy={busy} />
      )}

      {match.status === MatchStatus.CLAIMED && (
        <ClaimedStage presenter={presenter} match={match} busy={busy} />
      )}

      {match.status === MatchStatus.FINAL && (
        <FinalStage presenter={presenter} match={match} busy={busy} />
      )}

      {match.status === MatchStatus.CANCELED && (
        <div className="card">
          <div className="empty">
            <span className="empty__emoji">🚫</span>
            This match was called off. Nothing was recorded.
          </div>
        </div>
      )}

      {/* In play, the roster is the screen's main control and carries this
          itself. After a claim it is reference material. */}
      {match.status !== MatchStatus.IN_PROGRESS && (
        <TeamList presenter={presenter} match={match} />
      )}

      {presenter.cancelAuthority(match) === "organizer" && (
        <AbandonMatch presenter={presenter} match={match} busy={busy} />
      )}

      {match.viewerMayVoid && (
        <VoidMatch presenter={presenter} match={match} app={app} busy={busy} />
      )}
    </div>
  );
}

/**
 * Calling off a game that is not yours — the organizer's override. Always behind
 * a confirmation, because it is a game other people are in: possibly one they
 * actually sat down and played, which is exactly the thing the record is for.
 * This is the escape hatch for a game that fizzled out, not a way to undo a
 * result somebody dislikes. There is no way to do that, on purpose.
 */
function AbandonMatch({
  presenter,
  match,
  busy,
}: {
  presenter: MatchPresenter;
  match: Match;
  busy: boolean;
}) {
  const [confirming, setConfirming] = useState(false);
  const claimed = match.status === MatchStatus.CLAIMED;

  return (
    <>
      <button
        className="btn btn--ghost btn--sm"
        style={{ alignSelf: "center", color: "var(--ink-faint)" }}
        disabled={busy}
        onClick={() => setConfirming(true)}
      >
        Call this game off
      </button>

      {confirming && (
        <Sheet title="Call it off" onDismiss={() => setConfirming(false)}>
          <h2>Call this game off?</h2>
          <p style={{ color: "var(--ink-dim)" }}>
            {claimed ? (
              <>
                Somebody has already called this one. {gameLabel(match.game)} disappears and
                nothing is recorded — no win, no loss, for anybody. Use this for a game that
                fizzled out, not for a result somebody disagrees with: disagreeing with it is what
                objecting is for, and it does not need you.
              </>
            ) : (
              <>
                You did not set this one up. {gameLabel(match.game)} disappears for everyone in
                it, and they are not told why. If you simply do not want to play, leave instead —
                the rest of them can carry on.
              </>
            )}
          </p>
          <div className="stack">
            <button
              className="btn btn--concede btn--block"
              disabled={busy}
              onClick={() => {
                void presenter.cancel();
                setConfirming(false);
              }}
            >
              Yes, call it off
            </button>
            <button className="btn btn--ghost btn--block" onClick={() => setConfirming(false)}>
              Keep playing
            </button>
          </div>
        </Sheet>
      )}
    </>
  );
}

/**
 * Erasing a match — the administrator's override, and the only thing in the app
 * that takes something back out of the permanent record.
 *
 * Deliberately the plainest control on the screen, and deliberately separate
 * from calling a game off. Those are different acts with different results:
 * calling a game off leaves a cancelled match everybody can still see and read,
 * while this leaves nothing at all. The copy has to say which one you are about
 * to do, because after the tap there is nothing left to check against.
 */
function VoidMatch({
  presenter,
  match,
  app,
  busy,
}: {
  presenter: MatchPresenter;
  match: Match;
  app: AppPresenter;
  busy: boolean;
}) {
  const [confirming, setConfirming] = useState(false);
  const [reason, setReason] = useState("");
  const inRecord = match.status === MatchStatus.FINAL;

  return (
    <>
      <button
        className="btn btn--ghost btn--sm"
        style={{ alignSelf: "center", color: "var(--ink-faint)" }}
        disabled={busy}
        onClick={() => setConfirming(true)}
      >
        Void this game
      </button>

      {confirming && (
        <Sheet title="Void" onDismiss={() => setConfirming(false)}>
          <h2>Void this game?</h2>
          <p style={{ color: "var(--ink-dim)" }}>
            {gameLabel(match.game)} is deleted outright — the result, both sides, every
            concession. {inRecord ? (
              <>
                It comes straight off the scoreboard, and out of the head-to-head record
                between everyone who played it, as though it never happened.
              </>
            ) : (
              <>It disappears for everyone in it, and nothing is recorded.</>
            )}{" "}
            This cannot be undone and nobody is told.
          </p>
          <p style={{ color: "var(--ink-dim)" }}>
            For a game that should never have existed — not for a result somebody
            disagrees with. That is what disputing is for.
          </p>
          <div className="stack">
            <textarea
              className="textarea"
              placeholder="Why? (goes in the server log)"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
            <button
              className="btn btn--concede btn--block"
              disabled={busy}
              onClick={() => {
                void presenter.voidMatch(reason.trim()).then((done) => {
                  setConfirming(false);
                  if (done) app.goHome();
                });
              }}
            >
              Void it — erase this game
            </button>
            <button className="btn btn--ghost btn--block" onClick={() => setConfirming(false)}>
              Leave it alone
            </button>
          </div>
        </Sheet>
      )}
    </>
  );
}

// ── Lobby ────────────────────────────────────────────────────────────────────

// ── In play ──────────────────────────────────────────────────────────────────

function InPlayStage({
  presenter,
  match,
  busy,
}: {
  presenter: MatchPresenter;
  match: Match;
  busy: boolean;
}) {
  const [picking, setPicking] = useState(false);
  const myTeam = presenter.myTeamId(match);
  const teams = presenter.teamsWithPlayers(match);
  const inMatch = presenter.amIn(match);
  const canCallIt = presenter.canClaimForOthers(match);
  // Nobody to beat yet. The match is live either way — there is no lobby — but
  // there is no result to claim until somebody else is in it.
  const alone = match.joined.length < 2;

  return (
    <>
      <div className="card">
        <span className="section-label">{alone ? "Waiting for players" : "In play"}</span>
        <h2 style={{ marginTop: 4 }}>{contractText(match.contract)}</h2>
        <p style={{ margin: "6px 0 0", color: "var(--ink-dim)", fontSize: "0.88rem" }}>
          {alone
            ? "Posted " +
              timeAgo(match.startedAt) +
              ". Anyone who joins is playing under these terms — they were set when the game was posted and they do not change."
            : "Started " +
              timeAgo(match.startedAt) +
              ". When somebody wins, call it here — it does not go on the board until everyone else has answered."}
        </p>
      </div>

      <Roster presenter={presenter} match={match} busy={busy} inMatch={inMatch} />

      {inMatch ? (
        <div className="bottombar">
          <div className="stack" style={{ gap: 8 }}>
            <button
              className="btn btn--primary btn--block btn--claim"
              disabled={busy || alone || !myTeam}
              onClick={() => myTeam && void presenter.claimVictory(myTeam)}
            >
              {alone
                ? "Waiting for someone to join"
                : presenter.amSolo(match)
                  ? "🏆 I won"
                  : "🏆 We won"}
            </button>
            {/* The short circuit. Only the host gets it, and only once there is
                somebody to name — the common case is a game that just ended
                where the winner is not the one holding the phone. */}
            {canCallIt && !alone && (
              <button
                className="btn btn--block btn--sm"
                disabled={busy}
                onClick={() => setPicking(true)}
              >
                Somebody else won
              </button>
            )}
          </div>
        </div>
      ) : (
        <div className="bottombar">
          <button
            className="btn btn--primary btn--block btn--claim"
            disabled={busy}
            onClick={() => void presenter.join()}
          >
            Join this game
          </button>
        </div>
      )}

      {picking && (
        <Sheet title="Who won?" onDismiss={() => setPicking(false)}>
          <h2>Who won?</h2>
          <p style={{ color: "var(--ink-dim)", fontSize: "0.9rem" }}>
            Everyone else still gets asked, including you. Anybody who thinks this is wrong can say
            so, and the result carries their objection wherever it goes.
          </p>
          <div className="stack">
            {teams.map((team) => (
              <button
                key={team.id}
                className="btn btn--block"
                disabled={busy}
                onClick={() => {
                  void presenter.claimVictory(team.id);
                  setPicking(false);
                }}
              >
                {team.name}
              </button>
            ))}
            <button className="btn btn--ghost btn--block" onClick={() => setPicking(false)}>
              Never mind
            </button>
          </div>
        </Sheet>
      )}
    </>
  );
}

// ── Claimed: the ceremony ────────────────────────────────────────────────────

/**
 * The emotional center of the app. Somebody has named a winner and everyone
 * else is being asked.
 *
 * One screen, and no separate disputed version of it. Objecting used to move
 * the match to its own status with its own screen and its own referee; it now
 * marks the result and leaves everything else exactly where it was, so this is
 * the same claim with a flag on it. Nothing here is settled until the match
 * finalizes, and every answer on it — including the claim — can still be
 * changed by whoever gave it.
 */
function ClaimedStage({
  presenter,
  match,
  busy,
}: {
  presenter: MatchPresenter;
  match: Match;
  busy: boolean;
}) {
  const claimants = winners(match);
  const who = claimants.length === 1 ? displayName(claimants[0]) : winningTeamName(match);
  // Named by somebody who is not on the side they named — the host settling a
  // game on everyone's behalf. Worth saying out loud: it changes who is being
  // asked to agree with what.
  const caller = match.joined.find((p) => p.user?.id === match.claimedByUserId);
  const calledByOther =
    caller != null && caller.teamId !== match.claimedByTeamId ? displayName(caller.user) : "";

  return (
    <>
      <div className="claim">
        <div className="claim__eyebrow">{calledByOther ? `${calledByOther} says` : "Claims victory"}</div>
        <div className="claim__who">{who}</div>
        <div className="claim__says">
          {calledByOther ? "won " : ""}at {gameLabel(match.game)} — {contractText(match.contract)}
        </div>

        {/* The clock runs whatever anybody says. An objection is a mark on the
            result, not a hold on it, so there is always a deadline to report
            and it is always honest to report it. */}
        {match.awaitingConcession.length > 0 ? (
          <div className="claim__deadline">
            Waiting on {match.awaitingConcession.length}{" "}
            {match.awaitingConcession.length === 1 ? "person" : "people"} · goes on the board in{" "}
            {timeLeft(match.concedeDeadline)} if nobody answers
          </div>
        ) : (
          <div className="claim__deadline">Settling…</div>
        )}
      </div>

      <ContestedNotice match={match} presenter={presenter} />
      <AnswerBoard presenter={presenter} match={match} />
      <YourAnswer presenter={presenter} match={match} who={who} busy={busy} />
    </>
  );
}

/**
 * The flag itself: who is objecting to this result and why.
 *
 * Shown to everybody, named, with the reasons. That is a reversal — objections
 * used to be sent only to the family organizer, on the grounds that naming the
 * objector turned a paused game into a grudge. Nothing is paused now and nobody
 * is ruling on it, so the objection is the whole outcome: an asterisk on
 * somebody's win that nobody can attribute or argue back against is worse than
 * the grudge it was avoiding. If you are going to say a result did not happen,
 * you say it under your own name.
 */
function ContestedNotice({
  match,
  presenter,
}: {
  match: Match;
  presenter: MatchPresenter;
}) {
  const objections = presenter.objections(match);
  if (objections.length === 0) return null;

  return (
    <div className="card stack" style={{ borderColor: "var(--dispute)" }}>
      <span className="section-label">
        {objections.length === 1 ? "Somebody says that is not what happened" : "Contested"}
      </span>
      {objections.map((p) => (
        <div key={p.user?.id} className="team" style={{ borderColor: "var(--dispute)" }}>
          <div className="row" style={{ gap: 8 }}>
            <Avatar user={p.user} size="sm" />
            <span style={{ fontWeight: 700 }}>{displayName(p.user)}</span>
            <span className="spacer" />
            <span style={{ fontSize: "0.78rem", color: "var(--ink-faint)" }}>
              {timeAgo(p.disputedAt)}
            </span>
          </div>
          <p style={{ margin: "8px 0 0", color: "var(--ink-dim)", fontSize: "0.92rem" }}>
            {p.disputeReason?.trim() ? `“${p.disputeReason.trim()}”` : "No explanation given."}
          </p>
        </div>
      ))}
      <p style={{ margin: 0, fontSize: "0.8rem", color: "var(--ink-faint)" }}>
        This still counts as a result and it still shows here — it just stays off the scoreboard
        while the objection stands. Nobody settles this but the people in it.
      </p>
    </div>
  );
}

const ANSWER_BADGE: Record<Answer, { label: string; className: string }> = {
  claimant: { label: "🏆 named the winner", className: "badge badge--hot" },
  conceded: { label: "agrees", className: "badge" },
  disputed: { label: "objects", className: "badge badge--wait" },
  silent: { label: "no answer yet", className: "badge badge--wait" },
  // Not a kind of silence. Nobody is waiting on them and nobody ever will be.
  absent: { label: "not on the app", className: "badge" },
};

/**
 * Where everybody stands, right now. This is the running tally the match is
 * waiting on, and it is the thing that makes an unanswered claim feel like an
 * open question rather than a countdown.
 *
 * Nothing on it is hidden. Objections used to be redacted to everyone but the
 * family organizer, so this board had to hedge about whether a quiet player was
 * quiet or merely censored. Nobody adjudicates an objection now, so nothing is
 * withheld and the board is simply true.
 */
function AnswerBoard({ presenter, match }: { presenter: MatchPresenter; match: Match }) {
  const answers = presenter.answers(match);

  return (
    <div className="card stack">
      <span className="section-label">Where everyone stands</span>
      <div className="stack" style={{ gap: 6 }}>
        {answers.map(({ user, state }) => (
          <div key={user?.id} className="team__player" style={{ minHeight: 40 }}>
            <Avatar user={user} size="sm" />
            <span>{displayName(user)}</span>
            <span className="spacer" />
            <span className={ANSWER_BADGE[state].className}>{ANSWER_BADGE[state].label}</span>
          </div>
        ))}
      </div>
      {/* Says why the board will never fill in, so nobody sits waiting on a
          row that is not going to change. */}
      {presenter.hasAbsentPlayers(match) && (
        <p style={{ margin: 0, fontSize: "0.8rem", color: "var(--ink-faint)" }}>
          Players who are not on the app cannot answer, so nobody is waiting on them. When they
          sign up and get linked to an account, they can object to this result then.
        </p>
      )}
    </div>
  );
}

/**
 * The viewer's own answer, and the ways to change it.
 *
 * Deliberately always on screen while the match is open, whichever answer they
 * have given. Hiding the controls once somebody answered made a concession look
 * irreversible and left a claimant with no way to admit they had jumped the gun.
 */
function YourAnswer({
  presenter,
  match,
  who,
  busy,
}: {
  presenter: MatchPresenter;
  match: Match;
  who: string;
  busy: boolean;
}) {
  const confirming = useSignalValue(presenter.confirmingConcedeBroadcast);
  const disputing = useSignalValue(presenter.disputingBroadcast);
  const [withdrawing, setWithdrawing] = useState(false);
  const [reason, setReason] = useState("");

  const answer = presenter.myAnswer(match);
  const settles = presenter.answerWouldSettleIt(match);
  const done = match.status === MatchStatus.FINAL;

  // Not in the match at all — a family member reading someone else's game.
  if (!presenter.amIn(match)) return null;

  if (match.viewerIsClaimant) {
    return (
      <>
        <div className="bottombar">
          <button
            className="btn btn--block btn--sm"
            style={{ color: "var(--ink-faint)" }}
            disabled={busy}
            onClick={() => setWithdrawing(true)}
          >
            Take this claim back
          </button>
        </div>

        {withdrawing && (
          <Sheet title="Take it back" onDismiss={() => setWithdrawing(false)}>
            <h2>Take back the claim?</h2>
            <p style={{ color: "var(--ink-dim)" }}>
              The game goes back to being in play with nobody claiming it, and every answer given
              so far is cleared. This is “never mind”, not “they won” — if the other side actually
              won, they have to claim it themselves.
            </p>
            <div className="stack">
              <button
                className="btn btn--block"
                disabled={busy}
                onClick={() => {
                  void presenter.withdrawClaim();
                  setWithdrawing(false);
                }}
              >
                Yes, take it back
              </button>
              <button
                className="btn btn--ghost btn--block"
                onClick={() => setWithdrawing(false)}
              >
                Never mind
              </button>
            </div>
          </Sheet>
        )}
      </>
    );
  }

  return (
    <>
      <div className="bottombar">
        <div className="stack" style={{ gap: 8 }}>
          {done && match.viewerIsClaimant ? (
            answer === "disputed" ? (
              <>
                <button className="btn btn--block" disabled>
                  ⚑ You say you did not win this
                </button>
                <button
                  className="btn btn--concede btn--block btn--sm"
                  disabled={busy}
                  onClick={presenter.askToConcede}
                >
                  Actually, I did win it
                </button>
              </>
            ) : (
              <button
                className="btn btn--dispute btn--block btn--sm"
                disabled={busy}
                onClick={presenter.openDispute}
              >
                I did not win this
              </button>
            )
          ) : answer === "conceded" ? (
            <>
              <button className="btn btn--block" disabled>
                ✓ You conceded
              </button>
              <button
                className="btn btn--dispute btn--block btn--sm"
                disabled={busy}
                onClick={presenter.openDispute}
              >
                Actually, that is not what happened
              </button>
            </>
          ) : answer === "disputed" ? (
            <>
              <button className="btn btn--block" disabled>
                ⚖️ You are disputing this
              </button>
              <button
                className="btn btn--concede btn--block btn--sm"
                disabled={busy}
                onClick={presenter.askToConcede}
              >
                Actually, I concede
              </button>
            </>
          ) : (
            <>
              <button
                className="btn btn--concede btn--block"
                disabled={busy}
                onClick={presenter.askToConcede}
              >
                I concede
              </button>
              <button
                className="btn btn--dispute btn--block btn--sm"
                disabled={busy}
                onClick={presenter.openDispute}
              >
                That is not what happened
              </button>
            </>
          )}
        </div>
      </div>

      {confirming && match.viewerIsClaimant ? (
        <Sheet title="Take it back" onDismiss={presenter.cancelConcede}>
          <h2>Withdraw your objection?</h2>
          <p style={{ color: "var(--ink-dim)" }}>
            The result goes back to counting for you, on the scoreboard and everywhere else. You
            can object again later if you change your mind.
          </p>
          <div className="stack">
            <button
              className="btn btn--concede btn--block"
              disabled={busy}
              onClick={() => void presenter.concede()}
            >
              Yes, I won it
            </button>
            <button className="btn btn--ghost btn--block" onClick={presenter.cancelConcede}>
              Never mind
            </button>
          </div>
        </Sheet>
      ) : confirming ? (
        <Sheet title="Concede?" onDismiss={presenter.cancelConcede}>
          <h2>Concede to {who}?</h2>
          {/* Finalizing is the one step in this whole protocol that cannot be
              undone, so whether this tap is that step decides what to warn
              about. Every other concession can be taken back. */}
          <p style={{ color: "var(--ink-dim)" }}>
            {settles
              ? "You are the last one they are waiting on, so this puts it on the permanent record straight away. That part cannot be undone — though you can still object to it afterward."
              : "You can change your mind at any point, including after it lands. Once everybody has answered, the result goes on the permanent record for good."}
          </p>
          <div className="stack">
            <button
              className="btn btn--concede btn--block"
              disabled={busy}
              onClick={() => void presenter.concede()}
            >
              Yes. They won.
            </button>
            <button className="btn btn--ghost btn--block" onClick={presenter.cancelConcede}>
              Not yet
            </button>
          </div>
        </Sheet>
      ) : null}

      {disputing && (
        <Sheet title="Object" onDismiss={presenter.cancelDispute}>
          <h2>{match.viewerIsClaimant ? "Say you did not win this" : "Say that is not what happened"}</h2>
          <p style={{ color: "var(--ink-dim)" }}>
            This does not stop the game finishing and it does not undo the result. It marks it: the
            match stays on everyone's list with your name and your reason on it, and it counts for
            nobody — no win, no loss, no streak — for as long as you leave it there. Nobody rules on
            it. You can take it back whenever you like.
          </p>
          <div className="stack">
            <textarea
              className="textarea"
              placeholder="What actually happened?"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
            <button
              className="btn btn--dispute btn--block"
              disabled={busy}
              onClick={() => void presenter.dispute(reason)}
            >
              Object to this result
            </button>
            <button className="btn btn--ghost btn--block" onClick={presenter.cancelDispute}>
              Never mind
            </button>
          </div>
        </Sheet>
      )}
    </>
  );
}

// ── Final ────────────────────────────────────────────────────────────────────

function FinalStage({
  presenter,
  match,
  busy,
}: {
  presenter: MatchPresenter;
  match: Match;
  busy: boolean;
}) {
  const players = winners(match);
  const who = players.length === 1 ? displayName(players[0]) : winningTeamName(match);

  return (
    <>
      <div className="card">
        <div className="crown">
          <span className="crown__emoji" aria-hidden="true">
            {match.contested ? "⚑" : "🏆"}
          </span>
          <div className="crown__who">{who}</div>
          <p style={{ margin: "8px 0 0", color: "var(--ink-dim)" }}>
            won at {gameLabel(match.game)} · {contractText(match.contract)}
          </p>
          <p style={{ margin: "4px 0 0", color: "var(--ink-faint)", fontSize: "0.85rem" }}>
            {match.contested
              ? `Recorded ${timeAgo(match.finalizedAt)} · contested, so it is off the scoreboard`
              : `On the record ${timeAgo(match.finalizedAt)}`}
          </p>
        </div>
      </div>

      <ContestedNotice match={match} presenter={presenter} />
      <AnswerBoard presenter={presenter} match={match} />

      {/* Still answerable, on purpose. Seeing a result in the standings is when
          most people first notice one they disagree with, and by then it has
          already settled — under the old rules that was too late and the only
          remedy was an administrator erasing the match. */}
      {presenter.canAnswer(match) && (
        <YourAnswer presenter={presenter} match={match} who={who} busy={busy} />
      )}
    </>
  );
}

// ── Shared ───────────────────────────────────────────────────────────────────

function TeamList({ presenter, match }: { presenter: MatchPresenter; match: Match }) {
  const teams = presenter.teamsWithPlayers(match);
  const settled = match.status === MatchStatus.FINAL;

  return (
    <div className="stack">
      <span className="section-label">
        {match.joined.length} player{match.joined.length === 1 ? "" : "s"}
        {match.invited.length > 0 && ` · ${match.invited.length} still deciding`}
      </span>
      <div className="stack" style={{ gap: 8 }}>
        {teams.map((team) => {
          const claimed = team.id === match.claimedByTeamId;
          const lost = settled && !claimed;
          return (
            <div
              key={team.id}
              className={`team${claimed ? " team--claimed" : ""}${lost ? " team--lost" : ""}`}
            >
              <div className="row row--between">
                <span className="team__name">{team.name}</span>
                {claimed && <span className="badge badge--hot">🏆 {settled ? "Won" : "Claims"}</span>}
              </div>
              <div className="team__players">
                {team.players.map((p) => (
                  <div
                    key={p.user?.id}
                    className={`team__player${p.concededAt ? " team__player--conceded" : ""}`}
                  >
                    <Avatar user={p.user} size="sm" />
                    <span>{displayName(p.user)}</span>
                    <span className="spacer" />
                    {/* disputedAt is only sent to the organizer (and to the
                        person who objected), so this simply has nothing to
                        render for everyone else. */}
                    {p.disputedAt ? (
                      <span className="badge badge--wait">disputed</span>
                    ) : p.concededAt ? (
                      <span className="badge">conceded</span>
                    ) : null}
                  </div>
                ))}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function nameOf(match: Match, userId: string): string {
  const p = match.joined.find((x) => x.user?.id === userId);
  return displayName(p?.user);
}

function teamNameFor(match: Match, userIds: string[]): string {
  return userIds.map((id) => nameOf(match, id)).join(" & ");
}

function teamNameOf(match: Match, participant: MatchParticipant): string {
  const team = match.teams.find((t) => t.id === participant.teamId);
  const size = match.joined.filter((p) => p.teamId === participant.teamId).length;
  return size > 1 ? (team?.name ?? "") : "solo";
}

/**
 * What an invited player sees. Being named by whoever set the match up is a
 * question, not an enrolment — so this is the whole screen until it is
 * answered, and declining is as easy as accepting.
 *
 * The terms are on it because tapping in is the entire commitment: there is no
 * second screen where you confirm you have read them, and no way for anybody to
 * change them afterward.
 */
function InvitationStage({
  presenter,
  match,
  busy,
}: {
  presenter: MatchPresenter;
  match: Match;
  busy: boolean;
}) {
  const host = match.joined.find((p) => p.user?.id === match.createdBy)?.user;

  return (
    <>
      <div className="claim">
        <div className="claim__eyebrow">You are invited</div>
        <div className="claim__who">{gameLabel(match.game)}</div>
        <div className="claim__says">
          {displayName(host)} wants you in — {contractText(match.contract)}
        </div>
        {match.joined.length > 0 && (
          <div className="waiting">
            {match.joined.map((p) => (
              <span key={p.user?.id} className="waiting__name">
                {displayName(p.user)}
              </span>
            ))}
          </div>
        )}
      </div>

      <div className="bottombar">
        <div className="stack" style={{ gap: 8 }}>
          <button
            className="btn btn--primary btn--block btn--claim"
            disabled={busy}
            onClick={() => void presenter.join()}
          >
            I'm playing
          </button>
          <button
            className="btn btn--ghost btn--block btn--sm"
            disabled={busy}
            onClick={() => void presenter.decline()}
          >
            No thanks
          </button>
        </div>
      </div>
    </>
  );
}

/**
 * The roster, which in play is the screen's main control rather than a list.
 *
 * It used to be three cards showing the same people — a team builder, a ready
 * board and a player list. The ready board is gone with the ready check, and
 * one row per player carries the rest: who they are, and which side they are
 * on.
 */
function Roster({
  presenter,
  match,
  busy,
  inMatch,
}: {
  presenter: MatchPresenter;
  match: Match;
  busy: boolean;
  inMatch: boolean;
}) {
  const [selected, setSelected] = useState<string[]>([]);

  // Sides are the creator's to arrange, and only worth arranging once there are
  // enough people to pair up. Somebody who set the match up and then left is
  // reading it, not running it.
  const canPair = match.viewerIsCreator && inMatch && match.joined.length > 2;

  const toggle = (userId: string) => {
    if (!canPair) return;
    setSelected((prev) =>
      prev.includes(userId) ? prev.filter((id) => id !== userId) : [...prev, userId],
    );
  };

  const pairUp = () => {
    if (selected.length < 2) return;
    const rest = match.joined
      .map((p) => p.user?.id)
      .filter((id): id is string => id != null && !selected.includes(id));
    void presenter.setTeams([
      { name: teamNameFor(match, selected), userIds: selected },
      ...rest.map((id) => ({ name: nameOf(match, id), userIds: [id] })),
    ]);
    setSelected([]);
  };

  const splitAll = () => {
    void presenter.setTeams(
      match.joined
        .map((p) => p.user?.id)
        .filter((id): id is string => id != null)
        .map((id) => ({ name: nameOf(match, id), userIds: [id] })),
    );
    setSelected([]);
  };

  return (
    <div className="card stack">
      <div className="row row--between">
        <span className="section-label">
          {match.joined.length} player{match.joined.length === 1 ? "" : "s"}
        </span>
        {inMatch && (
          <button
            className="btn btn--ghost btn--sm"
            style={{ color: "var(--ink-faint)" }}
            disabled={busy}
            onClick={() => void presenter.leave()}
          >
            Leave
          </button>
        )}
      </div>

      {match.joined.length === 0 && (
        <p style={{ margin: 0, fontSize: "0.9rem", color: "var(--ink-dim)" }}>
          Nobody has joined yet. Be the first.
        </p>
      )}

      <div className="stack" style={{ gap: 6 }}>
        {match.joined.map((p) => {
          const id = p.user?.id ?? "";
          const isSelected = selected.includes(id);
          const partners = match.joined.filter((x) => x.teamId === p.teamId).length;

          return (
            <button
              key={id}
              className="rowbtn"
              style={{
                border: "1px solid",
                borderColor: isSelected ? "var(--gold)" : "var(--line)",
                borderRadius: "var(--radius)",
                cursor: canPair ? "pointer" : "default",
                minHeight: 54,
              }}
              aria-pressed={canPair ? isSelected : undefined}
              disabled={!canPair}
              onClick={() => toggle(id)}
            >
              <Avatar user={p.user} size="sm" />
              <span className="rowbtn__main">
                <span className="rowbtn__name">{displayName(p.user)}</span>
                {partners > 1 && <span className="rowbtn__meta">with {teamNameOf(match, p)}</span>}
                {p.user?.placeholder && (
                  <span className="rowbtn__meta">not on the app · will not be asked</span>
                )}
              </span>
            </button>
          );
        })}
      </div>

      {canPair && (
        <>
          <p style={{ margin: 0, fontSize: "0.8rem", color: "var(--ink-faint)" }}>
            Tap two or more players to make them partners. Sides decide who gets asked when
            somebody calls the win, so they are fixed once that happens.
          </p>
          <div className="row">
            {selected.length >= 2 ? (
              <button className="btn btn--sm btn--primary" onClick={pairUp} disabled={busy}>
                Make these {selected.length} partners
              </button>
            ) : (
              <button className="btn btn--sm btn--ghost" onClick={splitAll} disabled={busy}>
                Every player for themselves
              </button>
            )}
          </div>
        </>
      )}

      {match.invited.length > 0 && (
        <p style={{ margin: 0, fontSize: "0.8rem", color: "var(--ink-faint)" }}>
          Still deciding: {match.invited.map((p) => displayName(p.user)).join(", ")}. They can join
          any time before somebody calls it.
        </p>
      )}
    </div>
  );
}
