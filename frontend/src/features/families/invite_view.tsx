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
  const error = useSignalValue(app.families.errorBroadcast);
  const status = useRunnerStatus(app.families.actionBroadcast);
  const busy = status === Status.PENDING;

  return (
    <div className="app">
      <div className="hero">
        <span className="hero__mark" aria-hidden="true">
          ✉️
        </span>
        <h1 className="hero__title">You are invited</h1>
        <p className="hero__tag">Someone shared their family scoreboard with you.</p>
      </div>

      <div className="stack stack--lg">
        <div className="card" style={{ textAlign: "center" }}>
          <span className="section-label">Invite code</span>
          <div
            className="num"
            style={{
              marginTop: 8,
              fontFamily: "var(--font-mono)",
              fontSize: "2.2rem",
              fontWeight: 800,
              letterSpacing: "0.3em",
              textIndent: "0.3em",
              color: "var(--gold)",
            }}
          >
            {code}
          </div>
        </div>

        {error && <Alert message={error} />}

        <button
          className="btn btn--primary btn--block btn--claim"
          disabled={busy}
          onClick={() => void app.acceptInvite()}
        >
          {busy ? "Joining…" : "Join this family"}
        </button>

        <button className="btn btn--ghost btn--block" onClick={app.dismissInvite} disabled={busy}>
          Not now
        </button>
      </div>
    </div>
  );
}
