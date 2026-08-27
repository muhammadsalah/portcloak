<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# P4 — Resilience

**Goal.** A bad connection stops costing an operator their whole job. Every remote operation is
wrapped in bounded retry with backoff and jitter, guarded by a circuit breaker, and checkpointed
so that a transfer interrupted at 80% resumes at 80% — including after PortCloak itself has been
closed and reopened. The Activity screen makes all of this legible, and a failure explains
itself in a sentence an operator can act on.

**Covers.** UC-O1…UC-O6, UC-O8 · NFR-1, NFR-5, NFR-8.

**Depends on.** P0…P3.

**Packages.** `engine/resil`, `engine/config` (job checkpoints), `engine/obs` (audit log),
`engine/orchestrator`, `internal/app/jobs_controller`.

---

## Why this is a retrofit, and why that is correct

P2 and P3 call remote I/O directly. This phase wraps it. That ordering looks backwards and is
deliberate — rule 3 of the [rollout](./README.md#five-rules-the-plan-follows).

By now there are roughly a dozen real call sites: SSH exec, SFTP fetch, Docker API calls,
Kubernetes exec, disk writes. They have different retry semantics — an SFTP read can resume from
an offset, a `kubectl exec` cannot resume at all and must restart the export, a `Delete` on a
missing key is already success. Designing `Doer` before knowing that would have produced a
uniform abstraction that fits none of them, and the deeper mistake — **treating an
unretryable operation as retryable** — is exactly the kind that a speculative design makes and a
retrofit does not.

The retrofit is one focused change, and the fault-injection suite from
[01 §1.7](./01-test-strategy.md) is what proves it landed correctly.

## Tasks

### T-P4.1 — Retry, backoff and jitter
`Doer` with bounded retries, exponential backoff and full jitter, honouring context
cancellation between attempts. Errors are classified retryable or terminal by a per-adapter
classifier — a network reset is retryable, a rejected credential is not, and retrying the second
one just delays a message the operator needs immediately.

*Done when:* backoff and jitter are asserted with a fake clock; a terminal error does not
retry; cancellation during a backoff wait returns promptly rather than sleeping out the interval.

### T-P4.2 — Circuit breaker
Per-endpoint breaker: open after consecutive failures, half-open probe, close on success. When
open, the UI says so plainly — "the storage at `s3.eu-west-1` has been unreachable for two
minutes; retrying at 14:32" — rather than presenting a spinner that means nothing.

*Done when:* the state machine is unit-tested against a fake clock; recovery closes the breaker
with no operator action.

### T-P4.3 — Checkpoints on disk
`jobs/<job-id>.json` gains transfer checkpoints: which stage completed, byte offsets, multipart
upload IDs and part ETags. Written atomically after each committed unit, so a checkpoint never
describes progress that was not actually made.

Checkpoints on disk rather than in memory is the whole point: NFR-1 requires resume **across app
restarts**, and an in-memory checkpoint dies with the process that needed it.

*Done when:* a job killed at any stage boundary has a checkpoint that describes exactly the work
completed, verified by a test that kills at every boundary in turn.

### T-P4.4 — Resume, and convergence
Resume for each transport: SFTP by offset, disk by offset, and — landing in P5 alongside them —
S3 multipart and Azure block lists. Kubernetes and Docker exec cannot resume mid-export, so their
checkpoint granularity is the stage, and resume restarts the export inside a fresh clone. Saying
that honestly in the UI is better than implying a fine-grained resume that does not exist.

Resume is **convergent** (NFR-8): re-running an interrupted transfer produces one complete
object, never a duplicate and never a concatenation.

*Done when:* the fault-injection matrix passes for every transport, including the
kill-restart-resume row, and the final object is byte-identical to an uninterrupted run.

### T-P4.5 — Wrap every remote call site
The retrofit. Every adapter's remote operations go through `Doer` with a classifier appropriate
to that adapter. A lint rule or an architecture test asserts no raw remote call bypasses the
wrapper, so a future adapter cannot quietly opt out.

*Done when:* the architecture test passes and the fault-injection suite is green across all
targets and stores built so far.

### T-P4.6 — Partial-failure ledger and failure explanation
The ledger from [05 §5.5](../05-resilience.md): what succeeded, what degraded, what failed, and
why. This feeds UC-O5 — a failed job shows what it was doing, what went wrong, whether it is
retryable, and what to do next. An error that says `context deadline exceeded` has told the
operator nothing; one that says "the export ran for 20 minutes without producing output; the
database may be under load" has told them what to check.

*Done when:* every failure class in the injection matrix renders as an actionable sentence, and
none renders as a raw wrapped error.

### T-P4.7 — Activity screen: monitor, resume, cancel, discard
Running and interrupted jobs with live progress, throughput and retry state (UC-O1); resume
(UC-O2); cancel, which still runs teardown (UC-O3); and discard, which removes the checkpoint and
cleans up partial artifacts (UC-O4). Interrupted jobs survive an app restart and are offered on
launch.

*Done when:* an interrupted job is offered after a restart; cancel provably runs teardown;
discard leaves nothing behind on either side.

### T-P4.8 — Audit log
The append-only record (UC-O8, NFR-5) of what was captured or restored, from where, to where, and
when — plus every secret reveal from P6. No user identity is recorded, because there is none (N8).
Readable in the UI and as a plain file.

*Done when:* a capture, a restore and a reveal each produce an entry; the redaction suite covers
the audit log as an output.

---

## Testing

**Unit.** Backoff and jitter distribution against a fake clock. Error classification tables per
adapter. Breaker state machine. Checkpoint serialisation and atomicity. Ledger rendering for
every failure class.

**Architecture.** `TestNoUnwrappedRemoteCalls` — a static check that every remote call site goes
through `Doer`.

**Fault injection.** The full matrix from [01 §1.7](./01-test-strategy.md) against SFTP, SSH,
Docker and Kubernetes. The defining case: kill the process at 60% of a large fetch, relaunch,
resume, and assert the final object is byte-identical to an uninterrupted run.

**Integration.** A capture over a link that drops every 30 seconds still completes. A capture
against an endpoint that is down for two minutes opens the breaker, recovers on its own, and
completes.

**Manual.** Start a capture from a Kubernetes cluster, pull the network cable (or disable
Wi-Fi) for a minute, and watch what the UI says. The test is whether an operator would
understand what is happening and trust that waiting is the right response.

## Verification

| Requirement | Evidence |
|---|---|
| NFR-1 | The fault-injection matrix green for every transport, including `TestResume_AcrossAppRestart`. |
| NFR-5 | The audit log file from a capture + restore + reveal sequence, attached to the phase record. |
| NFR-8 | `TestResume_Converges` — interrupted and resumed transfer is byte-identical to an uninterrupted one; no duplicates. |
| UC-O1 | Activity screen screenshot with a running job showing throughput and retry state. |
| UC-O2 | `TestResume_AcrossAppRestart` plus the manual walkthrough. |
| UC-O3 | `TestCancel_RunsTeardown` — cancel a clone capture, assert no clone survives. |
| UC-O4 | `TestDiscard_RemovesCheckpointAndPartials`. |
| UC-O5 | `TestFailureRendering_AllClasses` — every injected failure renders an actionable sentence. |
| UC-O6 | The flaky-link integration test: drops every 30s, capture still completes. |
| UC-O8 | The audit log, plus redaction coverage over it. |

## Demo

Start a large capture from a remote SSH host. At 60%, kill PortCloak outright. Relaunch: the
Activity screen offers the interrupted job with what it had done and where it stopped. Resume.
It picks up from the checkpoint, finishes, and the resulting bundle is byte-identical to one
captured in a single uninterrupted run — shown by comparing digests on screen.

## Exit criteria

- [ ] Every remote call site goes through `Doer`; the architecture test enforces it.
- [ ] The fault-injection matrix is green for every transport built so far.
- [ ] A job resumes correctly across an app restart and converges.
- [ ] Cancel runs teardown; discard leaves nothing behind.
- [ ] Every failure class renders as an actionable sentence.
- [ ] The audit log records captures, restores and reveals, and is covered by the redaction suite.

## Commits

```
feat(resil): bounded retry with exponential backoff and full jitter
feat(resil): per-endpoint circuit breaker
feat(config): transfer checkpoints persisted atomically per job
feat(resil): convergent resume across transports
refactor(target,store): route every remote call through the resilience layer
test(arch): assert no remote call bypasses the resilience wrapper
feat(resil): partial-failure ledger and actionable failure rendering
feat(ui): activity screen with monitor, resume, cancel and discard
feat(obs): append-only audit log
test(resil): fault injection matrix across every transport
```

## Risks

**Retrying something unretryable.** Retrying a rejected credential wastes a minute and buries the
real message. *Mitigation:* classification is explicit per adapter and unit-tested as a table;
the default for an unclassified error is **terminal**, so a new error type surfaces rather than
silently looping.

**Checkpoints that lie.** A checkpoint written before the work commits causes resume to skip data
that was never transferred — the silent-corruption failure class, which is the worst outcome in
the tool. *Mitigation:* checkpoints are written only after a unit is durably committed, and the
kill-at-every-boundary test exists precisely to catch an off-by-one here.

**The retrofit touching everything at once.** *Mitigation:* one adapter per commit, with the
fault-injection suite run after each, so a regression is attributable.
