<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# P2 — Local Capture

**Goal.** The first vertical slice. Point PortCloak at a Keycloak installed on this machine,
choose a realm, and get a sealed, checksummed `.pck` bundle in a disk folder, with a manifest
that says exactly what it carries and a completeness report that admits anything it did not.
Everything downstream — remote targets, cloud storage, inspection, restore — is a variation on
what is built here.

**Covers.** UC-C1, UC-C5, UC-C6, UC-C7 · FR-C1, FR-C5, FR-C7, FR-C8, FR-C10 ·
FR-F1…FR-F9 · FR-M1, FR-M2, FR-M3 · FR-S1, FR-S6 · NFR-2, NFR-6, NFR-8.

**Depends on.** P0, P1.

**Packages.** `engine/target/local`, `engine/target/ports`, `engine/kc`, `engine/manifest`,
`engine/snapshot`, `engine/store/disk`, `engine/orchestrator`, `internal/app/capture_controller`.

---

## The order these tasks are in, and why

The pipeline is built **back to front**: packaging and the disk store first, then the manifest,
then `kc.sh`, then the orchestrator that joins them. Each stage can therefore be tested against
a fixture export checked into `testdata/` before any Keycloak is involved. By the time the real
export lands in T-P2.5, the only new variable is Keycloak itself — which is the variable most
likely to surprise us, and the one we want isolated.

## Tasks

### T-P2.1 — `BlobStore` and the disk store
The interface from [02 §2.4](../02-architecture.md) with its first implementation. The disk
layout is browsable by design ([04 §4.2](../04-storage-backends.md)): an operator who lost the
app should still be able to find and identify a snapshot with `ls`.

Writes are atomic — write to a temp name in the same directory, `fsync`, rename — so an
interrupted write can never leave something that looks like a complete bundle. This is the
disk-local instance of the rule P4 generalises.

*Done when:* the `BlobStore` contract suite passes, including zero-byte objects, empty prefixes,
deleting a nonexistent key, and a write interrupted by a kill.

### T-P2.2 — Snapshot packager
Normalize → tar → zstd → integrity tree → envelope, entirely `io.Reader`-chained with no
full-bundle buffering (NFR-6). Normalisation is stable file ordering plus canonical JSON, so the
same input yields a byte-identical bundle (NFR-8). The two sidecars —
`<id>.manifest.json` and `<id>.sha256` — are written next to the bundle, not inside it, so a
snapshot can be triaged without opening it.

*Done when:* packaging the same fixture twice yields identical bytes; a bundle from a 2 GB input
stays under a bounded memory ceiling asserted by the test; the integrity tree detects a single
flipped byte in any artifact.

### T-P2.3 — Manifest builder and completeness report
Parse the export directory into the realm inventory of
[07](../07-realm-carryover-manifest.md) — sections A through M — and produce the completeness
report. Three categories, and the distinction is the point:

- `complete` — captured, and counted.
- `partial` / `missing` — something went wrong, with the reason.
- `outOfScope` — deliberately not carried ([D1](../12-decisions.md) sessions,
  [D4](../12-decisions.md) themes and provider JARs).

`outOfScope` exists so a healthy snapshot never reads as broken. A tool that reports "sessions:
missing" on every single capture trains its operator to ignore the report, and then the one real
`missing` goes unread.

The manifest is emitted as JSON and rendered for humans (FR-M3). The secret ledger lists every
secret **by location and kind, never by value**.

*Done when:* the `rich` fixture produces an inventory matching its expected-inventory JSON;
sessions and themes appear as `outOfScope`; the ledger enumerates secrets without containing one.

### T-P2.4 — Free-port allocator
Bind `127.0.0.1:0`, record the port, release it, and pass it as `--http-port` / `--https-port` /
`--http-management-port` ([03 §3.4](../03-capture-targets.md)). There is an unavoidable race
between releasing and Keycloak binding, so a bind conflict is classified **retryable** and the
allocation is simply retried with fresh ports.

