// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The icons: the navigation rail's, and the ones on the buttons.
 *
 * Line drawings on a 16px grid, stroked in `currentColor` so a nav item's
 * hover, active and disabled states carry the icon with them and no second set
 * of colour rules is needed. They are inline SVG rather than a font or a sprite
 * sheet: a couple of dozen glyphs do not justify another asset in the bundle, and an icon
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
  snapshots: (
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
  // A plus: the one mark every interface uses for "another one of these".
  plus: <path d="M8 3.25v9.5M3.25 8h9.5" />,
  // A tick, for the action that commits what is on screen.
  check: <path d="m3 8.5 3.5 3.5L13 5" />,
  // An arrow back to where the reader came from, and its mirror onward.
  back: <path d="M13 8H3.25m0 0L7 4.25M3.25 8 7 11.75" />,
  next: <path d="M3 8h9.75m0 0L9 4.25M12.75 8 9 11.75" />,
  // A cycle: read it again rather than trust what is on screen.
  refresh: (
    <>
      <path d="M13.75 8a5.75 5.75 0 1 1-1.7-4.08" />
      <path d="M13.75 1.75v3.5h-3.5" />
    </>
  ),
  // A folder, for the places on disk an operator is asked to choose.
  folder: <path d="M1.75 12.5v-9h4.5l1.5 2h6.5v7a1 1 0 0 1-1 1h-10.5a1 1 0 0 1-1-1z" />,
  // The capture tray with the arrow coming in from outside: something arriving
  // that PortCloak did not make.
  importIn: (
    <>
      <path d="M8 1.75v6.5m0 0L5.5 5.75M8 8.25l2.5-2.5" />
      <path d="M2.25 10.5v2a1.5 1.5 0 0 0 1.5 1.5h8.5a1.5 1.5 0 0 0 1.5-1.5v-2" />
    </>
  ),
  // Two sheets, one behind the other.
  copy: (
    <>
      <path d="M5.75 5.75h7.5v7.5h-7.5z" />
      <path d="M10.25 5.75v-3h-7.5v7.5h3" />
    </>
  ),
  // An eye, for revealing what is deliberately hidden.
  eye: (
    <>
      <path d="M1.5 8s2.4-4.25 6.5-4.25S14.5 8 14.5 8s-2.4 4.25-6.5 4.25S1.5 8 1.5 8z" />
      <circle cx="8" cy="8" r="1.75" />
    </>
  ),
  // A shield with a tick: checked, and found to be what it claimed.
  verify: (
    <>
      <path d="M8 1.75 13.25 4v4c0 3-2.4 5.2-5.25 6.25C5.15 13.2 2.75 11 2.75 8V4z" />
      <path d="m5.75 7.75 1.75 1.75 3-3.25" />
    </>
  ),
  // A magnifier, for looking into something already made.
  inspect: (
    <>
      <circle cx="7" cy="7" r="4.25" />
      <path d="m10.25 10.25 3.5 3.5" />
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
