<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# 02 — The engine

Targets, ephemeral clones, storage and resilience. The theme running through every entry here is
the same one: **the dangerous failure is the one that reports success.** A capture that errors
costs an operator ten minutes. A capture that returns an empty snapshot and calls it complete
costs them the realm, eighteen months later, during an incident.

---

## 1. A read that finds nothing must say so

**Symptom** — A snapshot that looks complete and contains nothing. No error anywhere in the run.

**Cause** — Docker's `FetchDir` returned success with zero artifacts when the directory did not
exist. An export that wrote nothing therefore packaged cleanly. Kubernetes and SSH already
errored on the same input; only Docker swallowed it — which is the shape this fault always
takes, one adapter out of four disagreeing quietly with the other three.

**Rule** — Absence is an error, never an empty result, on any path that feeds the packager. A
directory that should exist and does not is fatal and names itself.

**Guard** — The `Executor` contract table (`internal/engine/target/targettest/contract.go`) runs
against local, SSH, Docker and Kubernetes. A divergence between adapters is a bug in the newest
adapter, never a reason to fork the table. This defect was found by running the existing table
against a real daemon for the first time.

---

## 2. Register teardown before the thing that can fail halfway

**Symptom** — A clone left running after a capture failed. It carries the same database
credentials as the serving instance.

**Cause** — Capture registered its teardown `defer` *after* checking `Prepare`'s error. A
`Prepare` that failed after creating the clone — an `ImagePullBackOff`, for instance — abandoned
it. Nothing covered it because nothing had failed that late before.

**Rule** — The `defer` goes immediately after the call that can create the resource, before the
error check, and the teardown tolerates a partially created resource. Teardown is unconditional:
it runs on success, on error, on cancellation and on panic.

**Guard** — Clone lifecycle teardown is asserted across five exit paths including panic, and CI
fails if any ephemeral clone outlives an integration run.

---

## 3. A clone inherits nothing

**Symptom** — Real user traffic arriving at an export pod that serves nothing.

**Cause** — A pod copied from a serving workload carries `app=keycloak`, so the production
`Service` selects it and routes live requests into it.

**Rule** — Nothing is inherited. `clone.go` strips the whole label set rather than filtering it,
and applies only PortCloak's own labels. A later change that copies "just the useful labels"
brings the trap straight back — the comment at the top of that file exists to say so.

---

## 4. An unclassified error is terminal

**Cause and rule together** — The resilience layer defaults an unrecognised error to
`Terminal` rather than retryable. The direction matters: an unclassified fault surfaces on the
first occurrence instead of being retried in a loop until a timeout hides what it was. A new
failure mode should be a bug report, not a stall.

The corollary: an **unreplayable stream is never retried.** Retrying one commits a truncated
object. Resume from the checkpoint is the mechanism there, and it is a different mechanism from
retry on purpose.

**Where this lives** — `internal/engine/resil`. Adapters are wrapped
(`internal/engine/reliable`) rather than sprinkled with retry code at call sites, so an adapter
cannot reach the orchestrator with an unguarded remote call in it.

---

## 5. A checkpoint is a hint; the destination is the authority

**Cause and rule together** — Resume asks the destination what it already holds —
`ListParts` on S3, `GetBlockList` on Azure, the file itself on disk and SFTP — and positions the
source reader to match. A checkpoint file records where a transfer *believed* it got to, which
is not the same claim and is not trusted on its own.

A resumed upload commits a prefix it never sent, so the checkpoint carries the rolling SHA-256
state alongside the offset, and the finished object is verified against the digest computed
before the transfer — the same check a fresh upload gets. Where no usable state survives, the
prefix is re-read rather than assumed.

**Guard** — Offset resume is its own contract table, so disk and SFTP prove the same guarantees
rather than one being tested and the other assumed.

---

## 6. Validate identity, not counts

**Symptom** — A restore that validates cleanly and leaves every previously issued token
failing to verify.

**Cause** — Checking that the destination realm has the right *number* of signing keys. The
right number of keys with the wrong IDs is exactly the failure the whole feature exists to
prevent — token continuity comes from carrying the realm's actual RSA keys, so the check has to
be by KID.

