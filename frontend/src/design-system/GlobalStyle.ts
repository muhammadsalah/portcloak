// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Everything that cannot belong to a component.
 *
 * The reset, the document's own type and colour, and the tokens republished as
 * CSS custom properties. Component styling lives with the component; this file
 * is deliberately short and should stay that way — a rule that lands here is a
 * rule no one can find by looking at the thing it paints.
 */
import { createGlobalStyle } from "styled-components";

export const GlobalStyle = createGlobalStyle`
  :root {
    --primary: ${(p) => p.theme.color.primary};
    --primary-hover: ${(p) => p.theme.color.primaryHover};
    --surface: ${(p) => p.theme.color.surface};
    --surface-subtle: ${(p) => p.theme.color.surfaceSubtle};
    --border: ${(p) => p.theme.color.border};
    --text: ${(p) => p.theme.color.text};
    --text-secondary: ${(p) => p.theme.color.textSecondary};
    --text-muted: ${(p) => p.theme.color.textMuted};
    --success: ${(p) => p.theme.color.success};
    --danger: ${(p) => p.theme.color.danger};
    --warning: ${(p) => p.theme.color.warning};
    --brand-ink: ${(p) => p.theme.color.brandInk};
    --brand-accent: ${(p) => p.theme.color.brandAccent};
  }

  * {
    box-sizing: border-box;
  }

  html,
  body {
    margin: 0;
    padding: 0;
    height: 100%;
  }

  body {
    font-family: ${(p) => p.theme.font.body};
    font-size: 14px;
    line-height: 1.5;
    color: ${(p) => p.theme.color.text};
    background: ${(p) => p.theme.color.page};
    -webkit-font-smoothing: antialiased;
  }

  h1, h2, h3, h4 {
    font-family: ${(p) => p.theme.font.heading};
    font-weight: 500;
    margin: 0;
  }

  /*
   * The application fills the window and never scrolls as a whole: the
   * navigation rail and the content column scroll independently, so the
   * masthead stays where the operating system's title bar is.
   */
  #app {
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
  }

  @keyframes portcloak-spin {
    to {
      transform: rotate(360deg);
    }
  }
`;
