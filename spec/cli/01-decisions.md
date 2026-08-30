<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# 01 — Decisions

Each entry is what was decided, why, what it cost, and what was rejected. Order
is the order they were taken, which is roughly the order the code was written.

---

## D1 — The CLI is a second binding layer, not a second engine

**Decision.** `internal/cli` builds the same `app.Engine` the window builds and
calls the same controllers — `app.NewCaptureController(eng).Start(...)` — rather
than reaching past them to `eng.Orch.Capture(...)`.

**Why.** The controllers are not a thin pass-through. `CaptureController.Start`
refuses unless `Encrypt || AcknowledgedUnencrypted`; `RestoreController.Apply`
supplies `Candidates: eng.keyCandidates()` because the orchestrator deliberately
will not reach into a keychain itself. A CLI that called the orchestrator
directly would have silently dropped the unencrypted acknowledgement — a
security-relevant divergence between two front ends over one engine.

**Cost.** The CLI inherits shapes designed for a screen: `*app.Failure` instead
of `error`, `lists()` normalisation it does not need for human output.

**Rejected.** Calling the orchestrator directly and re-implementing the gates.
Two copies of a safety check is one copy of a safety check.

---

## D2 — `pcloak`, not `portcloak-cli`

**Decision.** Binary and root command are `pcloak`; the package directory
matches, at `cmd/pcloak`.

**Why.** It is typed several times a session. It also cannot collide with the
Linux desktop binary, which is `portcloak` — named in
`build/linux/portcloak.desktop` and in `application.LinuxOptions.ProgramName` —
so both can sit on one `PATH` with nothing to disambiguate. Matching the
directory to the binary means `go install ./cmd/pcloak` needs no `-o`.

---

## D3 — Split `internal/app` rather than duplicate its wiring

**Decision.** Move the six Wails-importing files, plus the two build-tag files
and the two tests that read them, into a new `internal/desktop`. `internal/app`
keeps `Engine` and all nine controllers and imports no Wails.

**Why.** Wails on Linux is cgo over GTK. Had `internal/app` kept its Wails
imports, `pcloak` would have needed a webview toolkit installed to capture a
realm over SSH. The alternative — the CLI wiring its own registry against
`internal/engine` — duplicates ~150 lines of `NewEngine` that would then drift.

**What it cost.** Less than expected: **nothing had to be exported.** The seam
was already exactly where the unexported methods stop, because `controllers()`
only ever called exported constructors on the exported `*Engine`.

**Guard.** `TestHeadless_NoWailsImportBelowTheDesktopPackage` parses every file
under `cmd/pcloak`, `internal/app`, `internal/cli` and `internal/engine` with
`go/parser` and fails on a Wails import. It reads files regardless of build
constraints, which is stricter than asking the toolchain — a Wails import behind
`//go:build linux` compiles fine on a developer's Mac and breaks precisely the
build the rule protects.

---

## D4 — `--home` is an argument, never `os.Setenv("PORTCLOAK_HOME", …)`

**Decision.** `config.LocateWith(override)` takes the folder as a parameter, and
a fourth `HomeSource`, `HomeFlag`, records where it came from. `Locate()` is
`LocateWith("")`.

**Why.** Setting the environment variable from inside a library mutates
process-global state that anything the process launches would inherit, and it
lies about provenance: `HomeSource` exists precisely so the Settings screen can
say whether a folder is movable, and a folder typed for one run is not the same
fact as one a shell exported.

**Ordering.** `--home` outranks `PORTCLOAK_HOME`. An argument typed for one
invocation is more specific than an environment inherited from a shell profile
or a CI image.

**Postcondition.** `LocateWith` never writes the pointer file. A run against a
scratch tree must not redirect the next one, or a CI job would silently move the
operator's real PortCloak. Guarded by
`TestLocateWith_LeavesThePointerFileAlone`.

---

## D5 — `--config` moves one file, and `Bootstrap` must not create it

**Decision.** `config.Home` gains a `ConfigPath` field overriding only
`ConfigFile()`. `Home.Bootstrap` returns before writing the empty-config
template whenever it is set.

**Why.** `Bootstrap` writes `emptyConfigYAML` when no config file exists. Left
ungated, `pcloak --config ./typo.yaml` would have *created* `./typo.yaml` and
started an empty PortCloak that looked like it had worked — a write, from a
surface whose whole scope statement is that it does not define configuration.

**Surfaced from.** Reading `Bootstrap` while designing the flag, not from a
failure. See [`02-surfaced.md`](./02-surfaced.md) S1.

---

## D6 — The CLI calls controllers, never the orchestrator

**Decision.** `internal/cli` builds `app.NewCaptureController(eng)` and calls it,
rather than reaching past it to `eng.Orch.Capture(...)`.

