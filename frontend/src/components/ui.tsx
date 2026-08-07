import type { ReactNode } from "react";
import { useRunnerError, useRunnerStatus } from "@tcn/state/react";
import { Status, type IRunnerBroadcast } from "@tcn/state/core";

import type { User } from "../gen/scoreboard/v1/common_pb.js";
import { displayName, initials } from "../lib/format.js";

/**
 * AsyncBoundary renders a runner's loading and error states and delegates the
 * rest to its children, so every screen handles async the same way.
 */
export function AsyncBoundary<T>({
  runner,
  children,
  hideChildrenWhilePending = false,
}: {
  runner: IRunnerBroadcast<T>;
  children: ReactNode;
  /** Hide children while loading, instead of showing stale data underneath. */
  hideChildrenWhilePending?: boolean;
}) {
  const status = useRunnerStatus(runner);
  const error = useRunnerError(runner);

  if (status === Status.ERROR) {
    return (
      <div className="async-error" role="alert">
        <strong>Something went wrong.</strong>
        <span>{error?.message ?? "Unknown error"}</span>
      </div>
    );
  }

  const pending = status === Status.PENDING;
  const initial = status === Status.INITIAL;

  if ((pending || initial) && hideChildrenWhilePending) {
    return <div className="async-loading">Loading…</div>;
  }

  return (
    <>
      {pending && <div className="async-loading subtle">Updating…</div>}
      {children}
    </>
  );
}

export function Avatar({
  user,
  size = "md",
}: {
  user: User | undefined;
  size?: "sm" | "md" | "lg";
}) {
  const name = displayName(user);
  const className = `avatar${size === "lg" ? " avatar--lg" : size === "sm" ? " avatar--sm" : ""}`;

  if (user?.avatarUrl) {
    return <img className={className} src={user.avatarUrl} alt="" aria-hidden="true" />;
  }
  return (
    <span className={className} aria-hidden="true">
      {initials(name)}
    </span>
  );
}

/** A win–loss record, styled so the win half reads first. */
export function Record({ wins, losses }: { wins: number; losses: number }) {
  return (
    <span className="record num">
      <span className="record__w">{wins}</span>
      <span className="record__sep">–</span>
      <span className="record__l">{losses}</span>
    </span>
  );
}

export function Alert({ message, tone = "error" }: { message: string; tone?: "error" | "info" }) {
  return (
    <div className={tone === "info" ? "alert alert--info" : "alert"} role="alert">
      {message}
    </div>
  );
}

export function Empty({ emoji, children }: { emoji: string; children: ReactNode }) {
  return (
    <div className="empty">
      <span className="empty__emoji" aria-hidden="true">
        {emoji}
      </span>
      {children}
    </div>
  );
}

/**
 * A bottom sheet for the two decisions that deserve a beat of hesitation:
 * conceding, and disputing someone else's claim.
 */
export function Sheet({
  title,
  onDismiss,
  children,
}: {
  title: string;
  onDismiss: () => void;
  children: ReactNode;
}) {
  return (
    <div
      className="sheet"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onClick={(e) => {
        if (e.target === e.currentTarget) onDismiss();
      }}
    >
      <div className="sheet__panel">{children}</div>
    </div>
  );
}
