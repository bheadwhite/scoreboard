import { Signal } from "@tcn/state/core";

import type { User } from "../../gen/scoreboard/v1/common_pb.js";
import { fetchCapabilities, type Capabilities } from "../runtime_config.js";

export type AuthStatus = "loading" | "signed_out" | "signed_in";

/** Where the browser is now, as a same-origin path. */
function currentPath(): string {
  return window.location.pathname + window.location.search;
}

/**
 * Human-readable versions of the `?auth_error=` codes the server redirects back
 * with when the OAuth round trip fails.
 */
const AUTH_ERRORS: Record<string, string> = {
  google_not_configured: "Google sign-in is not set up on this server yet.",
  bad_state: "That sign-in link expired. Please try again.",
  missing_state: "That sign-in link expired. Please try again.",
  missing_code: "Google did not send us back a sign-in code. Try again.",
  token_exchange_failed: "Google would not complete the sign-in. Try again.",
  no_profile: "We could not read your Google profile.",
  login_failed: "Something went wrong creating your account.",
  access_denied: "Sign-in was cancelled.",
  server_error: "Something went wrong on our end. Try again.",
};

/**
 * Session is the identity half of the domain: whether anyone is signed in, and
 * who.
 *
 * There is no client-side auth library and no token in JavaScript. Signing in
 * is a full-page redirect to the server's OAuth endpoints, and the session
 * lives in an HttpOnly cookie the browser attaches automatically — which is
 * also why nothing here needs to handle refresh.
 */
export class Session {
  private readonly _status = new Signal<AuthStatus>("loading");
  private readonly _user = new Signal<User | null>(null);
  private readonly _error = new Signal<string | null>(null);
  /** What the server says it supports. Unknown until init() has run. */
  private readonly _capabilities = new Signal<Capabilities | null>(null);

  get statusBroadcast() {
    return this._status.broadcast;
  }

  /** The signed-in player's profile, once loaded from the API. */
  get userBroadcast() {
    return this._user.broadcast;
  }

  get errorBroadcast() {
    return this._error.broadcast;
  }

  get status(): AuthStatus {
    return this._status.get();
  }

  get user(): User | null {
    return this._user.get();
  }

  get capabilitiesBroadcast() {
    return this._capabilities.broadcast;
  }

  /** Whether the server has Google OAuth credentials configured. */
  get googleEnabled(): boolean {
    return this._capabilities.get()?.googleEnabled ?? false;
  }

  /** Whether the server exposes the local development login. */
  get devLoginEnabled(): boolean {
    return this._capabilities.get()?.devLogin ?? false;
  }

  get configured(): boolean {
    return this.googleEnabled || this.devLoginEnabled;
  }

  setUser(user: User | null): void {
    this._user.set(user);
    this._status.set(user ? "signed_in" : "signed_out");
  }

  setSignedOut(): void {
    this._user.set(null);
    this._status.set("signed_out");
  }

  setError(message: string | null): void {
    this._error.set(message);
  }

  /**
   * Picks up an `?auth_error=` left by a failed callback and clears it from the
   * address bar, so a refresh does not re-show a stale message.
   */
  async init(): Promise<void> {
    this._capabilities.set(await fetchCapabilities());

    const params = new URLSearchParams(window.location.search);
    const code = params.get("auth_error");
    if (!code) return;

    this._error.set(AUTH_ERRORS[code] ?? "Sign-in failed. Please try again.");
    params.delete("auth_error");
    const query = params.toString();
    window.history.replaceState(
      {},
      "",
      window.location.pathname + (query ? `?${query}` : ""),
    );
  }

  /**
   * Full-page redirect into the server's OAuth flow.
   *
   * The current location travels along so an invite link still works when the
   * recipient is signed out — otherwise the round trip through Google would
   * drop them on the home screen with the invite code gone. The server only
   * honours same-origin paths.
   */
  signInWithGoogle(next: string = currentPath()): void {
    window.location.href = `/auth/google/start?next=${encodeURIComponent(next)}`;
  }

  /** Local development only; the route does not exist in production. */
  devLogin(name: string, next: string = currentPath()): void {
    window.location.href =
      `/auth/dev/login?name=${encodeURIComponent(name)}&next=${encodeURIComponent(next)}`;
  }

  async signOut(): Promise<void> {
    await fetch("/auth/logout", { method: "POST" });
    this.setSignedOut();
    // Reload rather than tearing down state by hand: the session cookie is
    // gone, so a clean boot is both simpler and more obviously correct.
    window.location.href = "/";
  }

  dispose(): void {
    this._status.dispose();
    this._user.dispose();
    this._error.dispose();
    this._capabilities.dispose();
  }
}