**Rule** — Where a thing is identified by an ID, validate the ID. A count is a proxy, and a
proxy is what passes when the real property is broken.

---

## 7. Free ports are a race, and the race is retryable

**Cause and rule together** — On local and SSH the export can bind a port and collide with a
Keycloak already serving. There is an unavoidable gap between PortCloak releasing a reserved
port and Keycloak binding it, in which something else can take it. That failure is classified
retryable and the attempt is redone with fresh ports — it is a race, not a misconfiguration, and
treating it as terminal would make captures fail intermittently for no actionable reason.

**But only when a port option was passed.** See entry 9: on the versions whose `export` takes no
port option, PortCloak cannot move the export anywhere, so reallocating and retrying three times
would report a race it never had a say in. Retryability there is conditional on
`ExportCommand.PortsPassed`.

---

## 8. Integration tests that cannot run must not pass

**Rule** — Integration tests sit behind `-tags=integration` and run against real MinIO,
Azurite, sshd and Docker. A missing container reads as "not run", never as a silent pass. The
Docker defects in entry 1 and entry 2 existed for as long as they did because the table that
would have caught them had never been pointed at a real daemon.

---

## 9. `kc.sh export` takes the options that binary offers, not the ones the spec named

**Symptom** — Every capture fails immediately, having created and torn down an ephemeral clone
first:

```
Option: '--http-port' not valid for command export
Possible solutions: --http-access-log-enabled, --http-management-port, …
```

Before the classifier learned that wording, it surfaced as `kc.sh export exited with code 2`,
which names nothing that has to change.

**Cause** — The driver passed `--http-port`, `--https-port` and `--http-management-port` on
every target unconditionally. The design doc specified all three and the code carried a comment
saying it "costs nothing". Neither `--http-port` nor `--https-port` is an option of `export` or
`import` on **any** Keycloak measured, and an unrecognised option aborts the command before it
reads the realm. So it cost every capture, on every version.

What `export` actually offers, read from `--help-all` on real images (kept in
`testdata/kc-help`):

| Keycloak | port options on `export` |
|----------|--------------------------|
| 24.0 | none |
| 25.0 – 26.3 | `--http-management-port` (plus management TLS options) |
| 26.5 | none |

**Rule** — Ask the binary. `Orchestrator.discoverOptions` runs `kc.sh <sub> --help-all` in the
execution context the command will run in — the clone on Docker and Kubernetes, the host on
local and SSH — and `kc.BuildExport` emits only what came back. A version table would have been
wrong three times over the four versions above, and wrong again by the next release.

When discovery fails, **nothing** is passed. The two failures are not symmetric: a rejected
option fails every capture unconditionally, while a missing one risks a bind conflict only when
something is already listening. Prefer the conditional failure. The corollary is in the retry
loop — a bind conflict is only retryable when a port option actually reached the command line,
or PortCloak retries three times against a port it never had any say in.

**Guard** — `TestParseOptions_AgainstRealHelpOutput` and `TestBuildExport_AgainstEachRealVersion`
(`internal/engine/kc/options_test.go`) run against the four captured help outputs and assert
that `--http-port` never appears and that the management port appears exactly where that version
offers it. `TestCapture_PassesNoPortWhereKcAcceptsNone`,
`TestCapture_PassesNoPortWhenTheOptionsCannotBeAsked` and
`TestCapture_ReportsARejectedOptionByName` cover the orchestrator's three paths. Add the next
version's `--help-all` output to `testdata/kc-help` and the fixture table picks it up.

**How this was confirmed** — By running the built invocation against 24.0, 25.0.4, 26.3 and
26.5.0 containers: all four now reach `realm not found`, which is as far as an export gets with
no database behind it. The old invocation is still rejected by 26.3.

---

## 10. A push into a clone has to create the directory *and* own the file

**Symptom** — A restore onto Docker fails five times over:

```
send the file did not succeed after 5 attempts. The last problem was:
PortCloak could not write /tmp/portcloak-…/import/corp-a-realm.json into the clone.
```