This is the mechanism that stops an offline export from colliding with the Keycloak already
running on 8080 and exiting non-zero.

*Done when:* three ports are allocated and distinct; a deliberately occupied port causes a
retry, not a failure; the allocator is proven not to leave sockets open.

### T-P2.5 — `kc.sh` driver
Build the invocation from [03 §3.8](../03-capture-targets.md) — `--dir`, `--realm`,
`--users different_files`, `--users-per-file 1000`, and the three port flags — and parse the
result. Parsing is where the real work is: Keycloak writes to both streams, exits 0 while
warning, and changes its banner between versions. Canned outputs in `testdata/kc-output/` drive
the parser tests.

`--users different_files` is not an optimisation; it is what makes a 120,000-user realm
survivable for both the export and the P6 index.

*Done when:* the driver builds correct invocations for the version matrix, classifies exit
codes, extracts warnings, and detects a truncated export directory rather than treating it as
success.

### T-P2.6 — Local executor: `Prepare`, `Run`, `FetchDir`, `Teardown`
Complete the local `Executor`. `Prepare` reports `ModeInPlace` with an allocated `PortSet`;
`FetchDir` streams the export directory into the packager, computing SHA-256 as bytes pass
(NFR-2) rather than in a second read; `Teardown` removes the temp export directory
unconditionally, via `defer`, on every exit path including panic.

Even locally, the export directory holds unmasked secrets on disk. Leaving it behind after a
successful capture would be the tool creating exactly the exposure it exists to manage.

*Done when:* the temp directory is gone after success, after failure, after cancellation, and
after an injected panic.

### T-P2.7 — Capture orchestrator
The job state machine: probe → prepare → export → fetch → manifest → package → store →
teardown, emitting progress at each transition and persisting job state to `jobs/<id>.json`.
Cancellation propagates by context and still runs teardown.

Several realms in one run (UC-C5) produces **several independent snapshots**, one per realm
([D5](../12-decisions.md)) — a loop over the pipeline, not a multi-realm bundle. One realm
failing does not abandon the others.

*Done when:* a capture completes end to end; cancelling mid-export leaves no artifacts and no
clone; a three-realm run where the second fails still produces snapshots one and three, and says
plainly what happened to the second.

### T-P2.8 — Capture wizard and snapshot library
Wire the wizard from the design file — environment → realms → storage → options → review — and
the snapshot library listing what a disk storage holds. The encryption step is present and
prominently recommended, but does nothing yet; it becomes real in P5. It is shown from the start
so the opt-in flow is designed and reviewed early rather than bolted on.

*Done when:* a capture can be run entirely from the UI, progress is live, and the finished
snapshot appears in the library with its manifest rendered.

---

## Testing

**Unit.** Packager determinism and bounded memory. Integrity tree tamper detection. Manifest
parsing across every section of [07](../07-realm-carryover-manifest.md). Completeness
categorisation, including that out-of-scope never reports as missing. `kc.sh` output parsing
across the canned corpus. Port allocator conflict handling. Orchestrator state machine including
every cancellation point.

**Contract.** `BlobStore` (disk). `Executor` full lifecycle (local).

**Integration.** Capture from `minimal`, `rich`, `legacy` and `large`. For each: the three-way
fidelity assertion from [01 §1.5](./01-test-strategy.md) — against the source, against the
manifest, and (once P7 lands) against a round trip. The `large` fixture additionally asserts a
bounded memory ceiling and a completed `--users different_files` export.

**Fault injection.** Kill the process mid-package and assert no complete-looking bundle exists.
Fill the disk mid-write and assert the failure is clear and leaves nothing behind.

**Manual.** Capture a realm, then open the bundle with ordinary tools (`zstd -d | tar t`) and
confirm the layout matches [06 §6.1](../06-snapshot-and-manifest.md). Read the rendered manifest
and check that every claim it makes is one you can verify by hand.

## Verification

