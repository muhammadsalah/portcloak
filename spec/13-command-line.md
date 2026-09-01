<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# 13 — The command line

> Two front ends over one engine, and the one folder they share.

`pcloak` is a second binary that drives the same engine the window drives, reading
the same `~/.portcloak`. It exists because the places a realm migration actually
happens — a CI job seeding a test realm, a maintenance window run from a runbook,
a jump box with no display — are frequently places a desktop application cannot
go (**NFR-12**).

Its behaviour is specified in [`usecases/08-cli.md`](./usecases/08-cli.md). This
document is the mechanism: how the two surfaces are kept from drifting, how they
share a folder without damaging each other, and what a script can rely on.

---

## 13.1 The three-way split

```
cmd/portcloak  →  internal/desktop  ─┐
                                     ├→  internal/app  →  internal/engine/…
cmd/pcloak     →  internal/cli     ──┘
```

`internal/app` is the composition root and the controller layer: it owns the
home folder, wires the engine's adapters into a registry, and turns engine
capabilities into methods a front end can call. **It imports no Wails.**

`internal/desktop` is the window, the menu, the event bridge and the service
registry — ~330 lines whose only job is to expose `internal/app`'s controllers to
the frontend. It is the one package allowed to import Wails.

The rule has teeth because Wails on Linux is cgo over GTK. An import of it
anywhere below `internal/desktop` would mean `pcloak` could not be built — or even
vetted — on a machine with no webview toolkit, which is most of the machines a
realm is actually captured from. Two things enforce it:

- **`TestHeadless_NoWailsImportBelowTheDesktopPackage`** parses every file under
  `cmd/pcloak`, `internal/app`, `internal/cli` and `internal/engine` and fails on
  a Wails import. It reads files *regardless of build constraints*, which is
  stricter than asking the toolchain: an import behind `//go:build linux`
  compiles fine on a developer's Mac and breaks exactly the build this protects.
- **The `headless` CI job** builds `./cmd/pcloak` for linux, darwin and windows
  with `CGO_ENABLED=0` on a runner with no toolkit installed at all.

## 13.2 The CLI calls controllers, not the orchestrator

This is the decision the whole surface rests on.

The controllers are not a thin pass-through. `CaptureController.Start` refuses
unless `Encrypt || AcknowledgedUnencrypted`. `RestoreController.Apply` supplies
the keychain candidates, because the orchestrator deliberately will not reach into
a keychain itself. A command line that called `Orchestrator.Capture` directly
would have silently dropped the unencrypted acknowledgement — a security-relevant
divergence between two front ends over one engine.

So `internal/cli` builds `app.NewCaptureController(eng)` and calls it. The
consequences are all in the same direction:

- The CLI adds **no new engine or app API**. Every gate, every operator-facing
  sentence and every list normalisation is shared, so the two surfaces cannot
  come to describe the same thing differently.
- `--json` prints the controller's own JSON-tagged structure. The machine-readable
  contract is the one the desktop frontend already depends on, and is already
  covered by the tests that keep those two in step.
- Errors reach the terminal as `app.Failure` — a redacted sentence, an advice
  line, and whether waiting would help — rather than as Go errors.

## 13.3 One folder, two processes

Until there was a command line, only one process could hold `~/.portcloak`: the
window refuses a second launch through Wails' SingleInstance, which raises the
existing window instead. `pcloak` is by definition a second process.

The obvious tiering — the window holds the folder, readers queue — is the wrong
one, and the reason is worth stating because it is not obvious. **Two PortCloaks
capturing at once are fine.** Each writes its own job record and its own staging
directory, and one snapshot holds one realm, so there is nothing shared to
corrupt. What is genuinely unsafe between processes is narrower, and there are
exactly two of them:

1. **The startup sweep.** `JobStore.AdoptRunning` rewrites *every* `running` job
   to `interrupted`, and the index and work-directory sweeps keep only the
   sessions *this* process knows are open. Run beside a live capture it would mark
   that capture interrupted and delete the staging directory it is still writing
   into. That is data loss, not interleaving.
2. **A read-modify-write of `config.yaml`.** Both writers read the file before
   either writes, so the second silently drops the first one's change.

Hence:

| Claim | Meaning | Who holds it |
|-------|---------|--------------|
| **Shared** | A PortCloak is here | Everything, for its whole lifetime — the window for its session, a command for its run |
| **Exclusive** | No other PortCloak is here | The startup sweep, and a change to `config.yaml`. Taken briefly, released immediately |

The claim is an **OS advisory lock** on `~/.portcloak/portcloak.lock`, not a
pidfile. The property that decides it: the kernel drops the lock when the process
dies, however it dies. A pidfile survives a crash, wedges every later run, and the
only cure is telling an operator to delete a file by hand — which teaches them to
delete it whenever anything looks stuck.

Two consequences that are easy to get wrong:

- **The sweep must run before a process takes its shared claim.** Advisory locks
  conflict between *open file descriptions*, not between processes, so a process
  already holding the folder shared cannot take it exclusively on a second
  handle: it finds itself in the way.
