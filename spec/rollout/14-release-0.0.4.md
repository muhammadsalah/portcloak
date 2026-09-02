<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# Release — 0.0.4

**What 0.0.4 is.** The release where PortCloak stops needing a display. `pcloak`
is a second binary driving the same engine on the same `~/.portcloak`: capture,
the snapshot library and what is inside a snapshot, restore with its dry run, job
control, probes, keys, and — after the scope line moved once during the work —
environments and storage.

Nothing changes about what a snapshot carries or how one is sealed. A 0.0.3
bundle opens and restores under 0.0.4 unchanged, and a bundle written by `pcloak`
is the same artifact the app writes, because it is the same code writing it.

It exists because the places a realm migration actually happens are frequently
places a desktop application cannot go: a CI job seeding a test realm, a
maintenance window run from a runbook, a jump box with no display. The engine was
always headless — nothing under `internal/engine/` imports the UI toolkit — and
that was a property nothing could use.

The complete list is in [`CHANGELOG.md`](../../CHANGELOG.md#004--2026-09-02).
This page is the part worth reading before upgrading.

---

## The one thing to know before upgrading

**Two PortCloaks can now share one `~/.portcloak`, and the rules are not the
obvious ones.**

Before this, exactly one process could hold the folder, and nothing enforced it:
the desktop app refuses a second launch through Wails' SingleInstance, which sees
only other copies of itself. A second binary makes that assumption false, and the
consequence is worse than interleaving — the startup sweep rewrites every running
job to interrupted and deletes the working directories of snapshots it cannot see
are open. Run beside a live capture it would mark that capture interrupted and
delete the staging area it was still writing into.

The tiering is deliberately not "readers and writers":

| Claim | Meaning | Held by |
|-------|---------|---------|
| **Shared** | A PortCloak is here | Everything, for its whole lifetime |
| **Exclusive** | No other PortCloak is here | The startup sweep, and a change to `config.yaml`. Taken briefly, released immediately |

Two captures at once are safe: each writes its own job record and its own staging
directory, and one snapshot holds one realm, so there is nothing shared to
corrupt. What genuinely cannot be concurrent is narrower, and it is those two.

In practice: **the app can stay open** while you capture, restore, or watch a run
from a terminal. What it will refuse, for as long as the write takes, is
`env add`, `storage add`, `key generate`, `env probe` and `storage test` — all of
which record something in `config.yaml`. The refusal names who is holding the
folder and exits **6**, its own code, so a CI wrapper can retry on exactly that.

The claim is an OS advisory lock, not a pidfile: the kernel drops it when the
process dies, however it dies, so a crash cannot wedge the tool.

## What else changed underneath

**`internal/app` no longer imports Wails.** The window, the menu, the event
bridge and the service registry moved to a new `internal/desktop`; the
composition root and all nine controllers stayed. No behaviour changes in the
app. On Linux it means working on the engine, the controllers or the CLI no
longer needs GTK and WebKit headers installed.

**`pcloak` calls the same controllers the window calls.** Not the orchestrator
underneath them — the gate refusing an unacknowledged unencrypted capture and the
confirmation before overwriting a live realm live there, and going around them
would have put a security-relevant difference between two front ends over one
engine.

## What 0.0.4 does not do

- **`pcloak` is not inside the signed macOS `.app`.** It ships as its own archive
  per platform. On macOS, a keychain entry's ACL names the application that wrote
  it, so the first time `pcloak` reads a credential the app stored, the keychain
  asks — once per entry. Putting it inside the bundle would inherit the Developer
  ID signature and notarisation and fix this, but a nested Mach-O must be signed
  *before* the bundle signature or notarisation rejects it, and that failure
  appears only at Gatekeeper time on somebody else's machine. It belongs in a
  change that can be tested against a real notarisation run.
- **No release is signed by Apple or Microsoft.** Unchanged from 0.0.1. What is in
  place: `SHA256SUMS` signed with Sigstore and a build-provenance attestation, now
  covering the `pcloak` archives too, for free — the checksum step globs `dist/`.
- **A skipped phase is drawn live by a heuristic.** `obs` carries no skipped event
  kind, so the mark beside a phase as it completes reads the phase's message. The
  **summary is authoritative** and reads the job record, which is what the desktop
  app does. Verification that did not run never reads as verification that passed
  in either place, but the live mark is best-effort.
- **`pcloak` does not edit preferences.** Nothing is blocked on one: every
  preference is a default that a flag on the relevant command already overrides.
- **It is not a daemon, and there is no `--detach`.** The engine's detach is an
  in-process goroutine, so a command that returned early would take the export
  down with it and leave a clone running in your cluster. Background it with the
  shell.

## Where the work is recorded

- [13 — The command line](../13-command-line.md) — the mechanism: the three-way
  split, the shared folder, the controllers seam, the wait, the exit codes.
- [usecases/08 — Command line](../usecases/08-cli.md) — UC-L1…UC-L13.
- [cli/](../cli/README.md) — the decisions taken while building it, and the
  eleven things that surfaced doing it, including two bugs the work found in code
  that predated it.
