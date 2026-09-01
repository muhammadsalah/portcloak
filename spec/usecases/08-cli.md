<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# 08 — Command line

> `pcloak` drives the same engine the window drives, reading the same
> `~/.portcloak`. It exists because the places a realm migration actually happens
> — a CI job seeding a test realm, a maintenance window run from a runbook, a
> jump box with no display — are places a desktop application cannot go.

The prefix is **`UC-L`**, for command *line*. `UC-C` is already Capture, and
`UC-CLI` would break the one-letter convention every other package here follows.

These use cases add no capability to PortCloak. Every one of them is an existing
capability reached from a terminal instead of a window, which is why they cover
**NFR-12** and, through it, the requirements the equivalent screen covers.

---

## UC-L1 — Run PortCloak from a terminal

**Goal.** Reach the engine on a machine with no display.
**Preconditions.** `pcloak` on the `PATH`. No account, no sign-in, no daemon (N8).
**Trigger.** Any `pcloak` command.

**Main success scenario**
1. Operator runs `pcloak <command>`.
2. PortCloak resolves the home folder (UC-L2), bootstraps it, and reports itself
   present on it (UC-L10).
3. The command calls the same controller the equivalent screen calls.
4. Results go to **stdout**; progress, notes and refusals go to **stderr**.
5. The process exits with a code that classifies the outcome (UC-L12).

**Alternate flows**
- **A1 — `--json`.** The result is the controller's own JSON-tagged structure,
  so the machine-readable contract is the one the desktop frontend already
  depends on rather than a second one that can drift.
- **A2 — Configuration will not load.** The problem is reported on stderr and the
  command continues where it can, exactly as the app opens its window and says
  which line is wrong.

**Exceptions**
- **E1 — The home folder cannot be written.** Reported with the path (UC-O9 E1).

**Postconditions.** Nothing that the equivalent screen would not also have done.
**Covers.** NFR-12, N8.

---

## UC-L2 — Point one run at a different folder or config file

**Goal.** Run against a scratch tree, or against definitions from a checkout.

**Main success scenario**
1. `--home <dir>` names the whole tree; `--config <file>` names only
   `config.yaml`, leaving jobs, logs, indexes and working files where they were.
2. The order is `--home` → `PORTCLOAK_HOME` → the pointer file → `~/.portcloak`.
   An argument typed for one invocation is more specific than an environment
   inherited from a shell profile or a CI image.
3. The folder's source is reported as `flag`, and it is **not movable**: there is
   nowhere to record a different choice that the outside setting would not
   immediately override.

**Exceptions**
- **E1 — `--config` names a file that is not there.** Refused. The file is read,
  never created: filling in the empty template would start a PortCloak with no
  environments in it and look deliberate.

**Postconditions.** **The pointer file is never written.** A run against a scratch
tree does not redirect the next one, and a CI job cannot silently move an
operator's real PortCloak.
**Covers.** NFR-11, NFR-12.

---

## UC-L3 — Capture a realm from the command line

**Goal.** Produce a snapshot without a window.
**Preconditions.** An environment and a storage exist (UC-E1…E4, UC-S1…S4).

**Main success scenario**
1. `pcloak capture --env <name> --realm <realm> [--storage <name>]`.
2. Encryption is on unless declined (UC-L11 for how it is keyed).
3. The run proceeds exactly as UC-C1…C4 for the environment's kind, including the
   ephemeral clone where one applies.
4. Progress is rendered as it happens (UC-L4).
5. The command **blocks until the run finishes** and then prints one line per
   realm with where each snapshot landed.

**Alternate flows**
- **A1 — Several realms.** `--realm` repeats. Each becomes its own job and its own
  snapshot (FR-S6), sharing one probe and one ephemeral clone (UC-C5).
- **A2 — `--all-realms`.** The realms discovered at probe time.

**Exceptions**
- **E1 — The Admin API cannot enumerate realms.** `--all-realms` is refused rather
  than expanded to a guess, and says to name them. An offline export reads the
  database directly and works against a Keycloak with no API at all, so this is
  an ordinary state and not a fault.
- **E2 — One realm of several fails.** The rest continue. The run reports which
  succeeded and exits **partial** (UC-C5 E1).
- **E3 — Encryption declined without acknowledgement.** Refused before any work
  (UC-L11 A1).

**Postconditions.** One snapshot per successfully captured realm.
**Covers.** FR-C1…FR-C5, FR-C8, FR-S6, NFR-12.

