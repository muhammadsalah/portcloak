<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# 02 — What surfaced

Things the build turned up that were not true, or not known, when it started.
Each is written to be recognised again from its symptom.

Faults that reach *working code* belong in [`../notes/`](../notes/README.md) with
a guard test. What is here is narrower: findings from reading, and claims in the
existing code or documentation that turned out to be wrong.

## Entry format

- **Found** — how it came to light, because that is often the reusable part.
- **What** — the actual mechanism.
- **Fixed** — the change that was made, naming it, so this can be read against a
  diff rather than taken on trust.
- **Guard** — the test that fails if it comes back, or an honest statement that
  there is none and why.

`build/ci/check-traceability.sh` checks the names in the Guard lines the same way
it checks the rollout matrix, so a renamed test shows up here as a broken
reference instead of quietly becoming fiction.

---

## S1 — `Bootstrap` would have created a file `--config` only meant to read

**Found.** Reading `Home.Bootstrap` while designing the flag.

**What.** `Bootstrap` writes `emptyConfigYAML` when `ConfigFile()` does not
exist, and it runs on every start. With `--config` implemented as a plain
override of `ConfigFile()`, a mistyped path would have been created rather than
reported, and PortCloak would have started with an empty configuration that
looked deliberate.

**Fixed.** `Home.Bootstrap` returns before the template block whenever
`ConfigPath` is set, with the reason at the return. `internal/cli/context.go`
also stats the path up front and refuses with the path in the message, so the
failure arrives before an engine is built rather than as an empty config later.

**Guard.** `TestHome_ConfigPathOverridesOnlyTheConfigFile`,
`TestCLI_ConfigFileThatIsNotThereIsRefusedRatherThanCreated`.

---

## S2 — `StartupSweep` is not safe for a second process

**Found.** Reading `Engine.StartupSweep` while designing the lock.

**What.** Two of the three things it does are destructive to *another* process:

- `JobStore.AdoptRunning()` rewrites **every** `running` or `queued` job to
  `interrupted`. Run from `pcloak` while the window has a capture in flight, it
  marks that live job interrupted underneath it.
- `inspect.SweepIndexes` and `SweepWorkDirs` take their `keep` set from
  `Engine.OpenSessionIDs()`, which is *this* process's map. A second process
  sweeping would delete the first's decrypted working directory and index file
  while a snapshot is open in it.

That is data loss, not interleaving, and it is worse than what `run.go`'s
SingleInstance comment described.

**Fixed.** The destructive body became unexported `startupSweep`, reachable only
through `SweepIfSolelyHere`, which hands it to `config.Sweep` — and that takes
the exclusive claim, runs it, and releases. The guard is structural rather than a
boolean somebody has to remember to test: there is no arrangement of calls that
performs the sweep without having first proved nobody else is here.
`NewEngineAt` still does not call it, and both front ends call it *before* taking
their own shared claim, for the reason in S7.

**Guard.** `TestSweepIfSolelyHere_LeavesAnotherPortCloaksJobsAlone`.

---

## S3 — `controllers.go` claimed nothing implements `ServiceName`; seven do

**Found.** Reading the file while moving it into `internal/desktop`.

**What.** The comment read *"None of these implement Wails' ServiceName
interface, deliberately."* Seven of the nine controllers do — `audit`,
`inspect`, `jobs`, `keys`, `restore`, `settings`, `snapshot`. Only `config` and
`capture` do not.

**Whether it matters.** Checked against Wails v3.0.0-beta.15 rather than assumed:
`ServiceName` reaches only `getServiceName`, which is used for log lines and for
the message wrapping a service-startup error. It never touches how a bound method
is addressed. So the comment's *reasoning* was right and only its *fact* was
wrong; the two controllers without it are logged under their type name instead.

**Fixed.** The comment, not the code. It now states the count, says the
inconsistency is untidy rather than wrong, and says plainly that nobody should
read `ServiceName` as naming the call address — which is the misreading that once
had the frontend calling every method on every screen by a name that could not
resolve. The seven `// ServiceName is what the Wails binding layer calls this.`
comments were the other half of the same trap and now say the same thing.

Whether to add the method to the remaining two or drop it from the seven is left
open: it changes a log label and nothing else, and picking one silently inside a
refactor is how a decision nobody made becomes permanent.

**Guard.** None, and none is possible for a comment. The behaviour it describes
is guarded by `TestBindings_EveryFrontendCallResolves`, which resolves every call
the frontend makes against the real controllers.

---

## S4 — Widening the `Relocate` guard made its message wrong, and then its guard dead

**Found.** The wrong message during the comment pass over the split. The dead
guard a day later, writing the test for it — which is the point of writing one.

**What.** Two faults, one behind the other.

`Relocate` refused when `loc.Source == config.HomePinned`, with the message
*"Unset PORTCLOAK_HOME and restart PortCloak."* Widening the test to
`loc.Source.Pinned()` — so a `--home` folder is also refused — left that advice
attached to a case it does not help: telling somebody who passed `--home` to
unset an environment variable they never set.

