<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# P8 — Verification & Dependency Detection

**Goal.** Close the two honesty gaps. First: prove that the secrets in a bundle are **real
values, not masked placeholders** — because a client secret exported as `**********` imports
perfectly and then fails silently at the first authentication. Second: detect and report the
assets a realm depends on that live outside the realm — custom themes, provider and SPI JARs,
keystores — so an operator knows what the destination still needs.

Both are done through the Admin REST API, which is **strictly secondary and entirely optional**
([02 §2.5](../02-architecture.md)). If it is unreachable, capture still succeeds and the
completeness report records that verification was skipped.

**Covers.** UC-C8, UC-C9, UC-I10 · FR-C6, FR-D1.

**Depends on.** P0…P7.

**Packages.** `engine/admin`, `engine/manifest` (completeness), `frontend/`.

---

## Why last, when it protects the most important claim

Masked secrets are a real hazard, so the instinct is to build this first. Two reasons not to.

**It is only meaningful once fidelity is otherwise complete.** A masked-secret detector is
useful when everything else is right and this is the remaining failure mode. Built earlier, it
would have been tuned against a capture pipeline that was still changing shape.

**The hazard is already contained by the fixture matrix.** From P2, the `legacy` fixture exercises
the version whose masking behaviour motivates FR-C6, and the fidelity assertions compare
captured values against the source. A masked secret would already fail those tests. What P8 adds
is detection **at capture time, on an operator's own realm**, where no fixture exists — which is
protection in production rather than protection in CI.

The risk of leaving it late is therefore bounded and known, and the phase is small.

## Tasks

### T-P8.1 — Admin REST client
Authenticate against the target's Admin API using credentials on the environment, with the same
resilience wrapper as everything else. Unreachable is a **normal, expected outcome**, not an
error: an offline capture from a stopped Keycloak has no Admin API by definition, and that path
must stay first-class.

*Done when:* the client authenticates across the version matrix; unreachable degrades cleanly
without failing the capture.

### T-P8.2 — Secret verification
UC-C8, FR-C6. For each secret in the ledger, confirm the exported value is a real value rather
than a mask. A masked secret is flagged `partial` with the reason, rather than shipping as a dud
that will fail at first authentication.

The check is on **shape and provenance**, never on comparing values — the verifier must not
become a second path by which a secret is fetched and handled.

*Done when:* against the `legacy` fixture, a masked secret is detected and flagged `partial`
with a reason naming the client; against `rich`, all secrets verify; with the Admin API down,
the report says verification was skipped, not that secrets are missing.

### T-P8.3 — Dependency scanner
UC-C9, FR-D1. Enumerate custom themes and deployed provider/SPI JARs, and cross-reference them
against what the realm actually references — its login, account, admin and email themes, its
authenticator SPIs, its keystore paths. Record each with type, name and detected path in the
manifest ([07 §M](../07-realm-carryover-manifest.md)).

They are **detected and reported, never migrated** ([D4](../12-decisions.md)). They appear in the
completeness report as `outOfScope` with a note, never as `missing`.

*Done when:* the `themed` fixture's theme and provider JAR are both detected with correct type,
name and path; both are `outOfScope`, not `missing`; a realm using only built-in themes reports
no dependencies rather than a false positive.

### T-P8.4 — Surface both in capture and inspection
The capture result shows verification outcome and detected dependencies. The inspector's
dependency view (UC-I10) shows the same for a stored snapshot, and it is the same data the
restore preconditions step reads in P7 — one source, three views.

*Done when:* verification and dependency results appear in the capture result, the inspector and
the restore preconditions, and are provably the same records.

---

## Testing

**Unit.** Mask detection across the shapes different Keycloak versions produce. Dependency
cross-referencing, including a theme that is deployed but unreferenced (not a dependency) and a
theme that is referenced but absent (a dependency, and the interesting case). Completeness
categorisation as `outOfScope`.

**Integration.** `legacy` fixture: a masked secret detected and flagged. `rich`: all secrets
verify. `themed`: theme and provider JAR detected with correct paths. Admin API down: capture
succeeds, report says verification was skipped.

**Manual.** Capture a realm with the Admin API deliberately unreachable and read the completeness
report. The question is whether "verification skipped" reads as a normal condition rather than as
a fault — it is a normal condition, and the report must not imply otherwise.

## Verification

| Requirement | Evidence |
|---|---|
| FR-C6 · UC-C8 | `TestSecretVerification_DetectsMask` on `legacy`; `TestSecretVerification_AllRealOnRich`; `TestSecretVerification_SkippedWhenAdminUnreachable`. |
| FR-D1 · UC-C9 | `TestDependencyScan_Themed` — theme and provider JAR with correct type, name and path; `TestDependencyScan_NoFalsePositives` on a built-in-themes-only realm. |
| UC-I10 | `TestInspector_DependencyView_MatchesManifest` — the same records in capture, inspection and restore. |
| [D4](../12-decisions.md) | `TestDependencies_AreOutOfScopeNotMissing`. |
| [02 §2.5](../02-architecture.md) | `TestCapture_SucceedsWithoutAdminAPI` — the Admin API is genuinely optional. |

## Demo

Capture a realm from an older Keycloak whose Admin API masks client secrets. The completeness
report flags one client's secret as `partial` and says which client and why — before the bundle
is ever trusted for a migration. Then capture the `themed` realm: the report lists a custom login
theme and a provider JAR, with their paths, marked out of scope by design. Restore that snapshot
into a clean Keycloak and watch the preconditions step show the same two items, from the same
records.

## Exit criteria

- [ ] Masked secrets are detected and flagged `partial` with a reason.
- [ ] Capture succeeds and reports honestly when the Admin API is unreachable.
- [ ] Themes and provider JARs are detected with type, name and path.
- [ ] Dependencies are `outOfScope`, never `missing`.
- [ ] Capture, inspection and restore show the same dependency records.

## Commits

```
feat(admin): admin rest client with reachability treated as optional
feat(admin): masked-secret detection flagged as partial with a reason
feat(admin): theme and provider JAR detection cross-referenced with realm usage
feat(manifest): dependencies recorded as out of scope, never missing
feat(ui): verification and dependency results in capture, inspection and restore
test(admin): masking behaviour across the version matrix
```

## Risks

**Masking behaviour varies by version and endpoint.** *Mitigation:* detection is by shape across a
tested corpus, and an unrecognised shape is reported as "could not verify" rather than assumed
good. Assuming good is how a dud secret ships.

**Dependency detection producing noise.** Every deployed theme reported as a dependency would
make the list worthless. *Mitigation:* cross-reference against what the realm actually references,
and cover the deployed-but-unreferenced case explicitly in tests.
