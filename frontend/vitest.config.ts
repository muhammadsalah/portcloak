// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

/**
 * The frontend test run, kept apart from vite.config.ts on purpose.
 *
 * The build config is about producing something self-contained enough to embed
 * in a Go binary — a public directory outside this folder, one bundle, no code
 * splitting. None of that has any bearing on a test, and a test run that
 * inherited it would silently depend on it.
 *
 * Vitest prefers this file over vite.config.ts when both exist, so the two
 * never have to agree about anything.
 */
export default defineConfig({
  // The same Babel transform the build uses, so a styled component carries its
  // displayName here too. A test that fails on `WizardStep` is a test whose
  // failure names the component; one that fails on `.sc-bdVaJa` is not.
  plugins: [
    react({
      babel: {
        plugins: [["babel-plugin-styled-components", { displayName: true, pure: true }]],
      },
    }),
  ],
  test: {
    environment: "jsdom",
    setupFiles: ["src/test/setup.ts"],
    // Nothing in this suite talks to a network, a clock it does not control, or
    // the Go engine. Anything slower than this is a test that is waiting for
    // something it should have faked.
    testTimeout: 5000,
    include: ["src/**/*.test.{ts,tsx}"],
    coverage: {
      provider: "v8",
      reporter: ["text-summary", "html", "lcov", "json-summary"],
      reportsDirectory: "coverage",
      // Every source file counts, not only the ones a test happened to import.
      // Without this an untested page simply does not appear, and the number
      // describes the tested subset rather than the frontend.
      all: true,
      include: ["src/**/*.{ts,tsx}"],
      exclude: [
        "src/**/*.test.{ts,tsx}",
        "src/test/**",
        // The entry point mounts React into the document and does nothing else.
        "src/main.tsx",
        // Styled-components tokens and declarations: values and types, with no
        // branch in them to cover. Counting them would inflate the number
        // without any of it meaning a line was ever exercised.
        "src/design-system/theme.ts",
        "src/design-system/styled.d.ts",
        "src/design-system/GlobalStyle.ts",
      ],
      // Two floors, because one number cannot say both things.
      //
      // The global figure is low and honestly so: spec/rollout/01 §1.9 says
      // the frontend holds no business logic and is to be tested narrowly, and
      // most of what is left is a form wired to the engine. Its job here is to
      // stop the suite quietly rotting — it is set just under what the suite
      // reaches, so it can only be argued upwards.
      //
      // The per-file figures are the metric that means something. Every module
      // below decides something: which step a wizard will let you leave, what
      // the progress bar says a stalled job is doing, whether a snapshot is
      // sealed. The floor under them is well above the global one, so a change
      // that stops covering them fails here rather than being averaged away by
      // the pages around it.
      thresholds: {
        statements: 22,
        branches: 14,
        functions: 17,
        lines: 22,
        ...Object.fromEntries(
          [
            "src/app/ProgressContext.tsx",
            "src/app/ViewBoundary.tsx",
            "src/app/routes.ts",
            "src/components/ProbePanel.tsx",
            "src/components/SnapshotKeyFields.tsx",
            "src/hooks/useAsync.ts",
            "src/pages/activity/live.ts",
            "src/pages/capture/draft.ts",
            "src/pages/environments/kinds.ts",
            "src/pages/inspector/tabs.ts",
            "src/pages/restore/wizard.ts",
            "src/pages/storage/kinds.ts",
            "src/utils/format.ts",
          ].map((file) => [
            `**/${file}`,
            { statements: 75, branches: 75, functions: 75, lines: 75 },
          ]),
        ),
      },
    },
  },
});
