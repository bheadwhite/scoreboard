import { describe, expect, it, vi } from "vitest";

import { fetchDeployedBuild } from "./runtime_config.js";

/**
 * Every one of these is a case where the honest answer is "I do not know", and
 * every one of them has to read as "no update".
 *
 * The asymmetry is the point: a false negative delays the banner until the next
 * check, while a false positive tells somebody who is already up to date to
 * reload, which they would do, and which would change nothing, and which would
 * then happen again.
 */
function respondWith(body: unknown, init: { ok?: boolean; text?: string } = {}) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok: init.ok ?? true,
      json: async () => {
        if (init.text !== undefined) throw new SyntaxError("not JSON");
        return body;
      },
    })),
  );
}

describe("fetchDeployedBuild", () => {
  it("reports the build the server is serving", async () => {
    respondWith({ build: "2026-01-01T00:00:00.000Z" });
    await expect(fetchDeployedBuild()).resolves.toBe("2026-01-01T00:00:00.000Z");
  });

  it("asks the network rather than the cache", async () => {
    respondWith({ build: "x" });
    await fetchDeployedBuild();
    // A cached answer would be the build we are already running, so the update
    // would never be noticed at all.
    expect(fetch).toHaveBeenCalledWith("/version.json", { cache: "no-store" });
  });

  it("says nothing when the server has no such file", async () => {
    respondWith(null, { ok: false });
    await expect(fetchDeployedBuild()).resolves.toBe("");
  });

  // What the Vite dev server actually does: unknown paths get index.html.
  it("says nothing when the response is HTML rather than JSON", async () => {
    respondWith(null, { text: "<!doctype html>" });
    await expect(fetchDeployedBuild()).resolves.toBe("");
  });

  it("says nothing when the payload has no build in it", async () => {
    respondWith({ commit: "abc" });
    await expect(fetchDeployedBuild()).resolves.toBe("");
  });

  it("says nothing when build is not a string", async () => {
    respondWith({ build: 12345 });
    await expect(fetchDeployedBuild()).resolves.toBe("");
  });

  it("says nothing when the network is gone", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new TypeError("offline");
      }),
    );
    await expect(fetchDeployedBuild()).resolves.toBe("");
  });
});
