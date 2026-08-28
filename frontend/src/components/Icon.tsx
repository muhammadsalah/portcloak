// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The navigation icons.
 *
 * Line drawings on a 16px grid, stroked in `currentColor` so a nav item's
 * hover, active and disabled states carry the icon with them and no second set
 * of colour rules is needed. They are inline SVG rather than a font or a sprite
 * sheet: nine glyphs do not justify another asset in the bundle, and an icon
 * font that fails to load leaves squares where the navigation was.
 *
 * Each one draws the thing the screen acts on rather than an abstract symbol —
 * a tray with an arrow into it for capture, the same tray with the arrow coming
 * out for restore — so the pair reads as a pair at a glance.
 */
import type { ReactElement } from "react";

const glyphs: Record<string, ReactElement> = {
  // A tray with an arrow coming down into it.
  capture: (
    <>
      <path d="M8 1.75v6m0 0 2.25-2.25M8 7.75 5.75 5.5" />
      <path d="M2 9.25v3a1.5 1.5 0 0 0 1.5 1.5h9a1.5 1.5 0 0 0 1.5-1.5v-3" />
    </>
  ),
  // Stacked layers: one snapshot on top of the ones before it.
  library: (
    <>
      <path d="M8 1.75 14.25 5 8 8.25 1.75 5z" />
      <path d="m1.75 8.5 6.25 3.25 6.25-3.25" />
      <path d="m1.75 11.5 6.25 3.25 6.25-3.25" />
    </>
  ),
  // The capture tray, with the arrow going the other way.
  restore: (
    <>
      <path d="M8 7.75v-6m0 0L5.75 4M8 1.75 10.25 4" />
      <path d="M2 9.25v3a1.5 1.5 0 0 0 1.5 1.5h9a1.5 1.5 0 0 0 1.5-1.5v-3" />
    </>
  ),
  // A trace with something happening in it.
  activity: <path d="M1.25 8.5h3l1.75-4.75L9 12.25l1.75-3.75h4" />,
  // A bin: lid, body, and the two lines down it.
  trash: (
    <>
      <path d="M2.75 4.25h10.5" />
      <path d="M6.25 4.25V2.75h3.5v1.5" />
      <path d="M4.25 4.25 5 13.25h6l.75-9" />
      <path d="M6.75 6.75v4M9.25 6.75v4" />
    </>
  ),
  // A cross, for ending something rather than deleting it.
  close: <path d="M4 4l8 8M12 4l-8 8" />,
  // A clock, hands at ten past ten. Not a nav item — it labels the one fact on
  // a job card that is a moment rather than a thing.
  clock: (
    <>
      <circle cx="8" cy="8" r="6.25" />
      <path d="M8 4.5V8l2.5 1.75" />
    </>
  ),
  // Two rack units, each with its indicator lit.
  environments: (
    <>
      <rect x="1.75" y="2.25" width="12.5" height="4.5" rx="1.25" />
      <rect x="1.75" y="9.25" width="12.5" height="4.5" rx="1.25" />
      <path d="M4.25 4.5h.01M4.25 11.5h.01" />
    </>
  ),
  // The database cylinder, which is what every storage kind is underneath.
  storage: (
    <>
      <ellipse cx="8" cy="3.5" rx="5.75" ry="2" />
      <path d="M2.25 3.5v9c0 1.1 2.57 2 5.75 2s5.75-.9 5.75-2v-9" />
      <path d="M2.25 8c0 1.1 2.57 2 5.75 2s5.75-.9 5.75-2" />
    </>
  ),
  // A key, bit downwards.
  keys: (
    <>
      <circle cx="5.25" cy="10.75" r="3" />
      <path d="m7.4 8.6 6.35-6.35M11.25 4.75l1.75 1.75M9.4 6.6l1.75 1.75" />
    </>
  ),
  // A page with a record written on it.
  audit: (
    <>
      <path d="M3.25 1.75h6L12.75 5.25v9H3.25z" />
      <path d="M9.25 1.75v3.5h3.5" />
      <path d="M5.5 8.5h5M5.5 11h3.5" />
    </>
  ),
  // Sliders rather than a gear: at 16px a gear's teeth turn into a smudge.
  settings: (
    <>
      <path d="M1.75 4.5h6.5M11.75 4.5h2.5M1.75 11.5h2.5M7.75 11.5h6.5" />
      <circle cx="10" cy="4.5" r="1.75" />
      <circle cx="6" cy="11.5" r="1.75" />
    </>
  ),
};

/** The named glyph, or nothing at all if there is no such name. */
export function Icon({ name, className }: { name: string; className?: string }) {
  const glyph = glyphs[name];
  if (!glyph) return null;
  return (
    <svg
      className={className}
      viewBox="0 0 16 16"
      width="16"
      height="16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.3"
      strokeLinecap="round"
      strokeLinejoin="round"
      // Decorative: every icon sits beside the label it illustrates, so a
      // screen reader announcing it would read the item's name twice.
      aria-hidden="true"
    >
      {glyph}
    </svg>
  );
}