Splitting the message by source fixed the wording and left the real fault
untouched, and it took a test to see it: `loc` comes from `config.Locate()`,
which by construction answers for a caller with nothing to say about the folder
and **can never return `HomeFlag`**. The whole `--home` branch was unreachable.
A folder named on the command line was relocated anyway, and the pointer file was
written for it — which is precisely the postcondition UC-L2 promises will not
happen.

**Fixed.** `Relocate` reads `e.HomeSource()` — what the engine was actually built
with — instead of re-deriving it from `Locate()`. The comment at the call site
records why, because the wrong version looks entirely reasonable.

**Guard.** `TestRelocate_RefusesAFlaggedFolderInItsOwnWords`, which asserts both
halves: that the flagged folder is refused at all, and that the refusal does not
blame an environment variable nobody set.

**Reusable part.** A refusal whose branch cannot be reached is a refusal that
does not exist. This one was written, reviewed, commented and wrong, and the only
thing that caught it was a test that tried to take the branch.

---

## S5 — `--home` was reported as "default" while pointing somewhere else

**Found.** Running `pcloak --home /tmp/scratch config path` for the first time.

**What.** It printed the scratch folder as `folder`, and `source default` beside
it. `SettingsController.location` derived the source by calling
`config.Locate()`, which by construction answers for a caller with nothing to say
about the folder — and a caller that passed `--home` has something to say. So the
panel mislabelled the folder *and* would have offered to move it, having already
been told it could not be moved. Same root cause as S4, found from the other end.

**Fixed.** `Engine` records `homeSource` at construction —
`NewEngineFor(loc, version)` takes the resolved `config.Location` — and exposes
it as `HomeSource()`. `SettingsController.location` reads that instead of
`Locate()`; the default path and the pointer path still come from `Locate`,
because those are genuinely its to know. `rebind` keeps it truthful across a
relocation, and the honest value follows the *pointer file* rather than the act:
moving back to `~/.portcloak` clears the pointer, so the folder is the default
again rather than a choice that happens to match it.

**Guard.** `TestCLI_HomeFlagIsReportedAsAFlagNotAsTheDefault`.

---

## S6 — The engine's skip signal is not on the event stream

**Found.** Writing the terminal progress renderer.

**What.** A phase can be both completed and skipped — it reached its turn and
abstained — and commit `475e4db` fixed the desktop app drawing that as a pass.
Reproducing the fix in a terminal meant finding the signal, and there isn't one:
`obs` has no skipped event kind. `config.Job.SkipPhase` records it on the job.

Worse, the message is not a reliable proxy. Capture's verification skip says so
in words ("...were skipped. The export itself is unaffected."), but restore's
post-import validation calls `rep.CompletePhase` with an ordinary summary and
records the skip only on the job.

**Fixed.** Split by authority, which is the arrangement
`internal/app/logs.go` already makes between the event stream and the log store.
The live glyph in `internal/cli/sink.go` is a best-effort reading of the message
and is documented as one; the summary is authoritative and reads
`SkippedPhases` off the job record, rendered by `renderPhases` exactly as the
Activity screen renders it.

**Guard.** `TestRenderPhases_ASkippedPhaseIsNotDrawnAsPassed` covers the
authoritative path; `TestIsSkip_RecognisesTheEnginesOwnWording` pins the
heuristic to the engine's actual phrasing so a reworded message shows up as a
failure rather than as a silent tick.

**Not done.** Adding a skipped event kind to `obs` would make the live render
exact, at the cost of touching every reporter call for a presentation concern. It
is worth doing if a third front end ever needs it.

---

## S7 — The obvious lock tiering was the wrong one, and a test caught it

**Found.** `TestCLI_ReadOnlyCommandsWorkWhileAWriterHoldsTheHome` failed on every
command, including `snapshot list`.

**What.** The first design split the lock into *reads* and *writes*: the window
held **exclusive** for its whole session, and read-only commands took **shared**.
Shared and exclusive conflict, so a window open all afternoon refused every
terminal command for the whole afternoon — the exact opposite of the stated
benefit, which is watching a capture from a terminal while the window runs it.

**The mistake underneath.** "Writes" is not the hazard. Two PortCloaks capturing
at once are fine: each writes its own job record and its own staging directory,
and one snapshot holds one realm, so there is nothing shared to corrupt. The
hazards are narrower, and there are two:

- the startup sweep (S2);
- a read-modify-write of `config.yaml`, where the second writer's copy of the
  file predates the first writer's change and silently drops it.

**Fixed.** **Shared** now means "a PortCloak is here" and is held for the whole
lifetime by everything. **Exclusive** means "no other PortCloak is here", is
taken only for those two operations, and is released. `desktop.Run` takes shared
for the session; capture, restore, job control and every read take shared; only
`key generate/import/remember/rename/delete`, `env probe` and `storage test` take
exclusive — the probes because they are not read-only, they record what they
found on the definition.

**A second, sharper trap, fixed with it.** `flock` conflicts are between *open
file descriptions*, not between processes. A process already holding the folder
shared cannot then take it exclusively on a second handle: it finds itself in the
way. So the sweep runs **before** a process takes its shared claim, in both
`desktop.Run` and `internal/cli/context.go`, and the ordering is stated at both.

