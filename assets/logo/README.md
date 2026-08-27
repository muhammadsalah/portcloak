# The PortCloak mark

A padlock whose shackle is a restore arc. It says the two things the tool is
for at once — a realm sealed, and a realm put back — which is why it was chosen
over the other two directions drawn beside it.

Source: [`spec/design/Port Cloak Logo.html`](../../spec/design/Port%20Cloak%20Logo.html),
turn 2, direction 1a "Restore lock". These files are that direction extracted
verbatim: same 120×120 grid, same path data, same colours.

## Files

| File | Use |
|---|---|
| `mark.svg` | Primary. Ink body on a light background. |
| `mark-reversed.svg` | On a dark background — the masthead, a dark card. |
| `mark-ink.svg` | One colour, ink. Print, stamps, anywhere the accent cannot go. |
| `mark-teal.svg` | One colour, accent. |
| `favicon.svg` | Carries its own rounded ground, because a browser tab is neither a light nor a dark background. |

## Geometry

Every file is the same five shapes on a `0 0 120 120` viewBox, differing only in
which of three colour roles each shape takes:

| Shape | Role |
|---|---|
| `M36 56 A24 24 0 0 1 80.8 44`, stroke width 10 | **accent** — the restore arc |
| `M86.3 53.5 L73 48.5 L88.6 39.5 Z` | **accent** — its arrowhead |
| `rect 24,56 · 72×54 · r12` | **body** — the lock |
| `circle 60,76 r8` + `rect 56,80 · 8×18 · r4` | **keyhole** — cut out of the body |

The keyhole is always the body's opposite. There is no outline and no gradient:
the mark has to survive being 16px in a navigation rail, which is the size it
spends most of its life at.

## Colour

| Token | Value | |
|---|---|---|
| Ink | `#0E1A24` | Body on light, keyhole on dark |
| Accent | `#1FA896` | The restore arc, and *Cloak* in the wordmark |
| Accent deep | `#157F72` | Links, hover |
| Paper | `#EEF1EF` | The ground the mark was drawn against |
| Border | `#DCE2E0` | |
| Muted | `#6B7A78` on light, `#8A9A97` on dark | |

## The lockup

**PortCloak** set as one word: *Port* in ink (white on dark), *Cloak* in accent.
Space Grotesk 700, letter-spacing `-0.03em`, line-height 1, with the mark to its
left at roughly the cap height of the text and a gap of about 0.6× the mark.

There is deliberately no lockup SVG here. Setting the wordmark as `<text>` makes
the file render differently on every machine that lacks the font, and outlining
it would freeze a shape that the app can set live and correctly. The lockup is
built where it is used — the app's masthead does it in the DOM, exactly as the
design source does.

## What not to do

Do not recolour the arc to the app's PatternFly blue, redraw the keyhole, add an
outline, or set *Cloak* in anything but the accent. The mark is the one place
these colours appear; the rest of the interface stays on the Keycloak admin
console palette it was built to sit beside.
