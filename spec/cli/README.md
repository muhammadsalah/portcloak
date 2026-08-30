<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# `pcloak` — the command line

Working notes from building PortCloak's second front end: what was decided, what
turned out not to be true, and what had to change underneath to make a terminal
a first-class caller of the same engine the window drives.

This folder is **not** the specification. The behavioural model lives in
[`../usecases/08-cli.md`](../usecases/08-cli.md) and the mechanism in
[`../13-command-line.md`](../13-command-line.md). What is here is the record of
*how it went* — the kind of thing that is obvious while writing the code and
gone a month later.

| # | Note | Covers |
|---|------|--------|
| 01 | [`01-decisions.md`](./01-decisions.md) | Every decision taken while building, with what it cost and what was rejected |
| 02 | [`02-surfaced.md`](./02-surfaced.md) | Things the build turned up that were not true, or not known, when it started |

## Why a second front end at all

The engine was already headless — `internal/engine/**` never imported Wails, and
`internal/app.NewEngine` needed no desktop runtime. But the only way to reach any
of it was to click through a window, which ruled PortCloak out of exactly the
places a realm migration happens: a CI job seeding a test realm, a maintenance
window driven from a runbook, a jump box with no display.

The whole of this work is therefore *plumbing an existing capability to a second
mouth*, not writing a second implementation. Where that rule was broken it is
recorded in [`01-decisions.md`](./01-decisions.md) with the reason.