**Guards.** `TestCLI_ReadOnlyCommandsWorkWhileAWriterHoldsTheHome`,
`TestCLI_CaptureRunsAlongsideAnotherPortCloak`,
`TestCLI_RefusesAConfigChangeWhileAnotherPortCloakHoldsTheHome`,
`TestHomeLock_AllowsSeveralReaders`, `TestHomeLock_RefusesASecondWriter`,
`TestHomeLock_IsReleasedWhenTheProcessDies`.

---

## S8 — Wrapping an engine error with `%s` lost its advice

**Found.** The busy-folder refusal stopped saying what still works, and the test
asserting the advice line caught it.

**What.** `fmt.Errorf("%w: %s", errHomeBusy, err.Error())` keeps the sentinel in
the chain and flattens the engine error to text. `resil.Error` carries its advice
in a field, not in its message, so `resil.Hint` had nothing left to find.

**Fixed.** `errors.Join(errHomeBusy, err)` in `internal/cli/context.go`, which
keeps both — the sentinel for `errors.Is`, and the engine error so its sentence
*and* its advice survive. `report` in `internal/cli/cli.go` prints the advice as
a second indented line.

**Guard.** `TestHomeLock_NamesTheHolder` asserts the advice on the engine error;
`TestCLI_RefusesAConfigChangeWhileAnotherPortCloakHoldsTheHome` asserts it
survives the wrap and reaches the terminal.

**Reusable part.** An engine error rendered by anything other than `app.Fail` is
an engine error whose next step has been thrown away.

---

## S9 — Two of these notes cited tests that did not exist

**Found.** Being asked whether the notes recorded what was *done* about each
finding, and auditing the file to answer.

**What.** S2 cited a test named for the sweep being "refused without the
exclusive lock", which had been renamed when the guard became structural. `01-decisions.md` D11 quoted an
invented test name as part of the story of the traceability check catching it —
harmless as prose, indistinguishable from a citation to a grep.

Both are the exact failure `check-traceability.sh` exists to prevent, in a folder
it did not read.

**Fixed.** The script now checks `spec/cli/*.md` alongside the rollout matrix. The
stale citation was corrected, and the deliberately-nonexistent name in D11 is no
longer written as code so it cannot read as a citation. This file also gained the
entry format above, which makes a missing Guard line visible rather than something
to notice.

**Guard.** `build/ci/check-traceability.sh` itself, in CI on every push.

**Reusable part.** A document that cites tests needs the check pointed at it on
the day it is written, not the day it rots. The cost was one line of shell.

---

## S10 — The scope argument had a step missing, and nobody noticed until asked

**Found.** Being asked "how to add env / storage using cli?" and having to answer
that you cannot.

**What.** The reasoning that excluded them held that a headless machine is not
blocked by them. It is: provisioning a throwaway Keycloak in CI means pointing
PortCloak at it before anything can be captured. The argument was applied one
step too late in the sequence, and read fine at every review because the sentence
is true of the *capture* and false of the workflow the capture is part of.

The near-miss is the reusable part. The first answer drafted was an accurate
description of how to hand-edit `config.yaml` and derive credential-handle names
— a real, supported path presented as though it were the interface. A workaround
described confidently enough stops looking like a gap.

**Fixed.** [D12](./01-decisions.md) — `env add`/`remove`, `storage
add`/`remove`/`default`. Also the three places that told an operator to go and
use the app when the listing was empty, which had become bad advice the moment
the commands existed.

**Guard.** `TestCLI_NotFoundOnlySuggestsCommandsThatExist` pins the narrower
version of that mistake — advice naming a command that does not exist — for the
kinds that genuinely have no `add`.

**Reusable part.** "X is not what stands between the user and the goal" is a
claim about a *sequence*, and it is only checkable by walking the sequence. Asked
of the capture alone it was true; asked of the job the capture belongs to it was
false.

---

## S11 — `ExitUsage` was defined, documented, and never returned

**Found.** Adding mutually-exclusive flag groups and checking what a violation
exits with. It was 1.

**What.** `ExitUsage = 2` had existed since the CLI was written, was in the table
in UC-L12, and nothing returned it. Every cobra rejection — unknown flag, wrong
argument count, missing required flag — reached `report()`, which classifies with
`codeFor` and falls through to `ExitFailed`.

So a script that retried on failure would have retried a typo, for ever, and the
documented contract said it would not.

**Fixed.** Every `RunE` in the tree is wrapped once at construction with a marker;
if `Execute` returns an error and the marker was never set, nothing got past
cobra's validation and the code is 2. Structural rather than string-matching
cobra's messages, which are not an interface — see
[D14](./01-decisions.md).

**Guard.** `TestCLI_UsageErrorsExitTwo`, over four different kinds of usage error.

**Reusable part.** A constant that is exported, documented in a table and never
returned looks exactly like one that works. The exit codes were tested — but only
the ones something returned, so the test suite agreed with the bug. What would
have caught it is a test per row of the documented table, written from the table
rather than from the code.
