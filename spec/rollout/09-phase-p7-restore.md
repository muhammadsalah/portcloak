# P7 — Restore

**Goal.** The loop closes. A snapshot becomes a live realm on a target of the operator's
choosing: dependencies surfaced, changes previewed against the real destination, a strategy
chosen, the import applied, and the result validated. This is where the fidelity claims of P2
stop being assertions about a file and become a realm people can log in to.

**Covers.** UC-R1…UC-R8 · FR-R1, FR-R2, FR-R3, FR-R4 · FR-D2.

**Depends on.** P0…P6.

**Packages.** `engine/kc` (import), `engine/admin` (partial import, validation),
`engine/orchestrator` (restore path), `internal/app/restore_controller`, `frontend/`.

---

## Tasks

### T-P7.1 — Verify and decrypt before anything is written
FR-R4. Integrity tree recomputed and decryption confirmed **before** the target is touched.
A restore that half-writes a corrupted realm is worse than one that never starts, so this
gate is unconditional and cannot be skipped.

*Done when:* a tampered bundle is refused before any connection to the target is opened —
asserted by a test that fails if a request reaches the target at all.

### T-P7.2 — Restore preconditions, informative only
UC-R2 as specified: list the snapshot's external dependencies — themes, provider JARs, keystores
— each with its type, name and detected path, and state the consequence plainly, that a realm
referencing a missing theme or authenticator SPI **imports cleanly and then fails at login**.
Integrity and decryption results are shown alongside as already-passed items.

The step is **informative only**. Nothing is checked off and nothing is blocked; *Next* stays
enabled. The operator manages these environments and is assumed to know what is deployed where.

*Done when:* dependencies from the `themed` fixture are listed with type, name and path;
nothing blocks; `TestPreconditions_NeverBlocks` asserts *Next* is enabled even when every
dependency is missing.

### T-P7.3 — Dry-run diff against the destination
FR-R2, UC-R3. Diff the snapshot's manifest and projections against the **live target realm** —
what would be created, what would be overwritten, what would be left alone — before writing
anything. This is restore preview against a real destination, and it is deliberately not
snapshot-to-snapshot comparison, which is out of scope ([FR-V9 withdrawn](../11-traceability.md)).

Diffing an empty or nonexistent target realm is the common case and must read as "everything is
new", not as an error.

*Done when:* the diff is accurate against a modified copy of the `rich` realm, category by
category; a nonexistent target realm produces a clean all-new diff.

### T-P7.4 — Import strategies
FR-R3, UC-R4: overwrite, skip, merge. Each is explained in terms of what happens to an existing
resource, not in terms of a Keycloak flag name. The chosen strategy is reflected in the dry-run
diff, so the preview shown is the preview of the strategy actually selected — a preview computed
under a different strategy would be worse than no preview.

*Done when:* all three strategies produce the documented outcome against a target that already
holds a conflicting realm, and the diff changes when the strategy changes.

### T-P7.5 — Apply the import
FR-R1, UC-R5. Offline `kc.sh import` for offline targets; Admin API `partialImport` where a
running server is the right path. The same execution machinery as capture — including ephemeral
clones on Docker and Kubernetes, so the serving instance is not disturbed by a restore either —
and the same resilience layer, so a dropped connection during a restore behaves like a dropped
connection during a capture.

Progress is per-category, because "importing" on a 120,000-user realm is not a status.

*Done when:* restore succeeds through all four target kinds; teardown is proven on every exit
path, as in P3.

### T-P7.6 — Cancel a restore
UC-R8. Cancel propagates by context, runs teardown, and reports honestly **what had already been
written** — a restore cannot always be unwound, and pretending otherwise would leave an operator
with a wrong mental model of their own system.

*Done when:* cancelling mid-import leaves no clone and produces an accurate account of what was
applied before the cancel took effect.

### T-P7.7 — Post-restore validation
UC-R6. Re-read the destination and compare against the manifest: entity counts, key providers
present with the same KIDs, clients present, federations configured. Report per category.

The assertion that matters, and the one the whole tool has been building toward: **a user who
could authenticate on the source can authenticate on the destination with the same password and
the same OTP, and a token signed before the move still verifies after it.**

*Done when:* validation reconciles every category on the `rich` and `federated` fixtures, and the
authentication round trip passes.

### T-P7.8 — Restore into a freshly provisioned Keycloak
UC-R7. The empty-destination case: a brand-new Keycloak with nothing in it. Preconditions are
softer here by nature — there is nothing to conflict with — and the flow should not demand
answers to questions that only matter when overwriting.

*Done when:* a blank Keycloak receives the `rich` realm and the authentication round trip passes
against it.

