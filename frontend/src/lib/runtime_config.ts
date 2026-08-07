export interface Capabilities {
  googleEnabled: boolean;
  devLogin: boolean;
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
