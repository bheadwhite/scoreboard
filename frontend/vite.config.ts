/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// In development the Go API runs on :8080 and Vite proxies to it, so the app is
// same-origin in dev exactly as it is in production (where the Go binary serves
// this build directly). CORS therefore never enters the picture.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: true },
      // The OAuth round trip has to come back through Vite, or the redirect
      // URI would not match the origin the browser is on.
      "/auth": { target: "http://localhost:8080", changeOrigin: true },
      "/health": { target: "http://localhost:8080", changeOrigin: true },
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
