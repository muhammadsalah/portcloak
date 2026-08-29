// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Rendering a component the way the application renders it.
 *
 * A screen is never mounted bare: main.tsx puts a theme, a modal host and the
 * shell's navigation around every one of them, and a component that reads a
 * token or opens a modal throws without them. Tests that assembled that stack
 * by hand would each be asserting on a slightly different application, so it is
 * assembled here once.
 *
 * The progress provider is deliberately not included. It opens the engine's
 * event stream, and a test that wants progress events wants to send them
 * itself — see `renderWithProgress` below.
 */
import type { ReactElement, ReactNode } from "react";
import { ThemeProvider } from "styled-components";
import { render, type RenderResult } from "@testing-library/react";

import { ModalProvider, theme } from "@/design-system";

function Providers({ children }: { children: ReactNode }) {
  return (
    <ThemeProvider theme={theme}>
      <ModalProvider>{children}</ModalProvider>
    </ThemeProvider>
  );
}

export function renderApp(ui: ReactElement): RenderResult {
  return render(ui, { wrapper: Providers });
}
