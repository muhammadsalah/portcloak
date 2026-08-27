import { defineConfig } from "vite";

// The build output is embedded into the Go binary, so it has to be
// self-contained: relative asset paths, no code splitting, no external CDN.
export default defineConfig({
  base: "./",
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