- **SingleInstance stays, and is now honestly labelled.** It is the courtesy of
  raising a window rather than printing an error; it sees only other copies of the
  desktop binary. The guarantee is the lock.

A refused exclusive claim names the holder — program, subcommand, pid, since when
— says why two would be a problem, and says what still works. Its exit code is its
own, so a wrapper can retry on exactly that and nothing else.

## 13.4 Waiting on a detached run

`Capture` and `Restore` both save their jobs, start a goroutine under
`context.WithoutCancel`, and return the ids immediately. That suits a window,
which has an event loop to go back to; a command has nothing to go back to.

It blocks on two things at once, and the arrangement is the one
`internal/app/logs.go` already makes between the log store and the live event
stream: **the job records are the authority and the event stream is the
immediacy.** Sink-only would hang on an event that is never emitted; polling-only
would be up to a tick stale, which in a terminal reads as a process that has died.

The same split decides how a **skipped phase** is drawn. A phase can be both
completed and skipped — it reached its turn and abstained — and `obs` carries no
skipped event kind; only `config.Job.SkippedPhases` does. So the live mark is a
best-effort reading of the phase's message, and the **summary is authoritative**.
Verification that did not run must never read as verification that passed.

## 13.5 Cancelling

A capture may be holding an ephemeral clone in somebody else's cluster with the
production database credentials in it. Cancelling the command's own context would
do nothing at all — the run detached and is not listening to it — so SIGINT and
SIGTERM both cancel *through the orchestrator*, which runs the job's teardown
(UC-C11 A2), and then keep waiting for it to finish.

A second signal, or a grace period, prints every clone reference still
outstanding with its environment before exiting. That is the only path on which an
operator who must kill the process is told what was left behind, and it is the one
thing that must not be silent.

## 13.6 Secrets, and the machines with no keychain

Nothing secret is ever a command-line argument, because `ps` shows argv to every
user on the machine. A passphrase comes from a file, from stdin, from
`PORTCLOAK_PASSPHRASE`, or from a no-echo prompt — twice with a comparison when
sealing, because a snapshot sealed with a typo cannot be opened by anybody, ever.

`--key <name>` is the best of these and the reason key management is in scope at
all (UC-L8): an identity key contributes its recipient, which is public, and a
passphrase key contributes its secret from the keychain. Neither appears anywhere
a process listing can see.

Where the keychain itself is out of reach — a CI runner with no secret service, or
a macOS binary whose code signature is not the one that wrote the entries —
`--no-keychain` keeps it out of the run entirely and `PORTCLOAK_CREDENTIAL_*`
supplies values that are never written anywhere. Creating a key is refused in that
mode: the secret would go nowhere and `config.yaml` would be left naming a handle
with nothing behind it, which looks like a working key until the day it is needed.

## 13.7 What a script can rely on

Results go to **stdout** and everything else to **stderr**, without exception, so
a run can be piped while it is still narrating. Every prompt has a flag that
satisfies it, and under `--json` or with no terminal an unmet prompt becomes a
refusal naming that flag rather than a wait.

The exit codes are in [UC-L12](./usecases/08-cli.md#uc-l12--script-it). Three of
them exist because a script genuinely branches on them: **partial** (some realms
produced a snapshot and some did not), **precondition** (nothing was written to
the target), and **busy** (another PortCloak holds the folder).

## 13.8 The scope line, and where it moved to

The first version of this excluded defining environments and storage, on the
reasoning that they are forms with a dozen fields and a connection test behind
them, and that unlike a key they are not what stands between a headless machine
and a sealed snapshot.

That was wrong in one direction, and the direction matters. A CI job that
provisions a throwaway Keycloak has to point PortCloak at it *before* it can
capture anything — so those definitions are exactly what stands between a
headless machine and a sealed snapshot, just one step earlier than the argument
looked. Hand-editing `config.yaml` is a supported thing to do, deliberately, but
"assemble a YAML fragment and know the credential-handle naming convention" is a
workaround, and describing it as an interface was the mistake.

The connection-test half of the argument did not survive either: `env probe` and
`storage test` are already commands, so the check a form runs is a check a script
can run.

`env add` and `storage add` are one subcommand per kind, because the four kinds
share almost no fields and a single command would carry twenty-five flags of
which twenty are wrong for whatever you are doing. They contact nothing —
probing is a separate, explicit act — and take `--replace` so a script can be
re-run without either failing or silently overwriting.

What is still out is **preferences**, and the test is the same one, applied
honestly: nothing is blocked on them, because every preference is a default that
a flag already overrides.

## 13.9 What the command line does not do

It does not edit preferences, because nothing is blocked on one: every preference
is a default that a flag on the relevant command already overrides. It never
writes the pointer file, so a run against a scratch tree cannot redirect the next
one. It
never prompts for a keychain password and never caches a credential to disk. And
it is not a daemon: there is no `--detach`, because the engine's detach is an
in-process goroutine and a command that returned early would take the export down
with it.