**Note.** There is **no `--detach`**. The orchestrator's detach is an in-process
goroutine, so a command that returned early would take the export down with it
and leave a clone running in somebody's cluster. Backgrounding is the shell's job.

---

## UC-L4 — Watch a run in a terminal

**Goal.** See what is happening, in a form suited to where it is being read.

**Main success scenario**
1. On a **terminal**, each phase is drawn with a mark as it completes, with
   progress and retries shown live.
2. Anywhere else — a CI log, a pipe, a file — output is one timestamped line per
   transition, with no ANSI and no redraw.
3. `-v` interleaves what `kc.sh` said; without it that firehose is suppressed.
4. Results are on stdout throughout, so a run can be piped while it narrates.

**Alternate flows**
- **A1 — A phase abstains.** It is drawn as skipped, never as passed. The summary
  reads `SkippedPhases` off the job record, which is authoritative; the live mark
  is best-effort. "Verification passed" and "verification did not run" are the
  difference between a snapshot whose secrets were checked and one whose were not.
- **A2 — The circuit breaker opens.** The wait and its length are printed, so a
  pause is never a bare hang.

**Covers.** NFR-5, NFR-12.

---

## UC-L5 — Cancel a run with Ctrl-C

**Goal.** Stop, without leaving a clone holding production credentials.

**Main success scenario**
1. The first SIGINT — or SIGTERM, which is how a CI time limit arrives — prints
   what is happening and asks not to be killed.
2. Each running job is cancelled **through the orchestrator**, which runs its
   teardown (UC-C11 A2). Cancelling the command's own context would do nothing:
   the run detached and is not listening to it.
3. The command keeps waiting until teardown finishes, then exits *cancelled*.

**Alternate flows**
- **A1 — A second signal.** The operator has decided not to wait. Every clone
  reference still outstanding is printed with its environment, and the command
  exits.
- **A2 — Teardown exceeds its grace period.** The same, on a timer.

**Exceptions**
- **E1 — Teardown fails.** The clone's identifier is named prominently so it can
  be removed by hand, and the next launch's sweep will find it (UC-C12).

**Postconditions.** On the ordinary path, no clone remains. On every path, an
operator has been told what does.
**Covers.** FR-C11, NFR-1, NFR-12.

---

## UC-L6 — Read the library, and read inside a snapshot

**Goal.** Answer "what have I got" without a window.

**Main success scenario**
1. `pcloak snapshot list` needs **no key at all**: it is built from the
   secret-free manifest sidecar beside each bundle, so it works on a machine that
   could not open any of them.
2. `show`, `verify`, `ledger`, `entities` and `users` open the snapshot: fetch,
   decrypt, verify the integrity tree, read.
3. A snapshot id may be abbreviated to any unambiguous prefix.

**Alternate flows**
- **A1 — A key already on this machine fits.** It is tried without being asked
  for, and the one that worked is named.
- **A2 — An ambiguous prefix.** The candidates are listed rather than one being
  picked.

**Exceptions**
- **E1 — No key fits.** Refused as a precondition. Nothing was changed anywhere.
- **E2 — A storage cannot be reached.** Named as unlisted, never folded into "no
  snapshots": "there are none" and "I could not look" are different answers.

**Postconditions.** Every working directory a session decrypted is destroyed
before the process exits — including on the failure paths.
**Covers.** FR-V1…FR-V8, NFR-12.

---

## UC-L7 — Restore a snapshot from the command line

**Goal.** Put a realm into a Keycloak from a runbook.

**Main success scenario**
1. `pcloak restore <snapshot> --env <name> [--strategy overwrite|skip|merge]`.
2. Integrity is proved, preconditions are listed, and a dry run is printed.
3. The import is applied, then the destination is re-read and compared.

**Alternate flows**
- **A1 — `--dry-run`.** Steps 1–2 only. Nothing is written.
- **A2 — The realm exists and the strategy is overwrite.** Destructive and
  irreversible, so it takes the realm's **own name typed back**, as
  `--confirm-realm`. `--yes` does not answer it.

**Exceptions**
- **E1 — The snapshot cannot be proven intact.** Refused. A snapshot that cannot
  be proven intact is never written to a target.
- **E2 — Post-import validation could not run.** Recorded and reported as
  skipped, never as passed.

**Covers.** FR-R1…FR-R4, NFR-12.

---

## UC-L8 — Manage age keys from a terminal

