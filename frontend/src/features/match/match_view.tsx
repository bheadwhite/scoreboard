import { useEffect, useState } from "react";
import { useRunnerStatus, useSignalValue } from "@tcn/state/react";
import { useCollection } from "../../lib/domain/use_collection.js";
import { Status } from "@tcn/state/core";

import type { AppPresenter } from "../../app/app_presenter.js";
import type { MatchPresenter } from "./match_presenter.js";
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

      {match.status === MatchStatus.LOBBY && (
        <LobbyStage presenter={presenter} match={match} busy={busy} />
      )}

      {match.status === MatchStatus.IN_PROGRESS && (
        <InPlayStage presenter={presenter} match={match} busy={busy} />
      )}

      {match.status === MatchStatus.CLAIMED && (
        <ClaimedStage presenter={presenter} match={match} busy={busy} />
      )}

      {match.status === MatchStatus.DISPUTED && (
        <DisputedStage presenter={presenter} match={match} app={app} busy={busy} />
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

      <TeamList presenter={presenter} match={match} />
    </div>
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
            Agree on how this game is won <em>before</em> you start. It locks when play begins, so
            nobody can relitigate it afterward.
          </p>
        </div>
        <ContractPicker presenter={presenter} busy={busy} />
      </div>

      <TeamBuilder presenter={presenter} match={match} busy={busy} />

      <div className="row">
        {inMatch ? (
          <button className="btn" onClick={() => void presenter.leave()} disabled={busy}>
            Leave
          </button>
        ) : (
          <button className="btn" onClick={() => void presenter.join()} disabled={busy}>
            Join
          </button>
        )}
        <button className="btn btn--ghost" onClick={() => void presenter.cancel()} disabled={busy}>
          Call it off
        </button>
      </div>

      <div className="bottombar">
        <button
          className="btn btn--primary btn--block btn--claim"
          disabled={busy || !contractSet || match.participants.length < 2}
          onClick={() => void presenter.start_()}
        >
          {contractSet ? "Start playing" : "Set the contract first"}
        </button>
      </div>
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

function ContractPicker({ presenter, busy }: { presenter: MatchPresenter; busy: boolean }) {
  const [kind, setKind] = useState<ContractKind>(ContractKind.FIRST_TO_SCORE);
  const [target, setTarget] = useState(500);
  const [detail, setDetail] = useState("");

  const option = CONTRACT_OPTIONS.find((o) => o.kind === kind);
  const needsTarget = option?.needsTarget ?? false;
  const needsDetail = kind === ContractKind.CUSTOM;
  const ready = (!needsTarget || target > 0) && (!needsDetail || detail.trim().length > 0);

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
              setTarget(o.target);
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
            onChange={(e) => setTarget(Number(e.target.value))}
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
        onClick={() => void presenter.setContract(kind, target, detail.trim())}
      >
        Lock in the contract
      </button>
    </div>
  );
}

/**
 * Team assignment. Everyone starts on their own side, which is exactly right
 * for a free-for-all; partners are made by pairing people up.
 */
function TeamBuilder({
  presenter,
  match,
  busy,
}: {
  presenter: MatchPresenter;
  match: Match;
  busy: boolean;
}) {
  const [selected, setSelected] = useState<string[]>([]);

  const toggle = (userId: string) => {
    setSelected((prev) =>
      prev.includes(userId) ? prev.filter((id) => id !== userId) : [...prev, userId],
    );
  };

  const pairUp = () => {
    if (selected.length < 2) return;

    const rest = match.participants
      .map((p) => p.user?.id)
      .filter((id): id is string => id != null && !selected.includes(id));

    const teams = [
      { name: teamNameFor(match, selected), userIds: selected },
      ...rest.map((id) => ({ name: nameOf(match, id), userIds: [id] })),
    ];

    void presenter.setTeams(teams);
    setSelected([]);
  };

  const splitAll = () => {
    const teams = match.participants
      .map((p) => p.user?.id)
      .filter((id): id is string => id != null)
      .map((id) => ({ name: nameOf(match, id), userIds: [id] }));
    void presenter.setTeams(teams);
    setSelected([]);
  };

  return (
    <div className="card stack">
      <div className="row row--between">
        <span className="section-label">Teams</span>
        <button className="btn btn--sm btn--ghost" onClick={splitAll} disabled={busy}>
          Every player for themselves
        </button>
      </div>

      <p style={{ margin: 0, fontSize: "0.85rem", color: "var(--ink-faint)" }}>
        Tap two or more players to make them partners.
      </p>

      <div className="stack" style={{ gap: 8 }}>
        {match.participants.map((p) => {
          const id = p.user?.id ?? "";
          const isSelected = selected.includes(id);
          return (
            <button
              key={id}
              className="btn btn--block"
              style={{
                justifyContent: "flex-start",
                borderColor: isSelected ? "var(--gold)" : undefined,
              }}
              aria-pressed={isSelected}
              onClick={() => toggle(id)}
            >
              <Avatar user={p.user} size="sm" />
              <span>{displayName(p.user)}</span>
              <span className="spacer" />
              <span style={{ color: "var(--ink-faint)", fontSize: "0.82rem" }}>
                {teamNameOf(match, p)}
              </span>
            </button>
          );
        })}
      </div>

      {selected.length >= 2 && (
        <button className="btn btn--primary btn--block" onClick={pairUp} disabled={busy}>
          Make these {selected.length} partners
        </button>
      )}
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
            🏆 We won
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
 * The emotional center of the app. Somebody has claimed a win and the losing
 * side has to answer for it. Conceding is one deliberate tap behind a
 * confirmation, because it is pressed by someone who is annoyed and not looking
 * carefully.
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
  const confirming = useSignalValue(presenter.confirmingConcedeBroadcast);
  const disputing = useSignalValue(presenter.disputingBroadcast);
  const [reason, setReason] = useState("");

  const claimants = winners(match);
  const who = claimants.length === 1 ? displayName(claimants[0]) : winningTeamName(match);
  const owes = match.viewerOwesConcession;

  return (
    <>
      <div className="claim">
        <div className="claim__eyebrow">Claims victory</div>
        <div className="claim__who">{who}</div>
        <div className="claim__says">
          at {gameLabel(match.game)} — {contractText(match.contract)}
        </div>

        {match.awaitingConcession.length > 0 ? (
          <>
            <div className="claim__deadline">
              Waiting on {match.awaitingConcession.length}{" "}
              {match.awaitingConcession.length === 1 ? "person" : "people"} · auto-concedes in{" "}
              {timeLeft(match.concedeDeadline)}
            </div>
            <div className="waiting">
              {match.awaitingConcession.map((u) => (
                <span key={u.id} className="waiting__name">
                  {displayName(u)}
                </span>
              ))}
            </div>
          </>
        ) : (
          <div className="claim__deadline">Settling…</div>
        )}
      </div>

      {owes && (
        <div className="bottombar">
          <div className="stack" style={{ gap: 8 }}>
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
          </div>
        </div>
      )}

      {confirming && (
        <Sheet title="Concede?" onDismiss={presenter.cancelConcede}>
          <h2>Concede to {who}?</h2>
          <p style={{ color: "var(--ink-dim)" }}>
            This goes on the permanent record and cannot be undone.
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
            This stops the clock — the match will not auto-finalize. Your family organizer decides
            how it ends.
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

// ── Disputed ─────────────────────────────────────────────────────────────────

function DisputedStage({
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

  return (
    <div className="card stack">
      <div className="empty" style={{ paddingBottom: 8 }}>
        <span className="empty__emoji">⚖️</span>
        <strong>This result is disputed.</strong>
        <br />
        The clock is stopped. It will not finalize on its own.
      </div>

      {isOwner ? (
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
          Your family organizer will settle it.
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
        {match.participants.length} player{match.participants.length === 1 ? "" : "s"}
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
  const p = match.participants.find((x) => x.user?.id === userId);
  return displayName(p?.user);
}

function teamNameFor(match: Match, userIds: string[]): string {
  return userIds.map((id) => nameOf(match, id)).join(" & ");
}

function teamNameOf(match: Match, participant: MatchParticipant): string {
  const team = match.teams.find((t) => t.id === participant.teamId);
  const size = match.participants.filter((p) => p.teamId === participant.teamId).length;
  return size > 1 ? (team?.name ?? "") : "solo";
}
