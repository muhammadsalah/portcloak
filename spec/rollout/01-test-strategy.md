<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# 01 — Test Strategy

PortCloak moves credentials between production identity systems. A bug here does not produce a
wrong pixel; it produces a realm whose users cannot log in, or a bundle of unmasked client
secrets sitting somewhere nobody meant to put it. The test strategy is shaped around the four
things that can actually go wrong.

## 1.1 The four failure classes

| Class | What it looks like | Where it is caught |
|-------|--------------------|--------------------|
| **Fidelity loss** | The bundle is missing something, or carries a masked placeholder instead of a real secret, and nobody notices until login fails on the destination. | Fixture-matrix integration tests + the completeness report assertions (§1.5). |
| **Collateral damage** | A production pod is disturbed, a `Service` routes live traffic into an export pod, an ephemeral clone is left running. | Clone lifecycle tests, the label-stripping test, and the leak sweep (§1.6). |
| **Silent corruption** | A transfer is interrupted and the result *looks* like a valid bundle. | Fault-injection tests + checksum-tree verification (§1.7). |
| **Secret leakage** | A bind password or client secret reaches a log file, a crash dump, an exported CSV or a screenshot. | The redaction suite (§1.8), which is its own CI stage. |

Everything below exists to catch one of these four. A test that catches none of them, and does
not protect a refactor, is a test worth deleting.

## 1.2 Taxonomy

![Test taxonomy](./diagrams/png/r-03-test-taxonomy.png)

*Source: [`r-03-test-taxonomy.puml`](./diagrams/r-03-test-taxonomy.puml) · [SVG](./diagrams/svg/r-03-test-taxonomy.svg)*

**Unit** (`go test ./... -short`, no build tag). No network, no Docker, no Keycloak, no
keychain. Every remote seam has a fake. These run in seconds and are the tests that get run
on save.

**Contract** (`-short`, table-driven). One suite per interface, run against *every*
implementation. `Executor` and `BlobStore` each get one. When the SFTP store is added in P3, it
must pass the same table the disk store already passes — including the awkward rows: reading a
zero-byte object, listing an empty prefix, deleting something that is not there. This is what
stops "works on disk, subtly different on S3".

**Integration** (`-tags=integration`). Real Keycloak containers, real MinIO, real Azurite, a
real sshd, a real `kind` cluster. Slower, and the only place fidelity can genuinely be proved.

**Fault injection** (`-tags=integration`, own package). A proxy between PortCloak and its
dependency that can drop, stall, truncate and corrupt on command. See §1.7.

**Manual / exploratory.** A short scripted walkthrough per phase, listed in each phase document
under *Demo*. Not automated, and not optional: the questions "does this error message actually
help me?" and "is it obvious this bundle is unencrypted?" have no assertion form.

## 1.3 Fakes, and what they are allowed to be

Every fake lives beside its interface and is *shared* between packages — one `FakeExecutor`, not
one per test file.

A fake must be **hostile by default**. `FakeBlobStore` can be told to fail on the third `PutPart`,
to return a short read, to return an `ObjectInfo` whose size disagrees with the bytes it hands
back. A fake that only ever behaves is a fake that proves only the happy path, and the happy
path is not where this tool breaks.

`FakeExecutor` returns canned `kc.sh` stdout/stderr captured from real Keycloak runs and stored
in `testdata/kc-output/`, including the ugly ones: a version banner on stderr, an export that
exits 0 while writing a warning, an OOM kill. Canned output beats hand-written strings because
the parser's job is to survive what Keycloak actually emits.

## 1.4 The Keycloak fixture matrix

Fidelity claims (FR-F1…F9) are only meaningful against real Keycloak versions, because the
masking behaviour and the export layout both moved between them.

