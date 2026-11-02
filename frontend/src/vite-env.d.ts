/// <reference types="vite/client" />

/**
 * Which build this bundle is, frozen in at build time by the `define` in
 * vite.config.ts. Compared against the server's version.json to notice that a
 * deploy has happened underneath an open tab.
 *
 * Undefined under vitest, which does not apply the define — see runtime_config.
 */
declare const __BUILD_ID__: string | undefined;
