export interface Capabilities {
  googleEnabled: boolean;
  devLogin: boolean;
}

/**
 * The build this bundle was compiled as, or "" when there is not one — under
 * vitest, where the define is not applied, and under `vite dev`, where it is
 * applied but there is no deployed build to compare it against.
 */
export const RUNNING_BUILD: string =
  typeof __BUILD_ID__ === "string" ? __BUILD_ID__ : "";

/**
 * Which build the server is serving right now.
 *
 * Fetched `no-store` on purpose. The point of this file is to be the one thing
 * that cannot be answered from a cache — a stale copy would report the build we
 * are already running and the update would never be offered.
 *
 * Every failure reports "" and is read as "no update": offline, a server too old
 * to have the file, or — the case that actually happens — the Vite dev server,
 * which answers unknown paths with index.html and so fails to parse as JSON. A
 * false negative delays the banner; a false positive would nag somebody who is
 * already up to date.
 */
export async function fetchDeployedBuild(): Promise<string> {
  try {
    const res = await fetch("/version.json", { cache: "no-store" });
    if (!res.ok) return "";
    const body = (await res.json()) as { build?: unknown };
    return typeof body.build === "string" ? body.build : "";
  } catch {
    return "";
  }
}

/**
 * Which sign-in options actually work, according to the server.
 *
 * This is fetched rather than compiled in or templated into index.html. Under
 * the Vite dev server the Go binary never touches index.html, so a templated
 * value is missing exactly when a developer is looking at it and the frontend
 * has to guess — which produced a sign-in screen offering a Google button
 * directly above an error saying Google was not configured.
 */
export async function fetchCapabilities(): Promise<Capabilities> {
  try {
    const res = await fetch("/config.json", { credentials: "same-origin" });
    if (!res.ok) throw new Error(String(res.status));
    const body = (await res.json()) as Partial<Capabilities>;
    return {
      googleEnabled: body.googleEnabled === true,
      devLogin: body.devLogin === true,
    };
  } catch {
    // Unreachable server: offer nothing rather than a button that cannot work.
    // The sign-in screen says so explicitly.
    return { googleEnabled: false, devLogin: false };
  }
}