Behind it, once that is fixed, a second failure that reports success: the file lands, and the
import fails on `permission denied` reading it.

**Cause** — Two divergences in the same method, both the familiar shape of one adapter out of
four.

*The directory.* The restore pushes into `<workdir>/import/`, which `Prepare` does not create —
it is the caller's directory, named by the caller. `local.PushFile` calls `os.MkdirAll` and
`ssh.PushFile` calls `client.MkdirAll`; the clone path went straight to `CopyIn`, and Docker's
`CopyToContainer` refuses a destination that does not exist. Worse, a missing directory is
indistinguishable at that layer from a dropped connection, so it was classified retryable and
the same certain failure was attempted five times.

*The ownership.* `CopyToContainer` unpacks as root and honours the tar entry's uid and gid. The
header carried the default zeroes, so the realm landed as `root:root` mode 0600 — inside an
image that runs as `keycloak`. The copy succeeds and `kc.sh import`, running as that
unprivileged user, cannot read the file it was handed. Kubernetes never had this: it unpacks
with `tar` as the pod's own user, which cannot chown, so it lands the file correctly by
accident.

**Rule** — `clone.Executor.PushFile` creates the parent directory (once per directory, cached
for the life of the clone) and asks the clone who it runs as (`id -u; id -g`, once) before
handing the bytes to the platform. `clone.FileOwner` carries that into the tar header. Mode
stays 0600: the file holds unmasked secrets, so the fix is the right owner, not a looser mode.
A directory that cannot be created is fatal, not retryable.

**Why CI was green** — The contract table pushed only to `<workdir>/pushed.json`, a directory
that already exists, and the service container it ran against runs as root. Both faults need a
fixture shaped like production to appear: a nested destination, and a non-root image. The table
now pushes into a directory that does not exist yet, and the fault was confirmed and fixed
against a real `quay.io/keycloak/keycloak:26.4` container.

**Guard** — `pushfile creates the directory it was given` in the `Executor` contract table, plus
`TestPushFile_CreatesTheDirectoryAndOwnsTheFile`,
`TestPushFile_FallsBackToRootWhenTheCloneCannotSayWhoItIs` and
`TestPushFile_ADirectoryThatCannotBeMadeIsFatal` (`internal/engine/target/clone/clone_test.go`),
which also pin that three files into one directory cost one round trip rather than three.

**Found alongside** — `runRestore` registered its teardown `defer` after checking `Prepare`'s
error, the exact defect entry 2 records being fixed in the capture path. Restore never got the
same treatment, so a `Prepare` that failed after creating the clone leaked it. Fixed, with the
comment that says why the order matters.

---

## 11. A derived path is a guess, and a guess needs an override

**Symptom** — A capture from a custom-built Keycloak image fails partway through, in the export,
against a path nobody chose:

```
/opt/keycloak/bin/kc.sh: no such file or directory
```

The probe passed. The clone was created. The failure arrives after the expensive part.

**Cause** — Docker and Kubernetes derived `kc.sh` from `KEYCLOAK_HOME` in the container's
environment and otherwise fell back to `/opt/keycloak/bin/kc.sh`. Both are good guesses for the
official images and neither is a fact. An image built from a distroless base, or one that
installs Keycloak under `/app`, or a vendor's rebuild that leaves `KEYCLOAK_HOME` unset — all of
these are ordinary, and PortCloak had nowhere to be told.

Local and SSH never had this problem, because they ask for the install root and derive the path
from an answer the operator gave.

**Rule** — Where a path is derived rather than stated, the derivation is a default and the
environment carries an override. `Environment.KcPath` wins outright over both guesses on Docker
and Kubernetes; the probe reports which path it will use and says when the answer came from the
environment rather than from the image, so a wrong one is visible before a capture rather than
during one. A relative path is rejected when the config is read — the export runs from a work
directory that is not the install root, so a relative path has no meaning inside the container.

**Guard** — Config validation rejects a non-absolute `kcPath`
(`internal/engine/config/validate.go`, covered by the config validation tests), and both
platforms route the decision through a single `kcPathIn(configured, …)` that takes the override
first.

