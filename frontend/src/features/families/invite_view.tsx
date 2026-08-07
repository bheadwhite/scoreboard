import { useRunnerStatus, useSignalValue } from "@tcn/state/react";
import { Status } from "@tcn/state/core";

import type { AppPresenter } from "../../app/app_presenter.js";
import { Alert } from "../../components/ui.js";

/**
 * Where a /join/CODE link lands.
 *
 * Joining is one deliberate tap rather than automatic. The code is shown, so
 * somebody who followed a link from a group chat can see what they are about to
 * be added to — and someone who tapped it by accident has an obvious way out.
 */
export function InviteView({ app }: { app: AppPresenter }) {
  const code = useSignalValue(app.inviteBroadcast);
  const preview = useSignalValue(app.invitePreviewBroadcast);
  const error = useSignalValue(app.families.errorBroadcast);
  const status = useRunnerStatus(app.families.actionBroadcast);
  const busy = status === Status.PENDING;

  // A code that resolved to nothing is a dead link, not an invitation.
  const dead = !preview;

  return (
    <div className="app">
      <div className="hero">
        <span className="hero__mark" aria-hidden="true">
          {dead ? "🤷" : "✉️"}
        </span>
        <h1 className="hero__title">{dead ? "That link is dead" : "You are invited"}</h1>
        {preview ? (
          <p className="hero__tag">
            {preview.ownerName ? `${preview.ownerName} wants you on` : "You have been invited to"}{" "}
            their family scoreboard.
          </p>
        ) : (
          <p className="hero__tag">
            No family is using the code <strong>{code}</strong>. It may have been changed since the
            link was shared — ask for a new one.
          </p>
        )}
      </div>

      <div className="stack stack--lg">
        {preview && (
          <div className="card" style={{ textAlign: "center" }}>
            <div
              style={{
                fontFamily: "var(--font-display)",
                fontSize: "2rem",
                fontWeight: 800,
                letterSpacing: "-0.03em",
              }}
            >
              {preview.name}
            </div>
            <p style={{ margin: "6px 0 0", color: "var(--ink-dim)", fontSize: "0.9rem" }}>
              <span className="num">{preview.memberCount}</span>{" "}
              {preview.memberCount === 1 ? "member" : "members"}
            </p>
          </div>
        )}

        {error && <Alert message={error} />}

        {preview && (
          <button
            className="btn btn--primary btn--block btn--claim"
            disabled={busy}
            onClick={() => void app.acceptInvite()}
          >
            {busy ? "Joining…" : `Join ${preview.name}`}
          </button>
        )}

        <button className="btn btn--ghost btn--block" onClick={app.dismissInvite} disabled={busy}>
          {dead ? "Continue" : "Not now"}
        </button>
      </div>
    </div>
  );
}
