/**
 * The entry point: everything the app needs in scope, wrapped around the shell.
 *
 * Progress is outermost because the shell's job counters listen to it. The
 * modal host is innermost of the three so that anything below it — which is
 * every screen — can open one.
 */
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ThemeProvider } from "styled-components";

import { App } from "./app/App";
import { ProgressProvider } from "./app/ProgressContext";
import { ShellProvider } from "./app/ShellContext";
import { GlobalStyle, ModalProvider, theme } from "./design-system";

const root = document.getElementById("app");
if (!root) throw new Error("index.html has no #app for the application to mount into");

createRoot(root).render(
  <StrictMode>
    <ThemeProvider theme={theme}>
      <GlobalStyle />
      <ProgressProvider>
        <ShellProvider>
          <ModalProvider>
            <App />
          </ModalProvider>
        </ShellProvider>
      </ProgressProvider>
    </ThemeProvider>
  </StrictMode>,
);
