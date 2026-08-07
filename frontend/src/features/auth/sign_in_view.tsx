import { useState } from "react";
import { useSignalValue } from "@tcn/state/react";

import type { Session } from "../../lib/domain/session.js";
import { Alert } from "../../components/ui.js";

/**
 * The way in. A full-page redirect to the server's OAuth endpoint — no token
 * ever touches JavaScript.
 */
export function SignInView({ session }: { session: Session }) {
  const error = useSignalValue(session.errorBroadcast);
  const [devName, setDevName] = useState("Dev Player");

  return (
    <div className="app">
      <div className="hero">
        <span className="hero__mark" aria-hidden="true">
          🏆
        </span>
        <h1 className="hero__title">Scoreboard</h1>
        <p className="hero__tag">
          Family bragging rights, permanently on the record.
          <br />A win only counts when the losers concede it.
        </p>
      </div>

      <div className="stack stack--lg">
        {error && <Alert message={error} />}

        {session.googleEnabled ? (
          <button
            className="btn btn--primary btn--block btn--claim"
            onClick={() => session.signInWithGoogle()}
          >
            Continue with Google
          </button>
        ) : (
          <Alert
            tone="info"
            message="Google sign-in is not configured on this server. Set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET."
          />
        )}

        {session.devLoginEnabled && (
          <div className="card stack">
            <span className="section-label">Local development</span>
            <p style={{ margin: 0, fontSize: "0.85rem", color: "var(--ink-faint)" }}>
              Sign in as anyone, without Google. This route does not exist in production.
            </p>
            <input
              className="input"
              value={devName}
              onChange={(e) => setDevName(e.target.value)}
              placeholder="Name"
            />
            <button
              className="btn btn--block"
              disabled={!devName.trim()}
              onClick={() => session.devLogin(devName.trim())}
            >
              Sign in as {devName.trim() || "…"}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
