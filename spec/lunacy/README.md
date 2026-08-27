# Screen Designs (Lunacy)

The visual design lives in a Lunacy document following the Keycloak / PatternFly 4 idiom.
This file is the index: which screen exists, what it is for, and which use cases it covers.

> **The `.sketch` file in this folder is only current as of the last manual save in Lunacy.**
> Screens are authored live through the Lunacy MCP connection, which edits the open document
> in memory. Save from Lunacy (`⌘S`) before committing, or `design.sketch` will lag behind
> what this index describes.

## Design tokens

Taken from the Keycloak admin console so PortCloak looks like it belongs beside it. The
`00 · Design tokens` board in the document is the reference.

| Token | Value | Used for |
|-------|-------|----------|
| Primary | `#0066CC` | Primary buttons, active nav accent, links, selected tabs |
| Masthead | `#151515` | Top bar |
| Nav | `#212427`, active `#3C3F42` | Left navigation rail |
| Page | `#F0F0F0` | Page background |
| Surface | `#FFFFFF`, subtle `#FAFAFA` | Cards, list headers, form footers |
| Border | `#D2D2D2`, input `#8A8D90` | Card and field borders |
| Text | `#151515` primary, `#6A6E73` secondary, `#8A8D90` muted | |
| Success | `#3E8635` on `#F3FAF2` | Passed probes, verified integrity |
| Danger | `#C9190B` | Destructive actions, stale state, unencrypted warnings |
| Warning | `#F0AB00` | Degraded, retrying, out-of-scope notes |
| Radius | `3px` controls, `9–10px` pills | |
| Type | Red Hat Display (headings) · Red Hat Text (body) | |

Layout is uniform across every screen: a 76px masthead, a 260px navigation rail, and a
content column with 24px padding.

## Screens

| # | Screen | Purpose | Covers |
|---|--------|---------|--------|
| 00 | Design tokens | Palette, type scale and control reference | — |
| 01 | Snapshots library | Snapshots across every storage backend, no key held | UC-I1 |
| 02 | Capture wizard | Environment → realms → storage → options → review | UC-C1…UC-C5, UC-C6, UC-C7, UC-C10 |
| 03 | Inspector — Overview | Realm settings, key providers, completeness report | UC-I2, UC-I3 |
| 04 | Inspector — Users | Paged table, search, facets, single-user detail | UC-I4, UC-I5, UC-I6 |
| 05 | Restore — preconditions | External dependencies, informative only, never blocks | UC-R2 |
| 06 | Activity — jobs & resume | Running and interrupted jobs, throughput, retry state | UC-O1…UC-O4 |
| 07 | Environments (config) | List and detail, probe results, staleness | UC-E5, UC-E9 |
| 08 | Storage (config) | List and detail, folder rooting, default flag | UC-S5, UC-S7 |
| 09 | Environment editor — Kubernetes | Kind tabs, kind-specific fields, keychain handle, probe fact panel | UC-E1…UC-E4, UC-E6, UC-E7, UC-E8 |
| 10 | Storage editor — S3 | Kind tabs, endpoint/bucket/prefix, default and encryption-required toggles | UC-S1…UC-S4, UC-S6 |
| 11 | Storage browser | What a storage really holds, unrecognised objects included | UC-S8 |
| 12 | Inspector — Clients, keys & federations | Every non-user entity type; key providers with KIDs | UC-I7, UC-C8 |
| 13 | Inspector — Secret ledger & reveal | Secrets by location and kind; one revealed under audit, one masked at source | UC-I8, UC-I9, UC-C8 |
| 14 | Inspector — Verify, export & close | Per-artifact integrity, redacted export, index destruction | UC-I11, UC-I12, UC-I13 |
| 15 | Restore — dry run & strategy | Overwrite / skip / merge, diff computed for the selected strategy | UC-R3, UC-R4, UC-R7 |
| 16 | Restore — result & validation | What was applied, validation, authentication continuity, what the destination still needs | UC-R1, UC-R5, UC-R6, UC-C11 |
| 17 | Job outcome — failure & cancellation | Stage timeline, plain-language cause, retry ledger, checkpoint, partial restore | UC-O5, UC-O6, UC-R8 |
| 18 | Audit log & maintenance | Audit entries, config file, orphaned clones, working data and purge | UC-O7, UC-O8, UC-O10, UC-C12 |
| 19 | Inspector — External dependencies | Themes, provider JARs and keystores with the consequence of each being absent | UC-C9, UC-I10 |
| 20 | First run | Empty state naming the two things needed before a capture | UC-O9 |

All 60 use cases appear at least once. Three are covered as variants rather than as their own
screen, which is deliberate:

- **UC-R7 (restore into a freshly provisioned Keycloak)** is screen 15 against an empty target —
  every row reads *create*, nothing is overwritten. Drawing it separately would only show the
  same screen with different numbers.
- **UC-C8 (verify secrets are unmasked)** has no screen of its own because it is not a place you
  go; it is a fact that shows up where the secret does. Screens 12 and 13 both carry it, and
  screen 13 deliberately shows the failing case.
- **UC-C11 (destroy the ephemeral clone)** is a guarantee, not an interaction. It is asserted on
  screen 16 and shown as a completed stage on screen 17 — including on the failure path, which
  is the only place it matters.

Screen 09 sits behind 07 and screen 10 behind 08 — the same route with the detail pane focused,
drawn separately so the per-kind field sets and the probe fact panel can be reviewed at full size.

## A Lunacy authoring note

Two behaviours of this build cost time and are worth recording:

- **A `TEXT` layer ignores its `size` width** — it auto-sizes to its content, which silently
  collapses table columns. Wrap each cell in a `FRAME` with a fixed `size` and
  `autoLayout.fixWidth: true`, and let only the last column `stretchWidth`.
- **`create_layers` prepends** each top-level item it is given. Pass one root layer with its
  children nested (nested `layers` arrays keep their order), and when replacing siblings,
  create them in reverse.

## Two things the design has to get right

**A `Test` that reports facts, not a tick.** Screens 09 and 10 show the probe result as the
concrete things an operator needs — Keycloak version, `kc.sh` path, whether a clone can be
created and with how much quota headroom, free space — because "connected ✓" answers a
question nobody asked. The panel also states that nothing was written, since a probe against
production has to be visibly harmless.

**An unencrypted bundle that cannot be mistaken for a safe one.** Encryption is opt-in
([D8](../12-decisions.md)) and declining it is a respected choice, so the design does not
punish it — but a plaintext bundle carries unmasked client secrets and private signing keys,
and it is labelled as such in the library, the manifest and the completeness report. Screen 10
shows the counterpart control: a storage can be marked *encryption required*, which removes the
opt-out for anything written there.

## Related

- [09 — Workflows & Wails UI](../09-workflows-and-ui.md) — the information architecture these
  screens implement.
- [usecases/](../usecases/README.md) — the behavioural model each screen is drawn against.
- [rollout/](../rollout/README.md) — which phase builds each screen.
