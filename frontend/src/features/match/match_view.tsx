import { useEffect, useRef, useState } from "react";
import { useRunnerStatus, useSignalValue } from "@tcn/state/react";
import { useCollection } from "../../lib/domain/use_collection.js";
import { Status } from "@tcn/state/core";

import type { AppPresenter } from "../../app/app_presenter.js";
import type { Answer, MatchPresenter } from "./match_presenter.js";
import type { Match, MatchParticipant } from "../../gen/scoreboard/v1/common_pb.js";
import { ContractKind, MatchStatus } from "../../gen/scoreboard/v1/common_pb.js";
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
 * One match, from lobby to permanent record. The screen changes shape at each
 * stage rather than showing every control at once, because at any given moment
 * there is really only one thing to do.
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

      {match.status === MatchStatus.LOBBY && match.viewerIsInvited && (
        <InvitationStage presenter={presenter} match={match} busy={busy} />
      )}

      {match.status === MatchStatus.LOBBY && !match.viewerIsInvited && (
        <LobbyStage presenter={presenter} match={match} busy={busy} />
      )}

      {match.status === MatchStatus.IN_PROGRESS && (
        <InPlayStage presenter={presenter} match={match} busy={busy} />
      )}

      {(match.status === MatchStatus.CLAIMED || match.status === MatchStatus.DISPUTED) && (
        <ClaimedStage presenter={presenter} match={match} app={app} busy={busy} />
      )}

      {match.status === MatchStatus.FINAL && <FinalStage match={match} />}

      {match.status === MatchStatus.CANCELED && (
        <div className="card">
          <div className="empty">
            <span className="empty__emoji">🚫</span>
            This match was called off. Nothing was recorded.
          </div>
        </div>
      )}

      {match.status !== MatchStatus.LOBBY && (
        <TeamList presenter={presenter} match={match} />
      )}

      {presenter.cancelAuthority(match) === "organizer" && (
        <AbandonMatch presenter={presenter} match={match} busy={busy} />
      )}
    </div>
  );
}

/**
 * Calling off a game that is not yours — the organizer's override. Always behind
 * a confirmation, because it is somebody else's game either way: in a lobby it
 * is a host's plans, and in play it is a game people actually sat down and
 * played, which is exactly the thing the record is for. This is the escape hatch
 * for one that fizzled out, not a way to undo a result somebody dislikes.
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
  const inLobby = match.status === MatchStatus.LOBBY;

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
            {inLobby ? (
              <>
                You did not set this one up. {gameLabel(match.game)} disappears for everyone
                waiting in it, and they are not told why. If you simply do not want to play, leave
                instead — the rest of them can carry on.
              </>
            ) : (
              <>
                {gameLabel(match.game)} disappears and nothing is recorded — no win, no loss, for
                anybody. Use this for a game that fizzled out, not for a result somebody disagrees
                with. A disputed result is settled by resolving the dispute.
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

// ── Lobby ────────────────────────────────────────────────────────────────────

function LobbyStage({
  presenter,
  match,
  busy,
}: {
  presenter: MatchPresenter;
  match: Match;
  busy: boolean;
}) {
  const contractSet = match.contract != null && match.contract.kind !== ContractKind.UNSPECIFIED;
  const inMatch = presenter.amIn(match);

  return (
    <div className="stack">
      <div className="card stack">
        <div>
          <span className="section-label">Winning contract</span>
          <h2 style={{ marginTop: 4 }}>
            {contractSet ? contractText(match.contract) : "Not set yet"}
          </h2>
          <p style={{ margin: "6px 0 0", fontSize: "0.88rem", color: "var(--ink-dim)" }}>
            {match.viewerIsCreator
              ? "Set how this game is won. Changing it asks everyone to confirm again, and it locks the moment play begins."
              : "Whoever set the game up decides the terms. Confirm below once you are happy with them."}
          </p>
        </div>
        {/* The terms belong to whoever set the game up. Everyone else reads
            them and affirms, which is what the ready check is for. */}
        {match.viewerIsCreator && (
          <ContractPicker presenter={presenter} match={match} busy={busy} />
        )}
      </div>

      {/* Shown whether or not you are in yet. Who is already waiting is most of
          what you want to know before deciding to join. */}
      <LobbyRoster presenter={presenter} match={match} busy={busy} inMatch={inMatch} />

      <div className="row">
        {inMatch ? (
          <button className="btn" onClick={() => void presenter.leave()} disabled={busy}>
            Leave
          </button>
        ) : (
          <button className="btn btn--primary" onClick={() => void presenter.join()} disabled={busy}>
            Join this game
          </button>
        )}
        {/* Only the host gets the one-tap version, because this is their own
            game. An organizer looking at somebody else's lobby goes through
            AbandonMatch instead. */}
        {presenter.cancelAuthority(match) === "creator" && (
          <button className="btn btn--ghost" onClick={() => void presenter.cancel()} disabled={busy}>
            Call it off
          </button>
        )}
      </div>

      {inMatch && (
        <div className="bottombar">
          <ReadyOrStart presenter={presenter} match={match} busy={busy} contractSet={contractSet} />
        </div>
      )}
    </div>
  );
}

