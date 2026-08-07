import { useSignalValue } from "@tcn/state/react";
import { useCollection } from "../../lib/domain/use_collection.js";

import type { AppPresenter } from "../../app/app_presenter.js";
import type { FamiliesPresenter } from "./families_presenter.js";
import { FamilyRole } from "../../gen/scoreboard/v1/common_pb.js";
import { Alert, Avatar, Empty } from "../../components/ui.js";
import { displayName } from "../../lib/format.js";

/**
 * The family screen: the invite code to read out loud, everyone in it, and the
 * switcher for people who belong to more than one.
 */
export function FamilyView({
  presenter,
  app,
}: {
  presenter: FamiliesPresenter;
  app: AppPresenter;
}) {
  // Every hook runs unconditionally and before any early return.
  const families = useCollection(presenter.families);
  const familyId = useSignalValue(app.familyIdBroadcast);
  const copied = useSignalValue(presenter.copiedBroadcast);
  const error = useSignalValue(presenter.errorBroadcast);
  const code = useSignalValue(presenter.codeBroadcast);
  const members = useCollection(presenter.members(familyId));

  const family = families.find((f) => f.id === familyId);
  const isOwner = family?.viewerRole === FamilyRole.OWNER;
  const meId = app.domain.session.user?.id;

  if (!family) {
    return <Empty emoji="👋">Pick a family to get started.</Empty>;
  }

  return (
    <div className="stack stack--lg">
      <section className="stack">
        <span className="section-label">Invite code</span>
        <div className="card" style={{ textAlign: "center" }}>
          <div
            className="num"
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: "2.4rem",
              fontWeight: 800,
              letterSpacing: "0.3em",
              textIndent: "0.3em",
              color: "var(--gold)",
            }}
          >
            {family.inviteCode}
          </div>
          <p style={{ margin: "8px 0 14px", color: "var(--ink-dim)", fontSize: "0.88rem" }}>
            Anyone with this code can join {family.name}.
          </p>
          <div className="row">
            <button
              className="btn btn--block"
              onClick={() => void presenter.copyCode(family.inviteCode)}
            >
              {copied ? "Copied ✓" : "Copy code"}
            </button>
            {isOwner && (
              <button
                className="btn btn--ghost"
                onClick={() => void presenter.regenerateCode(family.id)}
              >
                New code
              </button>
            )}
          </div>
        </div>
      </section>

      {error && <Alert message={error} />}

      <section className="stack">
        <span className="section-label">
          {members.length} member{members.length === 1 ? "" : "s"}
        </span>
        <div className="card card--flush">
          {members.map((m) => (
            <div key={m.user?.id} className="rowbtn">
              <Avatar user={m.user} size="sm" />
              <button
                className="rowbtn__main"
                style={{
                  border: 0,
                  background: "none",
                  color: "inherit",
                  textAlign: "left",
                  padding: 0,
                }}
                onClick={() => m.user && app.openPlayer(m.user.id)}
              >
                <span className="rowbtn__name">
                  {displayName(m.user)}
                  {m.user?.id === meId && (
                    <span style={{ color: "var(--ink-faint)" }}> (you)</span>
                  )}
                </span>
                {m.role === FamilyRole.OWNER && (
                  <span className="rowbtn__meta">Organizer</span>
                )}
              </button>
              {isOwner && m.role !== FamilyRole.OWNER && m.user && (
                <button
                  className="btn btn--sm btn--ghost"
                  onClick={() => void presenter.removeMember(family.id, m.user!.id)}
                >
                  Remove
                </button>
              )}
            </div>
          ))}
        </div>
      </section>

      {families.length > 1 && (
        <section className="stack">
          <span className="section-label">Your families</span>
          <div className="card card--flush">
            {families.map((f) => (
              <button
                key={f.id}
                className="rowbtn"
                aria-pressed={f.id === familyId}
                onClick={() => app.selectFamily(f.id)}
              >
                <span className="rowbtn__main">
                  <span className="rowbtn__name">{f.name}</span>
                  <span className="rowbtn__meta num">{f.memberCount} members</span>
                </span>
                {f.id === familyId && <span style={{ color: "var(--gold)" }}>✓</span>}
              </button>
            ))}
          </div>
        </section>
      )}

      <section className="stack">
        <span className="section-label">Join another family</span>
        <div className="card stack">
          <input
            className="input input--code"
            placeholder="ABC123"
            maxLength={6}
            autoCapitalize="characters"
            autoComplete="off"
            value={code}
            onChange={(e) => presenter.setCode(e.target.value)}
          />
          <button className="btn btn--block" onClick={() => void presenter.join()}>
            Join
          </button>
        </div>
      </section>

      <button className="btn btn--ghost btn--block" onClick={() => void app.signOut()}>
        Sign out
      </button>
    </div>
  );
}