| Fixture | Keycloak | Contents | Proves |
|---------|----------|----------|--------|
| `minimal` | latest | One realm, two users, one client. | The pipeline works end to end. Used as the smoke fixture everywhere. |
| `rich` | latest | Users with TOTP and WebAuthn enrolments, several credential hash algorithms, clients with secrets, authorization services, an RSA and an HMAC key provider, custom auth flows, a password policy. | FR-F1, F2, F3, F4, F7, F8, F9. |
| `federated` | latest | An LDAP federation against a throwaway OpenLDAP container (with bind credentials and mappers) and a configured OIDC identity provider. | FR-F5, FR-F6. |
| `large` | latest | 120,000 generated users. | FR-C5 (`--users different_files`), NFR-6 (streaming), NFR-9 (index and paging stay usable). |
| `legacy` | an older supported major | The `rich` realm on an earlier version. | FR-C7 (version detection) and the masking quirks behind FR-C6. |
| `themed` | latest | A custom theme directory and a provider JAR deployed, referenced by realm settings. | FR-D1 detection, and that they are **reported, never migrated** ([D4](../12-decisions.md)). |

Fixtures are built by scripts in `testdata/fixtures/`, each producing a container image plus the
expected-inventory JSON that assertions compare against. They are built once and cached; a
fixture rebuild is an explicit action, because a silently drifting fixture makes every fidelity
assertion meaningless.

The exact version pinned as "latest" and "an older supported major" is chosen when P2 starts and
recorded in `testdata/fixtures/versions.env`, so the matrix is a fact in the repository rather
than a moving target in a document.

## 1.5 Proving fidelity

For each fixture, the integration test asserts in three places, because each catches a different
mistake:

1. **Against the source.** Query the live fixture's Admin API for the entity counts and identifiers
   it holds, and assert the captured bundle contains the same set. Catches *export* loss.
2. **Against the manifest.** Assert the completeness report marks every category `complete`, and
   that the categories the design says are out of scope are marked `outOfScope` — never `missing`.
   Catches *reporting* loss, where the data is present but the tool describes it wrongly.
3. **Against a round trip.** Restore the bundle into a blank Keycloak, then re-export from *that*
   and compare inventories with the original. Catches *import* loss, and is the only assertion
   that proves the bundle is actually usable rather than merely well-formed.

The round trip has one assertion that matters more than the rest and is worth naming: **a user
who could authenticate on the source can authenticate on the destination, with the same
password and the same OTP secret, and a token signed before the move still verifies after it.**
That single check exercises FR-F1, FR-F2 and FR-F4 together, and it is the promise the whole
tool is making.

## 1.6 Proving we did no harm

The collateral-damage class gets its own tests because its failures happen in someone else's
production namespace.

- **Serving workload untouched.** Before capture, record the serving `Deployment`/`StatefulSet`
  generation, its pod UIDs and its restart counts. After capture, assert all three are unchanged.
  A capture that restarted the production pod fails the test even if the bundle is perfect.
- **The label trap.** Create a `Service` selecting the serving workload's labels. Run a capture.
  Assert the clone pod never appears in that `Service`'s endpoints. This test is written *before*
  the clone code ([P3](./05-phase-p3-remote-targets.md)) and is the reason label stripping exists.
- **Teardown always.** Parameterised over every exit path — success, `kc.sh` failure, context
  cancellation, fetch failure, and a panic injected mid-export. After each, assert zero clone
  containers/pods and zero remote temp directories remain.
- **Leak sweep.** At the end of the whole integration run, list every container and pod carrying
  the `portcloak.io/ephemeral` label. A non-empty list fails the build, regardless of which test
  passed. This catches the leak that only happens in a combination nobody wrote a test for.

## 1.7 Fault injection

A configurable TCP proxy (`testdata/faultproxy`) sits between PortCloak and its dependency and
can, on a schedule: drop the connection, stall past the read timeout, truncate a response,
corrupt bytes in flight, and refuse new connections for a window.

Scenarios run against every remote storage backend and every remote target:

