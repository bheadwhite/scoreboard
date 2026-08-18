/// <reference types="vitest/config" />
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

/**
 * Identifies this build, so a browser left open across a deploy can notice.
 *
 * A timestamp rather than the git commit: `.git` is in `.dockerignore`, so the
 * Cloud Build container has no repository to ask. It only ever has to differ
 * from the previous build, never to be meaningful.
 *
 * Evaluated once when this config loads, so every chunk of one build agrees.
 */
const BUILD_ID = new Date().toISOString();

/**
 * Writes the build id to a file the running app can fetch.
 *
 * The pair is the whole mechanism: `__BUILD_ID__` is frozen into the bundle at
 * build time, `version.json` is read from the server at runtime, and a
 * difference between them means the tab is running a build the server has
 * replaced. Nothing on the Go side needs to know any of this — the file is
 * served as an ordinary static asset out of dist/.
 *
 * Deliberately not hashed and not under /assets/, so it misses the immutable
 * cache header and keeps a stable URL across deploys.
 */
function versionFile(buildId: string): Plugin {
  return {
    name: "scoreboard-version-file",
    apply: "build",
    generateBundle() {
      this.emitFile({
        type: "asset",
        fileName: "version.json",
        source: JSON.stringify({ build: buildId }) + "\n",
      });
    },
  };
}

// In development the Go API runs on :8080 and Vite proxies to it, so the app is
// same-origin in dev exactly as it is in production (where the Go binary serves
// this build directly). CORS therefore never enters the picture.
export default defineConfig({
  plugins: [react(), versionFile(BUILD_ID)],
  define: {
    __BUILD_ID__: JSON.stringify(BUILD_ID),
  },
  server: {
    port: 3000,
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: true },
      // The OAuth round trip has to come back through Vite, or the redirect
      // URI would not match the origin the browser is on.
      "/auth": { target: "http://localhost:8080", changeOrigin: true },
      "/health": { target: "http://localhost:8080", changeOrigin: true },
      // Which sign-in options the local server actually supports.
      "/config.json": { target: "http://localhost:8080", changeOrigin: true },
      // The live change stream. Buffering must be off or events would be held
      // until the connection closed, which is the whole thing SSE exists to
      // avoid.
      "/events": {
        target: "http://localhost:8080",
        changeOrigin: true,
        ws: false,
        configure: (proxy) => {
          proxy.on("proxyRes", (proxyRes) => {
            proxyRes.headers["cache-control"] = "no-cache, no-transform";
          });
        },
      },
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test_setup.ts"],
    restoreMocks: true,
    clearMocks: true,
  },
});