**Why.** Restated here from D1 because it is the decision everything else rests
on, and because the cost of getting it wrong is not obvious from the call site:
`CaptureController.Start` is where the unencrypted acknowledgement is enforced,
and `RestoreController.Apply` is where the keychain candidates are supplied. Both
would have been silently absent from the terminal.

**Consequence.** The CLI added **no new engine or app API** for capture, restore,
inspection, jobs, keys, environments or storage. The only additions anywhere
below it were `NewEngineAt`/`NewEngineFor`, `LocateWith`, `Home.ConfigPath`, the
home lock, and `config.NewFallback` — none of which is a capability, and all of
which the window benefits from too.

---

## D7 — There is no `--detach`

**Decision.** `capture` and `restore` block until the run finishes. The flag does
not exist.

**Why.** `Orchestrator.Capture` returns job ids immediately and runs the work in a
goroutine under `context.WithoutCancel`. That is an *in-process* detach, not a
daemon: if `main` returned, the goroutine would die mid-export, the job record
would be left saying `running`, and an ephemeral clone would be left in somebody's
cluster holding the production database credentials.

A `--detach` that did that would look like a feature and lose work. Backgrounding
is the shell's job — `&`, `nohup` — plus `pcloak job logs -f`.

**Cost.** A capture holds a terminal for as long as it takes. Accepted: the
alternative is a flag whose failure mode is somebody else's cluster.

---

## D8 — The exit codes a script branches on

**Decision.** 0 ok, 1 failed, 2 usage, **3 partial**, **4 precondition**,
5 retryable, **6 busy**, 130 cancelled.

**Why the three in bold.** Everything else is convention. These three exist
because a caller genuinely does something different:

- **partial** — a three-realm capture where one realm failed produced two real,
  individually restorable snapshots. One snapshot holds one realm, so there is no
  shared bundle to have corrupted. Reporting that as failure sends somebody
  looking for damage that is not there.
- **precondition** — nothing was written to the target. The fix is a flag or a
  configuration change, never a retry.
- **busy** — another PortCloak holds the folder. This is the one a CI wrapper
  should retry on, and the only one.

**Rejected.** A code per failure category. A script cannot act differently on
distinctions the tool cannot reliably make, and inventing them would invite it to
try.

---

## D9 — The command line ships as its own archive, not inside the app's

**Decision.** `pcloak-<version>-<os>-<arch>` per platform, built for all six from
whatever host runs `package.sh`. Not (yet) placed inside the `.app`, the Windows
zip or the Linux tarball.

**Why standalone.** The machines that want it most have no use for a download
carrying an embedded webview, and a standalone `linux/arm64` archive is the only
way to get one at all on a host with no Docker — the desktop binary needs a
container to cross-build, and the command line needs nothing.

**Why not also inside, yet.** Inside the `.app` it would inherit the bundle's
Developer ID signature and notarisation, which is the real answer to the macOS
keychain prompt an unsigned binary gets for entries the signed app wrote. But a
nested Mach-O has to be signed **before** the bundle signature or notarisation
rejects it — and that failure appears only at Gatekeeper time, on somebody else's
machine. It belongs in a change that can be tested against a real notarisation
run, not in this one.

**Free.** `SHA256SUMS` globs `dist/`, so the new artifacts are covered by the
checksums and by the Sigstore signature over them with no change at all.

---

## D10 — No release document was written

**Decision.** The work is recorded under `## [Unreleased]` in the changelog, in
`spec/13-command-line.md`, in `spec/usecases/08-cli.md` and here. No
`spec/rollout/14-release-0.0.4.md` was created.

**Why.** The other release documents in `spec/rollout/` describe releases that
happened: what was cut, what was signed, and the honest list of what that version
does not do. Writing one for a version that has not been cut would put a
description of a release into the folder whose whole value is that its contents
are true. It belongs in the commit that tags the release.

**What to do at release time.** Move the `[Unreleased]` section under its version
heading, and write `14-release-0.0.4.md` in the shape of
[`13-release-0.0.2.md`](../rollout/13-release-0.0.2.md) — including the two things
this work leaves open: `pcloak` is not yet inside the signed macOS bundle (D9),
and the live skipped-phase mark is a heuristic rather than an event kind
([02-surfaced.md](./02-surfaced.md) S6).

---

## D11 — The rollout matrix cites tests before the prose is written

**Decision.** `spec/rollout/12-rollout-traceability.md` was updated last, after
the tests existed.

**Why.** `build/ci/check-traceability.sh` fails the build when a cited test name
does not resolve, and it earned its keep immediately: the first draft of the
UC-L7 row cited a test named "Restore refuses a degraded snapshot", which sounded
exactly like a test this repository would have and does not. The check named it
in one line. A matrix whose evidence column is written from memory is a matrix
that becomes fiction, quietly.
