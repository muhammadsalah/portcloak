// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

import { writeFileSync } from "node:fs";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";

/**
 * Puts dist/.gitkeep back after every build.
 *
 * emptyOutDir clears dist/ on each run, which deletes the committed
 * placeholder that frontend/embed.go depends on: `//go:embed all:dist` fails
 * the Go build outright when the directory holds no files, so a developer who
 * ran `npm run build` and then `git status` would find the placeholder deleted
 * and, sooner or later, commit its removal — breaking `go build ./...` and
 * `go test ./internal/...` for anyone who has never run npm.
 *
 * Writing it back at closeBundle costs nothing and makes that impossible.
 */
function keepEmbedPlaceholder(): Plugin {
  return {
    name: "portcloak-keep-embed-placeholder",
    closeBundle() {
      writeFileSync(
        "dist/.gitkeep",
        "This file is the placeholder frontend/embed.go depends on; see that file.\n" +
          "Vite rewrites it after every build (see keepEmbedPlaceholder in vite.config.ts).\n",
      );
    },
  };
}

// The build output is embedded into the Go binary, so it has to be
// self-contained: relative asset paths, no code splitting, no external CDN.
export default defineConfig({
  plugins: [
    // The styled-components Babel transform is not cosmetic here: without it
    // every rule lands under a hashed class name, and the one thing a developer
    // does in front of this UI — open the inspector and find the component a
    // box came from — stops working. `displayName` puts the component's name in
    // the class; `pure` lets Rollup drop styles nothing renders.
    react({
      babel: {
        plugins: [["babel-plugin-styled-components", { displayName: true, pure: true }]],
      },
    }),
    keepEmbedPlaceholder(),
  ],
  base: "./",
  // The logo lives at the repository root, not under frontend/, because the
  // README and packaging need it too. Pointing Vite's public directory at it
  // keeps one copy: assets/logo/favicon.svg is served at /logo/favicon.svg in
  // dev and copied into dist — and therefore into the Go binary — for a build.
  publicDir: "../assets",
  // Only used by `npm run dev`, which the Go app proxies to when
  // FRONTEND_DEVSERVER_URL is set. Two things have to be pinned for that to
  // work at all:
  //
  //   host: Vite otherwise binds ::1 only, while the Wails asset proxy forces
  //   IPv4 for a "localhost" dev-server URL. The startup health check uses a
  //   plain http.Client and succeeds over IPv6, so the app reports "Connected
  //   to frontend dev server!" and then fails every asset request.
  //
  //   strictPort: on a taken port Vite silently moves to 5174, and the Go side
  //   goes on proxying to 5173. Failing to start is the useful outcome.
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    target: "es2022",
    rollupOptions: {
      output: {
        // One bundle. A desktop app loads from disk, so splitting buys nothing
        // and makes the embedded asset tree harder to reason about.
        //
        // This was `inlineDynamicImports: true` plus an explicitly undefined
        // `manualChunks`. Vite 8 bundles with Rolldown rather than Rollup and
        // deprecated the first in favour of this one flag, which says the same
        // thing plainly; the second was only ever restating the default.
        codeSplitting: false,
      },
    },
  },
});
