import { useEffect, useState } from "react";
import { useRunnerStatus, useSignalValue } from "@tcn/state/react";
import { useCollection } from "../../lib/domain/use_collection.js";
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
  winners,
  winningTeamName,
} from "../../lib/format.js";

/**
 * One match, from posted to permanent record. The screen changes shape at each
 * stage rather than showing every control at once, because at any given moment
 * there is really only one thing to do.
 *
 * Two stages, not five. There is no lobby — a match is in play from the moment
 * it is posted, and joining is the whole of the commitment. There is no
 * disputed screen, because an objection marks the result rather than moving it
 * somewhere else. And there is no claimed screen, which was the largest of the
 * three to lose: it showed a countdown and a running tally of who had answered,
 * and neither could change where the match was going to end up. A game is being
 * played, or it has a result and people have positions on it.
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
          itself. Once there is a result it is reference material. */}
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
            You did not set this one up. {gameLabel(match.game)} disappears for everyone in it,
            and they are not told why. If you simply do not want to play, leave instead — the
            rest of them can carry on.
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
  const myTeam = presenter.myTeamId(match);
  // A side with nobody left on it cannot have won anything.
  const sides = presenter.teamsWithPlayers(match).filter((team) => team.players.length > 0);
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
              ". When somebody wins, call it here. It goes on the board straight away, and anybody in the game can object to it afterward — at any point, forever."}
        </p>
      </div>

      <Roster presenter={presenter} match={match} busy={busy} inMatch={inMatch} />

      {canCallIt && !alone ? (
        // The host is usually the one holding the phone when a game ends, and
        // the winner is as likely to be anybody else as them. So every side is
        // one tap away, and the host's own is not given any special standing.
        <div className="bottombar">
          <div className="stack" style={{ gap: 8 }}>
            <span className="section-label">Who won? Tap to put it on the board</span>
            <div className="winner-grid">
              {sides.map((team) => (
                <button
                  key={team.id}
                  className="btn btn--primary btn--block"
                  disabled={busy}
                  onClick={() => void presenter.claimVictory(team.id)}
                >
                  {team.id === myTeam ? (presenter.amSolo(match) ? "Me" : "Us") : team.name}
                </button>
              ))}
            </div>
          </div>
        </div>
      ) : inMatch ? (
        <div className="bottombar">
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
    </>
  );
}

// ── The result ───────────────────────────────────────────────────────────────

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
 *
 * This carries more weight than it used to. With nothing pending in front of a
 * result, an objection is the only thing standing between the scoreboard and
 * whatever somebody felt like entering — so it has to be visible enough that a
 * result nobody agrees with is obvious at a glance.
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
  claimant: { label: "🏆 won it", className: "badge badge--hot" },
  conceded: { label: "agrees", className: "badge" },
  disputed: { label: "objects", className: "badge badge--wait" },
  // Not "no answer yet" any more — nothing is pending, so nobody is late. They
  // have not said anything about this result, which is its own fact.
  silent: { label: "has not said", className: "badge badge--wait" },
  // Not a kind of silence. There is nobody there to say anything.
  absent: { label: "not on the app", className: "badge" },
};

