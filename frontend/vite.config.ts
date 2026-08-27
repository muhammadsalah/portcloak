import { defineConfig } from "vite";

// The build output is embedded into the Go binary, so it has to be
// self-contained: relative asset paths, no code splitting, no external CDN.
export default defineConfig({
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
        manualChunks: undefined,
        inlineDynamicImports: true,
      },
    },
  },
});