### T-P7.9 — Restore wizard
Snapshot → destination environment → preconditions → dry run → strategy → apply → result. Live
progress, and a result screen that states what was written, what was validated, and anything the
destination still needs from the operator.

*Done when:* a restore runs end to end from the UI against all four target kinds.

---

## Testing

**Unit.** Diff computation per category, including the empty-target case. Strategy semantics
tables. `kc.sh import` invocation building and output parsing. Validation reconciliation.

**Integration — the round trip.** This phase completes the third leg of the fidelity assertion
from [01 §1.5](./01-test-strategy.md). For `rich` and `federated`: capture → restore into a blank
Keycloak → re-export from the destination → compare inventories with the original. Plus the
authentication round trip: password login, TOTP login, and verification of a token signed by the
source's key against the destination.

**Integration — targets.** Restore through all four target kinds, with the do-no-harm suite from
P3 applied to the restore path as well.

**Fault injection.** Drop the connection mid-import; assert resume or a clear, accurate account
of partial application — never a silent partial success reported as complete.

**Manual.** Restore into a Keycloak that is missing the theme the realm references. The import
succeeds; the realm then fails at login. Confirm the preconditions step warned about exactly this
and did not block — the specified behaviour, demonstrated end to end.

## Verification

| Requirement | Evidence |
|---|---|
| FR-R1 · UC-R1 · UC-R5 | `TestRestore_AllTargets` — all four kinds. |
| FR-R2 · UC-R3 | `TestDryRun_AccurateAgainstModifiedRealm` and `TestDryRun_EmptyTarget`. |
| FR-R3 · UC-R4 | `TestStrategies_OverwriteSkipMerge` plus `TestDryRun_ReflectsStrategy`. |
| FR-R4 | `TestRestore_RefusesTamperedBundleBeforeContactingTarget`. |
| FR-D2 · UC-R2 | `TestPreconditions_ListsDependencies` and `TestPreconditions_NeverBlocks`. |
| UC-R6 | `TestPostRestoreValidation_Reconciles` and `TestAuthRoundTrip_PasswordOTPAndTokenSignature`. |
| UC-R7 | `TestRestore_IntoBlankKeycloak`. |
| UC-R8 | `TestCancelRestore_ReportsWhatWasApplied`. |
| FR-F1…F9 (round trip) | `TestFidelity_RoundTrip_Rich` and `_Federated` — the third leg, completing the P2 evidence. |

## Demo

Take the snapshot captured in P2. Restore it into a blank Keycloak in a `kind` cluster. The
preconditions step lists a custom theme the destination does not have and says plainly what will
happen — and lets you continue. The dry run shows 12 clients, 4 key providers and 120,000 users,
all new. Apply. Then log in to the destination as a user from the source, with their original
password and their original TOTP code from the same authenticator app. Finally, take a token
issued by the *source* before the migration and verify it against the destination's JWKS — it
validates, because the signing keys came with the realm.

## Exit criteria

- [ ] Restore works through all four target kinds.
- [ ] Verification and decryption gate the restore before the target is contacted.
- [ ] Preconditions are informative and never block.
- [ ] The dry run is accurate and reflects the selected strategy.
- [ ] The full round trip passes on `rich` and `federated`.
- [ ] Password, OTP and token-signature continuity are all proven.
- [ ] Cancel reports accurately what was already applied.

## Commits

```
feat(restore): verify and decrypt before the target is contacted
feat(restore): informative dependency preconditions that never block
feat(restore): dry-run diff against the live destination realm
feat(restore): overwrite, skip and merge strategies reflected in the preview
feat(kc): kc.sh import invocation building and output parsing
feat(restore): apply through all four target kinds, with ephemeral clones
feat(restore): cancellation that reports what was already applied
feat(restore): post-restore validation against the manifest
feat(ui): restore wizard and result screen
test(fidelity): capture-restore-reexport round trip and the auth continuity check
```

## Risks

**Partial application on failure.** Keycloak's import is not transactional; a failure halfway
leaves a partly-populated realm. *Mitigation:* we do not pretend to roll back. We report precisely
what was applied, and the dry run tells the operator beforehand what a partial application would
touch. An honest account beats a fictional rollback.

**`partialImport` semantics differ from `kc.sh import`.** The same strategy name can mean
different things on the two paths. *Mitigation:* the strategy table is tested against **both**
paths, and where they genuinely cannot be reconciled, the UI names which path will be used and
what it will do.

**The auth round trip is the hardest test to write and the most valuable.** *Mitigation:* budget
for it explicitly. It needs a scripted TOTP client and a token captured before the move. It is
the acceptance test for the entire product, and it should be written first in this phase, not
last.