| Requirement | Evidence |
|---|---|
| FR-C1 | `TestCapture_Local_Minimal` produces a bundle; manual walkthrough recorded. |
| FR-C5 | `TestCapture_Large_UsersDifferentFiles` — 120k users, per-file split asserted, memory ceiling asserted. |
| FR-C7 | `TestProbe_DetectsVersionAndKcPath` across the version matrix (extends the P1 evidence). |
| FR-C8 | The capture path invokes offline `kc.sh export` only; `TestCapture_NoAdminAPICalls` asserts no Admin API request is made during capture. |
| FR-C10 | `TestPortAllocator_*` plus `TestCapture_WithKeycloakRunningOn8080` — capture succeeds while a server occupies the default ports. |
| FR-F1…F9 | `TestFidelity_Rich` and `TestFidelity_Federated`: inventory compared against the source Admin API, per category. Round-trip half completes in P7. |
| FR-M1 | The produced `manifest.json` for the `rich` fixture, attached to the phase record. |
| FR-M2 | `TestCompleteness_OutOfScopeIsNotMissing` and the report from the `themed` fixture. |
| FR-M3 | The same manifest rendered in the UI, screenshot attached. |
| FR-S1 | `BlobStore` contract suite (disk) plus a `ls`-legible folder, screenshot attached. |
| FR-S6 | `TestCapture_MultiRealm_ProducesOneBundlePerRealm`. |
| NFR-2 | `TestIntegrityTree_DetectsSingleByteFlip`. |
| NFR-6 | `TestPackager_BoundedMemory` on a 2 GB input. |
| NFR-8 | `TestPackager_Deterministic` — same input, byte-identical bundle. |
| UC-C5 | `TestCapture_MultiRealm_PartialFailure` — one realm fails, the others complete. |
| UC-C6 | Covered by P1; re-verified as the wizard's first step. |
| UC-C7 | `TestCapture_WithKeycloakRunningOn8080`. |

## Demo

With a Keycloak running on this machine on port 8080, capture its `rich` realm to a disk folder.
The export runs on ports nobody is using and the running server never notices. The library shows
the new snapshot; the manifest says 12 clients, 4 key providers, 2 identity providers, users with
TOTP; the completeness report says sessions are out of scope, by design. Then untar the bundle
on the command line and show the manifest inside it matches what the UI displayed.

## Exit criteria

- [ ] A local realm captures end to end into a disk folder, from the UI.
- [ ] The bundle is deterministic, streamed, and integrity-checked.
- [ ] The manifest and completeness report are accurate on the `rich` fixture.
- [ ] Out-of-scope categories never render as missing.
- [ ] Export temp directories are gone on every exit path.
- [ ] Capture works while Keycloak occupies the default ports.

## Commits

```
feat(store): BlobStore interface and the disk implementation
feat(snapshot): streaming tar+zstd packager with an integrity tree
feat(manifest): realm inventory parsing and the completeness report
feat(manifest): secret ledger by location and kind, never by value
feat(target/ports): free-port allocation with retry on bind conflict
feat(kc): kc.sh export invocation building and output parsing
feat(target/local): full executor lifecycle with unconditional teardown
feat(orchestrator): capture job state machine and checkpointed job state
feat(ui): capture wizard and snapshot library
test(capture): fidelity assertions against the rich and large fixtures
```

## Risks

**`kc.sh` output is not a stable interface.** It is a human-facing CLI and it changes.
*Mitigation:* the driver never parses prose for success — it uses the exit code and inspects the
produced directory. Prose parsing is confined to extracting warnings, where being wrong is
cosmetic.

**The `large` fixture is slow to build and slow to run.** *Mitigation:* build it once and cache
the image; run it nightly rather than per-push, but treat a nightly failure as blocking, because
NFR-6 and NFR-9 have no cheaper proof.

**Determinism is easy to lose.** A map iteration, a timestamp, or a compression-level default can
break byte-identity. *Mitigation:* `TestPackager_Deterministic` runs on every push, so the
commit that breaks it is named immediately rather than discovered in P7.