---

## 12. A secret the tool refuses to keep is a feature the operator turns off

**Symptom** — Nothing fails. Encryption simply stops being used. A passphrase typed at capture
has to be typed again at every restore, at every inspection, on every machine; a generated age
keypair was shown once and stored nowhere, so the operator became the key management system. The
observable outcome is a library of unencrypted snapshots and an operator who has a good reason
for each one.

**Cause** — PortCloak held every other secret it needed — SSH keys, S3 credentials, Admin API
passwords — in the OS keychain behind a handle, and made exactly one exception: the material that
protects the file holding unmasked client secrets and private signing keys. The exception was
deliberate and it was wrong. "We never store your key" reads as rigour and behaves as friction,
and friction is what decides whether encryption is on.

**Rule** — A key is a named, stored thing on the same terms as every other secret: value in the
keychain, name and handle in `config.yaml`, portable configuration and non-portable secrets. An
open tries what is stored before it asks for anything, and the trying happens while reading the
envelope — the first document in the archive, so a wrong key fails there rather than after a
multi-gigabyte extraction.

Order matters twice. Identities are tried before passphrases, because matching an age recipient
costs nothing and every scrypt attempt pays a deliberate second. And a key the operator typed is
tried before any stored one, so an explicit override stays an override.

Silent is not the same as invisible. The key that opened a snapshot is named on the screen and
recorded in the audit log, along with every creation, import, reveal and deletion — because the
thing that replaced a prompt has to leave more evidence than the prompt did, not less.

**Guard** — `TestOpen_UsesAStoredIdentityWithoutBeingAsked`,
`TestOpen_UsesAStoredPassphraseWithoutBeingAsked`,
`TestOpen_ASuppliedKeyIsNotAttributedToAStoredOne` and `TestOpen_SaysWhatItTried`
(`internal/engine/inspect/unlock_test.go`) seal real bundles and open them with nothing but a
stored key. `TestKeys_*` (`internal/app/keys_test.go`) covers the store itself: generated keys
are usable, imports derive their own public half, deletion takes the secret with it, and a reveal
is audited without recording what was revealed.

---

## 13. An in-memory SQLite database is named, and a name is shared

**Symptom** — Open one snapshot, browse its users, open a second, and its Users
tab shows only:

```
creating the index schema: SQL logic error: table users already exists (1)
```

The first snapshot is fine. Every subsequent one in the same run of the
application is not.

**Cause** — The inspection index is one database per snapshot, and the on-disk
form always was: `Home.IndexFile(snapshotID)`. The in-memory form — which is
what a realm under `InMemoryThreshold` gets, so that a small realm never touches
disk — opened `file::memory:?cache=shared`. SQLite's shared cache keys an
in-memory database by **name**, and that name was a constant. Every index in the
process was therefore the same database, and the second `CREATE TABLE users`
found the first one's schema already there.

The error was the lucky outcome. The same bug one step to the left — a schema
created with `IF NOT EXISTS` — mixes two realms' users into one searchable table
and answers questions about the wrong organisation.

The cache cannot simply be made private: an unshared in-memory database lives
only as long as the connection that created it, and `database/sql` may retire an
idle connection underneath the index. So the isolation has to come from the name.

**Rule** — One index is one snapshot, in memory or on disk, sharing nothing. The
in-memory database name carries a process-unique sequence number; the snapshot id
is folded in for legibility only, never for uniqueness.

The same rule caught a second case one layer up: re-opening a snapshot that was
already open replaced the session in the engine's map without closing the one it
displaced, leaving a decrypted working directory on disk until the next launch
swept it — and, above the threshold, a second index truncating the first one's
file underneath it. A session that is replaced is closed.

**Guard** — `TestIndex_IsOnePerSnapshot`
(`internal/engine/inspect/inspect_test.go`) opens two snapshots at once, indexes
both, and checks that each holds its own realm's users and that closing one does
not break the other. `TestSessions_ReopeningASnapshotClosesTheOneItReplaces`
(`internal/app/keys_test.go`) covers the session map.
