import { useSignalValue, useRunnerStatus } from "@tcn/state/react";
import { Status } from "@tcn/state/core";

import type { FamiliesPresenter } from "./families_presenter.js";
import { Alert } from "../../components/ui.js";

/**
 * The first screen a new player sees: start a family, or join the one your
 * relatives already made.
 */
export function OnboardingView({ presenter }: { presenter: FamiliesPresenter }) {
  const mode = useSignalValue(presenter.modeBroadcast);
  const name = useSignalValue(presenter.nameBroadcast);
  const code = useSignalValue(presenter.codeBroadcast);
  const error = useSignalValue(presenter.errorBroadcast);
  const status = useRunnerStatus(presenter.actionBroadcast);
  const busy = status === Status.PENDING;

  return (
    <div className="app">
      <div className="hero">
        <span className="hero__mark" aria-hidden="true">
          🏆
        </span>
        <h1 className="hero__title">Welcome</h1>
        <p className="hero__tag">A family is where the scoreboard lives.</p>
      </div>

      <div className="stack stack--lg">
        <div className="chips" role="tablist" aria-label="Create or join">
          <button
            className="chip"
            role="tab"
            aria-pressed={mode === "create"}
            aria-selected={mode === "create"}
            onClick={() => presenter.setMode("create")}
          >
            Start a family
          </button>
          <button
            className="chip"
            role="tab"
            aria-pressed={mode === "join"}
            aria-selected={mode === "join"}
            onClick={() => presenter.setMode("join")}
          >
            Join one
          </button>
        </div>

        {mode === "create" ? (
          <form
            className="card stack"
            onSubmit={(e) => {
              e.preventDefault();
              void presenter.create();
            }}
          >
            <div className="field">
              <label className="field__label" htmlFor="family-name">
                Family name
              </label>
              <input
                id="family-name"
                className="input"
                placeholder="The Whites"
                value={name}
                onChange={(e) => presenter.setName(e.target.value)}
              />
            </div>
            <button className="btn btn--primary btn--block" type="submit" disabled={busy}>
              {busy ? "Creating…" : "Create family"}
            </button>
            <p style={{ margin: 0, fontSize: "0.85rem", color: "var(--ink-faint)" }}>
              You will get an invite code to share with everyone else.
            </p>
          </form>
        ) : (
          <form
            className="card stack"
            onSubmit={(e) => {
              e.preventDefault();
              void presenter.join();
            }}
          >
            <div className="field">
              <label className="field__label" htmlFor="invite-code">
                Invite code
              </label>
              <input
                id="invite-code"
                className="input input--code"
                placeholder="ABC123"
                maxLength={6}
                autoCapitalize="characters"
                autoComplete="off"
                value={code}
                onChange={(e) => presenter.setCode(e.target.value)}
              />
            </div>
            <button
              className="btn btn--primary btn--block"
              type="submit"
              disabled={busy || code.length < 4}
            >
              {busy ? "Joining…" : "Join family"}
            </button>
          </form>
        )}

        {error && <Alert message={error} />}
      </div>
    </div>
  );
}
