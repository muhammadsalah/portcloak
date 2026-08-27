/**
 * Design tokens taken from the Keycloak admin console, so PortCloak looks like
 * it belongs beside it. The full reference is the `00 · Design tokens` board in
 * spec/lunacy/design.sketch.
 *
 * These are the same values the hand-written stylesheet carried as CSS custom
 * properties. They are a TypeScript object now so a component can reach a token
 * by name and be told at compile time when it reaches for one that does not
 * exist — `theme.color.danger` is checkable in a way `var(--dangr)` never was.
 *
 * GlobalStyle still publishes every one of them as a custom property, because
 * the shell needs them where no component is rendering: the scrollbars, the
 * selection colour, and anything a future stylesheet has to reach.
 */

export const theme = {
  color: {
    primary: "#0066cc",
    primaryHover: "#004d9f",
    primarySoft: "#f0f7fd",

    masthead: "#151515",
    nav: "#212427",
    navActive: "#3c3f42",
    navHover: "#2b2e33",
    navText: "#d2d2d2",

    page: "#f0f0f0",
    surface: "#ffffff",
    surfaceSubtle: "#fafafa",

    border: "#d2d2d2",
    borderInput: "#8a8d90",
    borderSubtle: "#ededed",

    text: "#151515",
    textSecondary: "#6a6e73",
    textMuted: "#8a8d90",

    success: "#3e8635",
    successBg: "#f3faf2",
    successBorder: "#bbe0b6",
    successText: "#1e4d16",

    danger: "#c9190b",
    dangerBg: "#faeaea",
    dangerBorder: "#f0a8a3",
    dangerText: "#7d1007",

    warning: "#f0ab00",
    warningBg: "#fdf7e7",
    warningBorder: "#f4d799",
    warningText: "#5c4100",
    warningBadgeText: "#795600",

    infoBg: "#e7f1fa",
    infoBorder: "#73bcf7",
    infoText: "#003b6f",
    infoBadgeText: "#004d9f",

    /*
     * Brand colours, which are not interface colours.
     *
     * The interface is the Keycloak admin console's palette, so PortCloak looks
     * like it belongs beside it. The mark is the one place these appear — see
     * assets/logo/README.md. Keeping them as separate tokens is what stops the
     * logo's teal leaking into buttons and badges that should stay PatternFly
     * blue.
     */
    brandInk: "#0e1a24",
    brandAccent: "#1fa896",

    logBg: "#1b1d21",
    logText: "#d2d2d2",
    logCmd: "#73bcf7",

    toggleOff: "#b8bbbe",
    rowHover: "#f5faff",
    rowSelected: "#f0f7fd",
    scrim: "rgba(3, 3, 3, 0.45)",
  },

  radius: {
    base: "3px",
    pill: "10px",
  },

  size: {
    mastheadHeight: "88px",
    navWidth: "260px",
    contentPadding: "24px",
  },

  font: {
    heading: '"Red Hat Display", "Helvetica Neue", Arial, sans-serif',
    body: '"Red Hat Text", "Helvetica Neue", Arial, sans-serif',
    mono: '"SF Mono", "JetBrains Mono", Menlo, Consolas, monospace',
  },

  z: {
    modal: 40,
  },
} as const;

export type Theme = typeof theme;

/** The five tones every status surface in the app is painted in. */
export type Tone = "ok" | "info" | "warn" | "danger" | "neutral";

/** The four a notice can take — a notice is never "neutral", it says something. */
export type NoticeTone = Extract<Tone, "ok" | "info" | "warn" | "danger">;
