import { useEffect } from "react";
import { useSignalValue } from "@tcn/state/react";
import { useCollection } from "../../lib/domain/use_collection.js";

import type { AppPresenter } from "../../app/app_presenter.js";
import type { FamiliesPresenter } from "./families_presenter.js";
import { FamilyRole } from "../../gen/scoreboard/v1/common_pb.js";
import { Alert, Avatar, Empty } from "../../components/ui.js";
import { displayName } from "../../lib/format.js";

/**
 * The family screen: the invite link that brings the next relative in, and
 * everyone who is already here.
 *
 * There is nothing to choose between. One scoreboard, so the family is never
 * named and never switched — the only thing this screen is for is the roster
 * and the link.
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
  const members = useCollection(presenter.members(familyId));

  const family = families.find((f) => f.id === familyId);
  const isOwner = family?.viewerRole === FamilyRole.OWNER;
  const meId = app.domain.session.user?.id;

  useEffect(() => {
    if (!familyId) return;
    presenter.start(familyId);
    return () => presenter.stop();
  }, [presenter, familyId]);

  if (!family) {
    return <Empty emoji="👋">You are not on the scoreboard yet.</Empty>;
  }

  return (
    <div className="stack stack--lg">
      <section className="stack">
        <span className="section-label">Invite</span>
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
            Send this to whoever is playing. They sign in and they are on the scoreboard.
          </p>
          <div className="stack" style={{ gap: 8 }}>
            <button
              className="btn btn--primary btn--block"
              onClick={() => void presenter.shareInvite(family.inviteCode)}
            >
              {copied ? "Link copied ✓" : "Share invite link"}
            </button>
            <p style={{ margin: 0, fontSize: "0.78rem", color: "var(--ink-faint)", wordBreak: "break-all" }}>
              {presenter.inviteLink(family.inviteCode)}
            </p>
          </div>
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
          {members.length} player{members.length === 1 ? "" : "s"}
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

      <button className="btn btn--ghost btn--block" onClick={() => void app.signOut()}>
        Sign out
      </button>
    </div>
  );
}