/**
 * Where everybody stands on the result, right now.
 *
 * It used to be a tally the match was waiting on. Nothing waits, so what it is
 * now is the honest state of opinion about a result that already counts —
 * which is more useful, not less: a win four people agree with and a win nobody
 * has looked at are very different things, and the board is the only place that
 * difference shows.
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
          Players who are not on the app have no way to answer, so their row will never change.
          When they sign up and get linked to an account, the history becomes theirs and they can
          object to any of it.
        </p>
      )}
    </div>
  );
}

/**
 * The viewer's own position on the result, and the ways to change it.
 *
 * Always on screen, whichever position they hold. Hiding the controls once
 * somebody answered made a concession look irreversible and left a winner with
 * no way to admit they had jumped the gun — and now that a result lands the
 * instant somebody names it, jumping the gun is the likeliest mistake there is.
 *
 * Nothing here is a step in a protocol any more. The result already counts;
 * these are positions people hold about it, and every one of them can be
 * changed in either direction for as long as the record exists. Which makes
 * this screen, rather than any countdown, the thing that keeps the scoreboard
 * honest — so the objection control is never more than one tap away, for
 * everybody in the match including whoever the win is credited to.
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
  // The side the result credits. They object in different words to everybody
  // else — "I did not win this" is a denial of their own win, which is the most
  // damning thing anybody in the match can say — and they alone can take the
  // result off the board outright.
  const iWon = match.viewerIsClaimant;

  // Not in the match at all — a family member reading someone else's game.
  if (!presenter.amIn(match)) return null;

  const objectLabel = iWon ? "I did not win this" : "That is not what happened";
  const concedeLabel = iWon ? "Actually, I did win it" : "I concede";

  return (
    <>
      <div className="bottombar">
        <div className="stack" style={{ gap: 8 }}>
          {answer === "disputed" ? (
            <>
              <button className="btn btn--block" disabled>
                {iWon ? "⚑ You say you did not win this" : "⚖️ You are objecting to this"}
              </button>
              <button
                className="btn btn--concede btn--block btn--sm"
                disabled={busy}
                onClick={presenter.askToConcede}
              >
                {concedeLabel}
              </button>
            </>
          ) : answer === "conceded" ? (
            <>
              <button className="btn btn--block" disabled>
                {iWon ? "🏆 You won this" : "✓ You agree with this"}
              </button>
              <button
                className="btn btn--dispute btn--block btn--sm"
                disabled={busy}
                onClick={presenter.openDispute}
              >
                {iWon ? objectLabel : "Actually, that is not what happened"}
              </button>
            </>
          ) : (
            <>
              {!iWon && (
                <button
                  className="btn btn--concede btn--block"
                  disabled={busy}
                  onClick={presenter.askToConcede}
                >
                  {concedeLabel}
                </button>
              )}
              <button
                className="btn btn--dispute btn--block btn--sm"
                disabled={busy}
                onClick={presenter.openDispute}
              >
                {objectLabel}
              </button>
            </>
          )}

          {/* Refusing the win. Only the side it credits gets this, which is what
              keeps "a win can be refused, a loss cannot be erased" true. */}
          {presenter.canWithdraw(match) && (
            <button
              className="btn btn--block btn--sm"
              style={{ color: "var(--ink-faint)" }}
              disabled={busy}
              onClick={() => setWithdrawing(true)}
            >
              Take this result back
            </button>
          )}
        </div>
      </div>

      {withdrawing && (
        <Sheet title="Take it back" onDismiss={() => setWithdrawing(false)}>
          <h2>Take this result back?</h2>
          <p style={{ color: "var(--ink-dim)" }}>
            The win comes off the board — off the standings, off the head-to-head record, out of
            everybody's streaks — and {gameLabel(match.game)} goes back to being in play with
            nobody credited. Every answer on it is cleared.
          </p>
          <p style={{ color: "var(--ink-dim)" }}>
            This is “never mind”, not “they won”. If the other side actually won, they have to
            call it themselves. Only you can do this, because it is your win to refuse — nobody
            can take a loss back off the board.
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
            <button className="btn btn--ghost btn--block" onClick={() => setWithdrawing(false)}>
              Never mind
            </button>
          </div>
        </Sheet>
      )}

      {confirming && iWon ? (
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
          {/* Nothing irreversible left to warn about — this used to be the tap
              that could put a result on the permanent record, and the result is
              already there. So the copy says what it actually does: it puts
              your name to something, and takes down an objection if you had
              one. */}
          <p style={{ color: "var(--ink-dim)" }}>
            The result already counts — this puts your name to it, and takes down your objection
            if you had one. You can change your mind at any point, including years from now.
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
          <h2>{iWon ? "Say you did not win this" : "Say that is not what happened"}</h2>
          <p style={{ color: "var(--ink-dim)" }}>
            This does not undo the result and nobody rules on it. It marks it: the match stays on
            everyone's list with your name and your reason on it, and it counts for nobody — no
            win, no loss, no streak — for as long as you leave it there. One objection is enough,
            you do not need anybody's agreement, and you can take it back whenever you like.
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

      {/* A result nobody in the game was in a position to deny. It counts and
          it is shown like any other, carrying the one fact that makes it
          different — said here rather than enforced, because the old rule that
          held such a claim back for a week ended by recording it anyway. */}
      {match.unwitnessed && (
        <div className="card">
          <p style={{ margin: 0, color: "var(--ink-dim)", fontSize: "0.88rem" }}>
            Nobody who lost this one is on the app, so there is nobody who could say it went
            differently. It counts like any other result — but every other result on the board has
            been open to that, and this one has not. If they sign up and get linked to an account,
            they can object to it then.
          </p>
        </div>
      )}

      <ContestedNotice match={match} presenter={presenter} />
      <AnswerBoard presenter={presenter} match={match} />

      {/* The only thing between the scoreboard and whatever somebody felt like
          entering, so it is on every settled match forever, for everybody in
          it. Seeing a result in the standings is when most people first notice
          one they disagree with, and that is always after it has landed. */}
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
                {claimed && <span className="badge badge--hot">🏆 Won</span>}
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
                    {/* Nothing is redacted — everybody sees who objected. */}
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
          {match.invited.length > 0 && ` · ${match.invited.length} pending`}
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
            <div key={id} style={{ display: "flex", alignItems: "center", gap: 6 }}>
              <button
                className="rowbtn"
                style={{
                  border: "1px solid",
                  borderColor: isSelected ? "var(--gold)" : "var(--line)",
                  borderRadius: "var(--radius)",
                  cursor: canPair ? "pointer" : "default",
                  flex: 1,
                  minWidth: 0,
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
              {match.viewerIsCreator && !presenter.isMe(id) && (
                <button
                  className="btn btn--ghost btn--sm"
                  style={{ color: "var(--ink-faint)" }}
                  aria-label={`Remove ${displayName(p.user)}`}
                  disabled={busy}
                  onClick={() => {
                    setSelected((prev) => prev.filter((x) => x !== id));
                    void presenter.removePlayer(id);
                  }}
                >
                  Remove
                </button>
              )}
            </div>
          );
        })}

        {/* Invited and still deciding. Given a row rather than a sentence
            underneath, because "who is in this game" is one question and
            answering half of it in a list and half of it in prose made the
            people who had not answered yet easy to miss. The pill is what
            separates them: same row, one word of difference.

            Never selectable for pairing. They hold no team until they join,
            so there is nothing to put on a side. */}
        {match.invited.map((p) => {
          const id = p.user?.id ?? "";
          return (
            <button
              key={id}
              className="rowbtn"
              style={{
                border: "1px solid",
                borderColor: "var(--line)",
                borderRadius: "var(--radius)",
                cursor: match.viewerIsCreator ? "pointer" : "default",
                minHeight: 54,
                opacity: match.viewerIsCreator ? 1 : 0.72,
              }}
              disabled={!match.viewerIsCreator || busy}
              onClick={() => match.viewerIsCreator && void presenter.joinFor(id)}
            >
              <Avatar user={p.user} size="sm" />
              <span className="rowbtn__main">
                <span className="rowbtn__name">{displayName(p.user)}</span>
                {match.viewerIsCreator && (
                  <span className="rowbtn__meta">tap if they are here and playing</span>
                )}
              </span>
              <span className="badge badge--wait">Pending</span>
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
          {match.viewerIsCreator
            ? "They can join themselves any time before somebody calls the win — or tap them in if they are sitting right there. They still get asked to agree with however it ends."
            : "Pending players can still join any time before somebody calls the win."}
        </p>
      )}

      {inMatch && <InvitePlayers presenter={presenter} match={match} busy={busy} />}
    </div>
  );
}

