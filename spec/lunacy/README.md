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

### Built

| # | Screen | Purpose | Covers |
|---|--------|---------|--------|
| 00 | Design tokens | Palette, type scale and control reference | — |
| 01 | Snapshots library | Snapshots across every storage backend, no key held | UC-I1 |
| 02 | Capture wizard | Environment → realms → storage → options → review | UC-C1…C7, UC-C10 |
| 03 | Inspector — Overview | Realm settings, key providers, completeness report | UC-I2, UC-I3 |
| 04 | Inspector — Users | Paged table, search, facets, user detail | UC-I4, UC-I5, UC-I6 |
| 05 | Restore — preconditions | External dependencies, informative only, never blocks | UC-R2, FR-D2 |
| 06 | Activity — jobs & resume | Running and interrupted jobs, throughput, retry state | UC-O1…UC-O4 |
| 07 | Environments (config) | List and detail, per-kind fields, probe results | UC-E5, UC-E9 |
| 08 | Storage (config) | List and detail, folder rooting, default flag | UC-S5, UC-S7 |
| 09 | Environment editor — Kubernetes | Kind tabs, kind-specific fields, keychain handle, full probe fact panel | UC-E1…E4, UC-E6…E8 |
| 10 | Storage editor — S3 | Kind tabs, endpoint/bucket/prefix, default and encryption-required toggles | UC-S1…S4, UC-S6 |

### Planned

| # | Screen | Purpose | Covers |
|---|--------|---------|--------|
| 11 | Storage browser | What a storage actually holds, unrecognised objects included | UC-S8 |
| 12 | Inspector — Clients, keys & federations | Every non-user entity type | UC-I7 |
| 13 | Inspector — Secret ledger & reveal | Secrets by location and kind; audited single reveal | UC-I8, UC-I9 |
| 14 | Inspector — Verify, export & close | Integrity check, redacted export, index destruction | UC-I11, UC-I12, UC-I13 |
| 15 | Restore — dry run & strategy | Diff against the live target; overwrite / skip / merge | UC-R3, UC-R4 |
| 16 | Restore — applying & result | Per-category progress, post-restore validation, cancel | UC-R1, UC-R5, UC-R6, UC-R8 |
| 17 | Job failure detail | What failed, why, whether it is retryable, what to do | UC-O5, UC-O6 |
| 18 | Settings & maintenance | Config file location, audit log, orphaned clones, purge | UC-O7, UC-O8, UC-O10, UC-C12 |

Screen 09 sits behind 07 and screen 10 behind 08 — they are the same route with the detail
pane focused, drawn separately so the per-kind field sets and the probe fact panel can be
reviewed at full size.

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