**Goal.** Let a machine with no display seal snapshots and open them again.

**Main success scenario**
1. `pcloak key generate <name>` creates an age keypair, stores the private half
   in this machine's keychain, and records the public half.
2. The private half is shown once, or written to a new `0600` file with
   `--private-key-file` — which is what a pipeline uses, because a private key in
   CI log output is a private key in whatever archives that log.
3. `capture --key <name>` then seals with it, and a later `snapshot show` opens
   with no flag at all.

**Alternate flows**
- **A1 — `key import`.** Only the private half is supplied; the public half is
  derived from it, so a mismatched pair cannot be stored.
- **A2 — `key public`.** The recipient alone, for pasting elsewhere. Not audited:
  a public key is not a secret.
- **A3 — `key reveal`.** The secret half, for a backup. Audited, like every other
  reveal in PortCloak.

**Exceptions**
- **E1 — `--no-keychain`.** Key creation is refused. The secret would go nowhere
  and `config.yaml` would be left naming a handle with nothing behind it — a key
  that lists as present, seals a snapshot, and cannot open it.
- **E2 — `key delete`.** Every snapshot sealed with the key becomes permanently
  unreadable, and PortCloak cannot say which ones those were. It takes the key's
  own name typed back; `--yes` does not answer it.

**Postconditions.** The secret half is in the keychain and nowhere else, unless
the operator asked for a file.
**Covers.** NFR-3, FR-N6, NFR-12.

**Scope note.** Keys were the first configuration `pcloak` wrote, and for a while
the only one. Environments and storage followed — see UC-L13, and the reasoning
in [13 §13.8](../13-command-line.md).

---

## UC-L9 — Probe an environment or a storage

**Goal.** Find out whether a capture would work, before running one.

**Main success scenario**
1. `pcloak env probe <name>` runs the same checks a capture runs, and prints each
   one — the passes as well as the failures.
2. `pcloak storage test <name>` round-trips: list, write, verify, remove.
3. A blocked result exits *precondition*: nothing was attempted, nothing changed.

**Alternate flows**
- **A1 — A check could not run.** Reported as skipped, never as passed.

**Postconditions.** Nothing on the target is changed. The result is recorded on
the definition, which is why this needs the folder to itself (UC-L10).
**Covers.** FR-C7, FR-N3, NFR-12.

---

## UC-L10 — Run while the desktop app is open

**Goal.** Two PortCloaks over one `~/.portcloak`, without either damaging the other.

**Main success scenario**
1. Every PortCloak — the window for its session, a command for its run — takes a
   **shared** claim on the home folder and keeps it.
2. Capturing, restoring, job control and every read therefore run beside each
   other and beside the window. Two captures at once are safe: each writes its own
   job record and its own staging directory, and one snapshot holds one realm, so
   there is nothing shared to corrupt.
3. Two things take an **exclusive** claim, briefly, and release it: the startup
   sweep, and a change to `config.yaml`.

**Alternate flows**
- **A1 — `--wait-for-lock <duration>`.** Queue behind the holder instead of
  failing, which turns a failing pipeline into a waiting one.

**Exceptions**
- **E1 — An exclusive claim is refused.** The refusal names the holder — program,
  subcommand, pid, since when — says why two would be a problem, and says what
  still works. Its exit code is its own, so a wrapper can retry on exactly this.
- **E2 — A process died holding the folder.** Nothing is wedged: the claim is an
  OS advisory lock, which the kernel drops when the process dies however it dies.

**Postconditions.** The startup sweep never runs while another PortCloak is here,
so it cannot mark a live capture interrupted or delete a working directory that is
in use.
**Covers.** NFR-11, NFR-12.

---

## UC-L11 — Seal a snapshot without a secret on the command line

**Goal.** Encrypt from a script without the key appearing in `ps`.

**Main success scenario**
1. `--key <name>` names a key already stored here. An identity key contributes its
   recipient — the public half, which is not a secret — and a passphrase key
   contributes the passphrase from the keychain.
2. Otherwise: `--recipient`, `--recipients-file`, `--passphrase-file`,
   `--passphrase-stdin`, or `PORTCLOAK_PASSPHRASE`.
3. On a terminal with none of these, the passphrase is prompted without echo, and
   **twice with a comparison**: a snapshot sealed with a typo cannot be opened by
   anybody, ever.
