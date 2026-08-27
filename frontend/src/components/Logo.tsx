/**
 * The PortCloak mark.
 *
 * A padlock whose shackle is a restore arc — sealed, and put back. The
 * canonical files and the rules around them are in `assets/logo/`; this is the
 * same five shapes as JSX so the app can draw it at any size without a second
 * network fetch, and so the masthead's lockup is live text rather than a
 * picture of text.
 */

/** The three colour roles every variant of the mark is made of. */
export interface MarkTone {
  /** The restore arc and its arrowhead. */
  accent: string;
  /** The lock itself. */
  body: string;
  /** Cut out of the body, so always its opposite. */
  keyhole: string;
}

export const INK = "#0E1A24";
export const ACCENT = "#1FA896";

/** On a light background. */
export const onLight: MarkTone = { accent: ACCENT, body: INK, keyhole: "#FFFFFF" };

/** On a dark background — the masthead. */
export const onDark: MarkTone = { accent: ACCENT, body: "#FFFFFF", keyhole: INK };

/**
 * The mark at a given size.
 *
 * No outline and no gradient: it has to survive being 16px in a navigation
 * rail, which is the size it spends most of its life at.
 */
export function Mark({ size, tone }: { size: number; tone: MarkTone }) {
  return (
    <svg
      viewBox="0 0 120 120"
      width={size}
      height={size}
      fill="none"
      role="img"
      aria-label="PortCloak"
    >
      <path d="M36 56 A24 24 0 0 1 80.8 44" fill="none" stroke={tone.accent} strokeWidth="10" />
      <path d="M86.3 53.5 L73 48.5 L88.6 39.5 Z" fill={tone.accent} />
      <rect x="24" y="56" width="72" height="54" rx="12" fill={tone.body} />
      <circle cx="60" cy="76" r="8" fill={tone.keyhole} />
      <rect x="56" y="80" width="8" height="18" rx="4" fill={tone.keyhole} />
    </svg>
  );
}