const CONTRACT_OPTIONS = [
  { kind: ContractKind.FIRST_TO_SCORE, label: "First to…", needsTarget: true, target: 500 },
  { kind: ContractKind.BEST_OF, label: "Best of…", needsTarget: true, target: 3 },
  { kind: ContractKind.LAST_STANDING, label: "Last standing", needsTarget: false, target: 0 },
  { kind: ContractKind.SINGLE_GAME, label: "Single game", needsTarget: false, target: 0 },
  { kind: ContractKind.CUSTOM, label: "Custom", needsTarget: false, target: 0 },
];

function ContractPicker({
  presenter,
  match,
  busy,
}: {
  presenter: MatchPresenter;
  match: Match;
  busy: boolean;
}) {
  const [kind, setKind] = useState<ContractKind>(ContractKind.FIRST_TO_SCORE);
  // Held as text, not a number. Number("") is 0, so a numeric state variable
  // re-renders a "0" the instant you clear the field and you can never type a
  // value without selecting the old one first.
  const [target, setTarget] = useState("500");
  const [detail, setDetail] = useState("");
  const targetValue = Number(target);

  // Adopt whatever the group has settled on. Everyone is editing the same
  // contract from a different phone, so a change by one player has to show up
  // in everybody else's picker — otherwise two people see different terms and
  // the last one to tap wins silently.
  //
  // Keyed on the incoming value, not on every render, so it does not fight
  // somebody midway through typing their own.
  const incoming = match.contract;
  const signature = incoming
    ? `${incoming.kind}|${incoming.target}|${incoming.detail}`
    : "";
  const lastAdopted = useRef<string | null>(null);

  useEffect(() => {
    if (!incoming || incoming.kind === ContractKind.UNSPECIFIED) return;
    if (lastAdopted.current === signature) return;
    lastAdopted.current = signature;
    setKind(incoming.kind);
    if (incoming.target > 0) setTarget(String(incoming.target));
    setDetail(incoming.detail);
  }, [signature, incoming]);

  const option = CONTRACT_OPTIONS.find((o) => o.kind === kind);
  const needsTarget = option?.needsTarget ?? false;
  const needsDetail = kind === ContractKind.CUSTOM;
  const ready =
    (!needsTarget || (Number.isFinite(targetValue) && targetValue > 0)) &&
    (!needsDetail || detail.trim().length > 0);

  return (
    <div className="stack">
      <div className="chips" role="group" aria-label="Victory condition">
        {CONTRACT_OPTIONS.map((o) => (
          <button
            key={o.kind}
            className="chip"
            aria-pressed={kind === o.kind}
            onClick={() => {
              setKind(o.kind);
              setTarget(String(o.target));
            }}
          >
            {o.label}
          </button>
        ))}
      </div>

      {needsTarget && (
        <div className="field">
          <label className="field__label" htmlFor="contract-target">
            {kind === ContractKind.BEST_OF ? "How many rounds" : "Points to win"}
          </label>
          <input
            id="contract-target"
            className="input num"
            type="number"
            inputMode="numeric"
            min={1}
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            onFocus={(e) => e.target.select()}
          />
        </div>
      )}

      <div className="field">
        <label className="field__label" htmlFor="contract-detail">
          {needsDetail ? "How is this won?" : "Notes (optional)"}
        </label>
        <input
          id="contract-detail"
          className="input"
          placeholder={needsDetail ? "Whoever still has chips at midnight" : "House rules, etc."}
          value={detail}
          onChange={(e) => setDetail(e.target.value)}
        />
      </div>

      <button
        className="btn btn--block"
        disabled={busy || !ready}
        onClick={() => void presenter.setContract(kind, targetValue || 0, detail.trim())}
      >
        Lock in the contract
      </button>
    </div>
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
  const [picking, setPicking] = useState(false);
  const myTeam = presenter.myTeamId(match);
  const teams = presenter.teamsWithPlayers(match);

  return (
    <>
      <div className="card">
        <span className="section-label">In play</span>
        <h2 style={{ marginTop: 4 }}>{contractText(match.contract)}</h2>
        <p style={{ margin: "6px 0 0", color: "var(--ink-dim)", fontSize: "0.88rem" }}>
          Started {timeAgo(match.startedAt)}. When someone wins, claim it here — it does not count
          until the other side concedes.
        </p>
      </div>

      <div className="bottombar">
        {myTeam ? (
          <button
            className="btn btn--primary btn--block btn--claim"
            disabled={busy}
            onClick={() => void presenter.claimVictory(myTeam)}
          >
            {presenter.amSolo(match) ? "🏆 I won" : "🏆 We won"}
          </button>
        ) : (
          <button
            className="btn btn--block btn--claim"
            disabled={busy}
            onClick={() => setPicking(true)}
          >
            Record the winner
          </button>
        )}
      </div>

      {picking && (
        <Sheet title="Who won?" onDismiss={() => setPicking(false)}>
          <h2>Who won?</h2>
          <p style={{ color: "var(--ink-dim)", fontSize: "0.9rem" }}>
            They will still need the other side to concede.
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
 * The emotional center of the app. Somebody has claimed a win and everyone else
 * has to answer for it.
 *
 * One screen serves CLAIMED and DISPUTED, because they are not two stages — they
 * are the same thing (a claim on the table, a set of answers to it) read with
 * and without an objection in the pile. Splitting them produced a screen that
 * went blank the moment you answered, as though your part were over; in fact
 * nothing here is settled until the match finalizes, and every answer on it,
 * including the claim, can still be changed by the person who gave it.
 */
function ClaimedStage({
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
  const claimants = winners(match);
  const who = claimants.length === 1 ? displayName(claimants[0]) : winningTeamName(match);
  const disputed = match.status === MatchStatus.DISPUTED;

  return (
    <>
      <div className="claim">
        <div className="claim__eyebrow">{disputed ? "Claim disputed" : "Claims victory"}</div>
        <div className="claim__who">{who}</div>
        <div className="claim__says">
          at {gameLabel(match.game)} — {contractText(match.contract)}
        </div>

        {/* An objection stops the clock, so there is no deadline to report and
            saying nothing about time is the honest thing to show. */}
        {disputed ? (
          <div className="claim__deadline">
            ⚖️ Somebody says that is not what happened. The clock is stopped — this will not
            finalize on its own.
          </div>
        ) : match.awaitingConcession.length > 0 ? (
          <div className="claim__deadline">
            Waiting on {match.awaitingConcession.length}{" "}
            {match.awaitingConcession.length === 1 ? "person" : "people"} · counts as conceded in{" "}
            {timeLeft(match.concedeDeadline)} if nobody answers
          </div>
        ) : (
          <div className="claim__deadline">Settling…</div>
        )}
      </div>

      <AnswerBoard presenter={presenter} match={match} />
      <YourAnswer presenter={presenter} match={match} who={who} busy={busy} />
      <OrganizerRuling presenter={presenter} match={match} app={app} busy={busy} />
    </>
  );
}

const ANSWER_BADGE: Record<Answer, { label: string; className: string }> = {
  claimant: { label: "🏆 claims the win", className: "badge badge--hot" },
  conceded: { label: "conceded", className: "badge" },
  disputed: { label: "disputes it", className: "badge badge--wait" },
  silent: { label: "no answer yet", className: "badge badge--wait" },
};

/**
 * Where everybody stands, right now. This is the running tally the match is
 * waiting on, and it is the thing that makes an unanswered claim feel like an
 * open question rather than a countdown.
 */
function AnswerBoard({ presenter, match }: { presenter: MatchPresenter; match: Match }) {
  const answers = presenter.answers(match);
  // Who objected is redacted server-side for everyone but the organizer, so a
  // disputer reads as "no answer yet" to the rest of the family. Say that
  // rather than let the board imply somebody has ignored it.
  const hidden =
    match.status === MatchStatus.DISPUTED && !answers.some((a) => a.state === "disputed");

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
      {hidden && (
        <p style={{ margin: 0, fontSize: "0.8rem", color: "var(--ink-faint)" }}>
          One of them is objecting. Who, and why, is shown only to your family organizer.
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
  const settles = presenter.concessionWouldSettleIt(match);

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
          {answer === "conceded" ? (
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

      {confirming && (
        <Sheet title="Concede?" onDismiss={presenter.cancelConcede}>
          <h2>Concede to {who}?</h2>
          {/* Finalizing is the one step in this whole protocol that cannot be
              undone, so whether this tap is that step decides what to warn
              about. Every other concession can be taken back. */}
          <p style={{ color: "var(--ink-dim)" }}>
            {settles
              ? "You are the last one they are waiting on, so this puts it on the permanent record straight away. That part cannot be undone."
              : "You can change your mind while the others are still answering. Once everybody has conceded it goes on the permanent record for good."}
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
      )}

      {disputing && (
        <Sheet title="Dispute" onDismiss={presenter.cancelDispute}>
          <h2>Dispute this claim</h2>
          <p style={{ color: "var(--ink-dim)" }}>
            This stops the clock — the match will not finalize on its own for as long as your
            objection stands. You can take it back at any time, and whoever claimed can withdraw
            the claim. Your organizer only steps in if you all get stuck.
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
              Submit dispute
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

/**
 * The organizer's last resort.
 *
 * Gated on organizerMayResolve, which the server computes and also enforces: the
 * ruling only becomes available once the players genuinely cannot move it
 * themselves. An organizer who can settle any disagreement the moment it starts
 * is a referee, and a family that plays in front of a referee stops arguing —
 * which is the part of this app worth protecting.
 */
function OrganizerRuling({
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
  const families = useCollection(app.families.families);
  const family = families.find((f) => f.id === match.familyId);
  const isOwner = family?.ownerId === app.domain.session.user?.id;
  const teams = presenter.teamsWithPlayers(match);
  // Redacted server-side for anyone but the organizer, so this is empty for
  // everyone else even before the check above.
  const objections = match.participants.filter((p) => p.disputedAt != null);

  if (match.status !== MatchStatus.DISPUTED) return null;

  if (!isOwner) {
    return (
      <div className="card">
        <p style={{ margin: 0, color: "var(--ink-dim)", fontSize: "0.9rem" }}>
          {match.organizerMayResolve
            ? "Nobody is budging, so your family organizer can settle this one."
            : "Sort it out between you. If you get properly stuck, your family organizer can step in."}
        </p>
      </div>
    );
  }

  return (
    <div className="card stack">
      {objections.length > 0 && (
        <div className="stack" style={{ gap: 8 }}>
          <span className="section-label">What was objected to</span>
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
          <p style={{ margin: 0, fontSize: "0.78rem", color: "var(--ink-faint)" }}>
            Only you can see this. Nobody else in the match is told who objected or why.
          </p>
        </div>
      )}

      {match.organizerMayResolve ? (
        <>
          <span className="section-label">You organize this family — call it</span>
          <div className="stack" style={{ gap: 8 }}>
            {teams.map((team) => (
              <button
                key={team.id}
                className="btn btn--block"
                disabled={busy}
                onClick={() => void presenter.resolveDispute(team.id)}
              >
                {team.name} won
              </button>
            ))}
            <button
              className="btn btn--ghost btn--block"
              disabled={busy}
              onClick={() => void presenter.resolveDispute("", true)}
            >
              Void it — nobody gets this one
            </button>
          </div>
        </>
      ) : (
        <p style={{ margin: 0, color: "var(--ink-dim)", fontSize: "0.9rem" }}>
          You organize this family, so you can settle this — but not yet. They are still arguing it
          out, and somebody has not answered. Give them the chance to.
        </p>
      )}
    </div>
  );
}

// ── Final ────────────────────────────────────────────────────────────────────

function FinalStage({ match }: { match: Match }) {
  const players = winners(match);
  const who = players.length === 1 ? displayName(players[0]) : winningTeamName(match);

  return (
    <div className="card">
      <div className="crown">
        <span className="crown__emoji" aria-hidden="true">
          🏆
        </span>
        <div className="crown__who">{who}</div>
        <p style={{ margin: "8px 0 0", color: "var(--ink-dim)" }}>
          won at {gameLabel(match.game)} · {contractText(match.contract)}
        </p>
        <p style={{ margin: "4px 0 0", color: "var(--ink-faint)", fontSize: "0.85rem" }}>
          On the record {timeAgo(match.finalizedAt)}
        </p>
      </div>
    </div>
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
          {displayName(host)} wants you in
          {match.contract && match.contract.kind !== ContractKind.UNSPECIFIED
            ? ` — ${contractText(match.contract)}`
            : ""}
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
            I am in
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
 * The bottom bar in a lobby. Everyone affirms; only the creator starts.
 *
 * Withdrawing has to stay reachable. Saying you are ready is a statement about
 * terms that can still change and about a game that has not begun — treating it
 * as a one-way door means the only exit from a lobby you have second thoughts
 * about is leaving it altogether.
 */
function ReadyOrStart({
  presenter,
  match,
  busy,
  contractSet,
}: {
  presenter: MatchPresenter;
  match: Match;
  busy: boolean;
  contractSet: boolean;
}) {
  if (!contractSet) {
    return (
      <button className="btn btn--block btn--claim" disabled>
        {match.viewerIsCreator ? "Set the contract first" : "Waiting for the contract"}
      </button>
    );
  }

  // Not ready yet: affirming is the only thing this player should be doing.
  if (!match.viewerIsReady) {
    return (
      <button
        className="btn btn--primary btn--block btn--claim"
        disabled={busy}
        onClick={() => void presenter.setReady(true)}
      >
        I'm ready — {contractText(match.contract)}
      </button>
    );
  }

  const withdraw = (
    <button
      className="btn btn--ghost btn--block btn--sm"
      disabled={busy}
      onClick={() => void presenter.setReady(false)}
    >
      Actually, not ready
    </button>
  );

  if (!match.viewerIsCreator) {
    return (
      <div className="stack" style={{ gap: 8 }}>
        <button className="btn btn--block btn--claim" disabled>
          {match.allReady
            ? "Waiting for the game to start"
            : `Waiting on ${match.awaitingReady.length} more`}
        </button>
        {withdraw}
      </div>
    );
  }

  const tooFew = match.joined.length < 2;
  return (
    <div className="stack" style={{ gap: 8 }}>
      <button
        className="btn btn--primary btn--block btn--claim"
        disabled={busy || tooFew || !match.allReady}
        onClick={() => void presenter.start_()}
      >
        {tooFew
          ? "Waiting for someone to join"
          : !match.allReady
            ? `Waiting on ${match.awaitingReady.map((u) => displayName(u)).join(", ")}`
            : `Start with ${match.joined.length} players`}
      </button>
      {withdraw}
    </div>
  );
}

/**
 * The lobby's single roster.
 *
 * This used to be three separate cards — a team builder, a ready board, and a
 * player list — all showing the same people. One row per player carries
 * everything instead: who they are, which side they are on, and whether they
 * have confirmed the terms.
 */
function LobbyRoster({
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

  const contractSet = match.contract != null && match.contract.kind !== ContractKind.UNSPECIFIED;
  // Teams are part of the terms, so they are the creator's to arrange — and
  // only worth arranging once there are enough people to pair up. Someone who
  // set the match up and then left is reading it, not running it.
  const canPair = match.viewerIsCreator && inMatch && match.joined.length > 2;
  const readyCount = match.joined.length - match.awaitingReady.length;

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
        {contractSet && (
          <span className="section-label num">
            {readyCount} / {match.joined.length} ready
          </span>
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
          const ready = p.readyAt != null;
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
                {partners > 1 && (
                  <span className="rowbtn__meta">
                    with {teamNameOf(match, p)}
                  </span>
                )}
              </span>
              {contractSet &&
                (ready ? (
                  <span className="badge badge--live">✓ Ready</span>
                ) : (
                  <span className="badge">waiting</span>
                ))}
            </button>
          );
        })}
      </div>

      {canPair && (
        <>
          <p style={{ margin: 0, fontSize: "0.8rem", color: "var(--ink-faint)" }}>
            Tap two or more players to make them partners. Changing sides asks everyone to confirm
            again.
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
          Still deciding: {match.invited.map((p) => displayName(p.user)).join(", ")}. You can start
          without them.
        </p>
      )}
    </div>
  );
}