| Scenario | Expected behaviour |
|----------|--------------------|
| Drop at 40% of a large upload | Retry with backoff, resume from the last checkpoint, final object byte-identical. |
| Drop repeatedly, past the retry budget | Job fails with a clear reason, checkpoint on disk, **resumable after an app restart** (NFR-1). |
| Stall for longer than the read timeout | Timeout fires, treated as retryable, does not hang forever. |
| Corrupt bytes in flight | Checksum mismatch detected, the object is **not** marked complete, no bundle that "looks valid" is produced. |
| Refuse connections for 60s | Circuit breaker opens, the UI says so plainly, breaker closes on recovery without operator action. |
| Kill the process mid-upload, restart, resume | Converges to one complete object — never a duplicate (NFR-8). |

The last row is the one that justifies checkpoints living on disk rather than in memory, and it
is the acceptance test for P4.

## 1.8 Redaction {#redaction}

Its own CI stage, because a leak here is the failure with the longest tail.

- **Property test.** Generate realm representations containing marked secret values, run every
  one through the full capture and inspection pipeline with logging at debug level, then assert
  no marker appears in the log output, the audit log, an exported CSV or JSON, the progress
  events sent to the frontend, or a rendered error message.
- **Handler unit tests.** The `slog` handler redacts by key (`secret`, `password`, `privateKey`,
  `bindCredential`, `credentialRef`, …) and by shape (PEM blocks, JWT-looking strings, long
  base64 runs), and redacts inside nested structures and wrapped errors.
- **A deliberate hostile case.** A realm whose *client name* is the literal string
  `-----BEGIN RSA PRIVATE KEY-----`. It must be logged intact — over-redaction that mangles
  ordinary data is a real bug too, just a quieter one.
- **Reveal is the only exception.** UC-I9's audited reveal is the one path where a secret is
  rendered. The test asserts it writes an audit entry naming what was revealed and when, and
  that the value still never reaches the log.

## 1.9 Frontend testing

The frontend holds no business logic, so it is tested narrowly and deliberately:

- **Binding contract tests** — the TypeScript types generated by Wails are checked against the Go
  controller signatures in CI, so a renamed field fails the build rather than producing an
  undefined at runtime.
- **View-state unit tests** for the three places with real logic: wizard step validity, the job
  progress reducer, and the user-table paging/facet state.
- **Manual accessibility and clarity pass** per phase against the [design file](../lunacy/) —
  keyboard reachability, focus order, and whether the unencrypted-bundle warning is unmissable.

The suite is Vitest over jsdom with Testing Library, run by `npm --prefix frontend test`, and
each test file sits beside the module it covers. The engine is never reached: a test that needs
an answer from it mocks `src/api.ts`, which is the frontend's only door to Go.

That a module *is* view state, rather than something drawn, is the thing the tests depend on, so
the three named above were moved out of their components to where they can be stated on their
own — `pages/capture/draft.ts`, `pages/restore/wizard.ts`, and `pages/activity/live.ts`. Only the
user table stayed in place, because paging and facets are held in the query it sends and the
assertion worth making is which query that was.

Coverage is measured with two floors, because one number cannot say both things
([`vitest.config.ts`](../../frontend/vitest.config.ts)). The global figure is low and honestly
so — most of what is left is a form wired to the engine, exactly as this section intends — and
its only job is to stop the suite rotting. The per-file figure is the one that means something:
every module that decides something is covered completely, and a change that stops covering one
fails CI rather than being averaged away by the pages around it.

## 1.10 What "verified" means in the phase documents

Each phase's *Verification* table maps a requirement to **evidence**: a named test, a produced
artifact, or a recorded manual walkthrough. Three rules keep it honest:

1. Evidence is something that exists after the phase — a test name, a file, a screenshot.
   "Implemented in `engine/kc`" is not evidence.
2. A requirement is verified by the test that would *fail* if it were broken. If no such test
   exists, the requirement is not verified, however much code was written.
3. Out-of-scope items are verified too — by asserting the tool *says* they are out of scope
   ([D1](../12-decisions.md), sessions) rather than silently omitting them.
