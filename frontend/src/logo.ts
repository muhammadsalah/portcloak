/**
 * The PortCloak mark.
 *
 * A padlock whose shackle is a restore arc — sealed, and put back. The
 * canonical files and the rules around them are in `assets/logo/`; this is the
 * same five shapes built in the DOM so the app can draw it at any size without
 * a second network fetch, and so the masthead's lockup is live text rather than
 * a picture of text.
 */

const NS = "http://www.w3.org/2000/svg";

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
 * Builds the mark at a given size.
 *
 * No outline and no gradient: it has to survive being 16px in a navigation
 * rail, which is the size it spends most of its life at.
 */
export function mark(size: number, tone: MarkTone): SVGSVGElement {
  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("viewBox", "0 0 120 120");
  svg.setAttribute("width", String(size));
  svg.setAttribute("height", String(size));
  svg.setAttribute("fill", "none");
  svg.setAttribute("role", "img");
  svg.setAttribute("aria-label", "PortCloak");
  svg.innerHTML =
    `<path d="M36 56 A24 24 0 0 1 80.8 44" fill="none" stroke="${tone.accent}" stroke-width="10"/>` +
    `<path d="M86.3 53.5 L73 48.5 L88.6 39.5 Z" fill="${tone.accent}"/>` +
    `<rect x="24" y="56" width="72" height="54" rx="12" fill="${tone.body}"/>` +
    `<circle cx="60" cy="76" r="8" fill="${tone.keyhole}"/>` +
    `<rect x="56" y="80" width="8" height="18" rx="4" fill="${tone.keyhole}"/>`;
  return svg;
}
