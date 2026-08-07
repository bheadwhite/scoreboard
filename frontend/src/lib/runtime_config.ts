interface RuntimeConfig {
  googleEnabled: boolean;
  devLogin: boolean;
}

declare global {
  interface Window {
    __SCOREBOARD_CONFIG__?: Partial<Record<keyof RuntimeConfig, unknown>>;
  }
}

/**
 * Reads a value the Go server substituted into index.html.
 *
 * Under the Vite dev server the file is served verbatim, so the placeholders
 * survive as literal `__NAME__` strings — that is the signal to fall back to a
 * development default rather than trust the value.
 */
function flag(value: unknown, fallback: boolean): boolean {
  if (typeof value === "boolean") return value;
  if (typeof value === "string") {
    if (value.startsWith("__") && value.endsWith("__")) return fallback;
    return value === "true";
  }
  return fallback;
}

const injected = typeof window !== "undefined" ? window.__SCOREBOARD_CONFIG__ : undefined;

/**
 * What the browser is allowed to know about the server's configuration: which
 * sign-in options to offer, and nothing else. There are no credentials here —
 * authentication happens entirely server-side behind an HttpOnly cookie.
 *
 * The dev-server fallbacks assume a local backend with both options wired up,
 * which is what `make dev-api` gives you.
 */
export const runtimeConfig: RuntimeConfig = {
  googleEnabled: flag(injected?.googleEnabled, true),
  devLogin: flag(injected?.devLogin, true),
};