4. `--passphrase` takes the value directly. It is offered because refusing it
   sends people writing secrets to temporary files to get past a flag that will
   not take one, which is worse — but it warns, once, that argv is visible in
   `ps` to every user on the machine and that shells record it.

**Alternate flows**
- **A1 — Encryption declined.** `--no-encrypt` alone prints the full notice — the
  file will hold unmasked client secrets, LDAP bind credentials and RSA private
  signing keys in the clear — and refuses. It takes
  `--i-understand-unencrypted`, which is long, first-person and has no shorthand,
  and which `--yes` does not answer. The choice is recorded in the audit log
  either way.
- **A2 — The storage requires encryption.** The opt-out is not available at all.

**Exceptions**
- **E1 — No source and no terminal.** Refused, naming the flags that would
  satisfy it, rather than blocking on a prompt nothing will answer. Asking for a
  prompt where there is no terminal is refused the same way: the message names
  the alternatives rather than reporting the absence of a terminal, which is true
  and useless.
- **E2 — Two sources at once.** A usage error rather than a precedence rule.
  Four ways to supply one secret are alternatives; picking between two that were
  both given is a guess.

**Covers.** NFR-3, FR-C6, NFR-12.

---

## UC-L12 — Script it

**Goal.** Make `pcloak` usable in a pipeline.

**Main success scenario**
1. Every prompt has a flag that satisfies it, so no run needs a person.
2. Under `--json`, or with no terminal, an unmet prompt becomes a refusal naming
   the flag rather than a wait.
3. The exit code classifies the outcome:

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Failed — nothing to retry |
| 2 | Usage |
| 3 | Partial — some realms produced a snapshot and some did not |
| 4 | Precondition not met — **nothing was written to the target** |
| 5 | Retryable, or the time limit expired; the job is resumable |
| 6 | Busy — another PortCloak holds the folder |
| 130 | Cancelled |

4. `--timeout` bounds the run and cancels it the way Ctrl-C does, so teardown
   still happens.

**Covers.** NFR-4, NFR-5, NFR-12.

---

---

## UC-L13 — Define an environment and a storage from a script

**Goal.** Point PortCloak at a Keycloak, and at somewhere to put snapshots,
without a window.

**Main success scenario**
1. `pcloak env add <kind> <name> …` — one subcommand per kind, because the four
   share almost no fields and a single command would carry twenty-five flags of
   which twenty are wrong for whatever is being described.
   A secret comes from `--credential-prompt` (typed on the terminal, no echo),
   `--credential-file`, `--credential-stdin`, or `--credential` — the last of
   which warns. A definition with none is ordinary and stores nothing.
2. `pcloak storage add <kind> <name> …`, the same way.
3. Nothing is contacted. The definition is written and the operator is told to
   probe it.
4. `pcloak env probe <name>` / `pcloak storage test <name>` say whether it works.

**Alternate flows**
- **A1 — `--replace`.** Overwrites a definition of that name, so re-running the
  same provisioning script neither fails nor silently changes something.
- **A2 — `--default` on a storage.** What a capture uses when none is named.
- **A3 — `--encryption-required` on a storage.** Removes the opt-out entirely, so
  a snapshot cannot be written there in the clear even by an operator who passes
  `--i-understand-unencrypted`.
- **A4 — Removal.** `env remove` and `storage remove` forget the definition and
  delete its keychain entries. **Removing a storage does not empty it**: the
  snapshots stay, and deleting those is `snapshot delete`, one at a time.

**Exceptions**
- **E1 — The name is already taken.** Refused, naming `--replace`. Overwriting a
  definition by accident is how a capture ends up pointed somewhere else.
- **E2 — A job is using it.** Refused rather than removed out from under the run.
- **E3 — A prompt was asked for with no terminal.** Refused, naming the other
  three ways in.

**Postconditions.** A definition exists and nothing was contacted. Snapshots
already captured from an environment survive its removal: a snapshot records
where it came from as a fact about the past, not as a reference that has to
resolve.
**Covers.** FR-N1, FR-N2, FR-N4, FR-N6, NFR-12.

---

## Non-goals

`pcloak` never writes the **pointer file**; never prompts for a keychain
password; never caches a credential to disk; and is **not a daemon**.

It does not edit **preferences**, and the test that keeps them out is the one
that let environments and storage in: nothing is blocked on a preference, because
every one of them is a default that a flag on the relevant command already
overrides. [13 §13.8](../13-command-line.md) records where that line moved and
why it moved.