/**
 * Adding more people to a game that is already posted, several at a time.
 *
 * This screen went without it for a while, which made posting a game a
 * one-shot: forget somebody at the New game screen and your only recourse was
 * hoping they spotted the match under "Happening now" and joined themselves.
 * Setting a game up is the moment you are least likely to have everybody's
 * name straight, so the ask has to stay available afterward.
 *
 * Whoever posted the match is saying who is at the table, so the people they
 * pick go straight in. Any other player can use it too, as an invitation: that is
 * not a claim on the result and it costs the invitee nothing to decline, so gating
 * it behind the host would just mean waiting for the host.
 */
function InvitePlayers({
  presenter,
  match,
  busy,
}: {
  presenter: MatchPresenter;
  match: Match;
  busy: boolean;
}) {
  const members = useCollection(presenter.members(match));
  const [open, setOpen] = useState(false);
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [selected, setSelected] = useState<string[]>([]);

  const candidates = presenter.invitable(match, members);
  const isHost = match.viewerIsCreator;
  const verb = isHost ? "Add" : "Ask";

  const toggle = (id: string) =>
    setSelected((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]));

  const close = () => {
    setOpen(false);
    setSelected([]);
  };

  const submit = () => {
    void presenter.invitePlayers(selected);
    close();
  };

  const addPlaceholder = () => {
    const trimmed = name.trim();
    if (!trimmed) return;
    void presenter.addPlaceholder(match, trimmed);
    setName("");
    setAdding(false);
  };

  return (
    <>
      <button className="btn btn--block btn--sm" disabled={busy} onClick={() => setOpen(true)}>
        {isHost ? "Add players" : "Ask someone to join"}
      </button>

      {open && (
        <Sheet title={isHost ? "Add players" : "Ask someone to join"} onDismiss={close}>
          <h2>Who else is playing?</h2>
          <p style={{ color: "var(--ink-dim)", fontSize: "0.9rem" }}>
            {isHost
              ? "Everyone you pick goes straight into the game."
              : "They decide for themselves — the game carries on without the ones who never answer."}
          </p>

          <div className="stack sheet__list" style={{ gap: 6 }}>
            {candidates.map((m) => {
              const id = m.user?.id ?? "";
              const picked = selected.includes(id);
              return (
                <button
                  key={id}
                  className="rowbtn"
                  aria-pressed={picked}
                  disabled={busy}
                  onClick={() => toggle(id)}
                >
                  <Avatar user={m.user} size="sm" />
                  <span className="rowbtn__main">
                    <span className="rowbtn__name">{displayName(m.user)}</span>
                    {/* Anyone can put them straight in, because there is no
                        device to ask. For the host that is every row. */}
                    {m.user?.placeholder && (
                      <span className="rowbtn__meta">
                        {isHost ? "not on the app" : "not on the app · added, not asked"}
                      </span>
                    )}
                  </span>
                  <span style={{ color: picked ? "var(--gold)" : "var(--ink-faint)" }}>
                    {picked ? "✓" : "+"}
                  </span>
                </button>
              );
            })}

            {candidates.length === 0 && (
              <p style={{ margin: 0, color: "var(--ink-dim)", fontSize: "0.9rem" }}>
                Everybody on the scoreboard is already in this one, or has answered it.
              </p>
            )}
          </div>

          {selected.length > 0 && (
            <button className="btn btn--primary btn--block" disabled={busy} onClick={submit}>
              {verb} {selected.length}
            </button>
          )}

          {adding ? (
            <div className="stack">
              <div className="field">
                <label className="field__label" htmlFor="match-placeholder-name">
                  Their name
                </label>
                <input
                  id="match-placeholder-name"
                  className="input"
                  placeholder="Grandma"
                  value={name}
                  autoFocus
                  onChange={(e) => setName(e.target.value)}
                  onKeyDown={(e) => e.key === "Enter" && addPlaceholder()}
                />
              </div>
              <p style={{ margin: 0, fontSize: "0.82rem", color: "var(--ink-faint)" }}>
                They get a real spot on the scoreboard, but they cannot concede or object to
                anything, so they go straight into the game.
              </p>
              <button
                className="btn btn--primary btn--block"
                disabled={busy || name.trim() === ""}
                onClick={addPlaceholder}
              >
                Add {name.trim() || "them"}
              </button>
            </div>
          ) : (
            <button className="btn btn--block" disabled={busy} onClick={() => setAdding(true)}>
              Someone who is not on the app
            </button>
          )}

          <button className="btn btn--ghost btn--block" onClick={close}>
            Done
          </button>
        </Sheet>
      )}
    </>
  );
}
