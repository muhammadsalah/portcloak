# P6 — Inspection

**Goal.** A snapshot stops being an opaque file. An operator can survey every snapshot across
every storage backend without holding a key, open one and read its realm settings, browse
120,000 users with instant search and faceting, see exactly which secrets it carries, reveal one
deliberately and under audit, verify integrity without restoring anything — and close it,
leaving nothing behind on disk.

This is the phase that answers the question people actually have before a migration: *is what I
need actually in there?*

**Covers.** UC-I1…UC-I13, UC-O10 · FR-V1…FR-V8, FR-V10 · NFR-9, NFR-10.

**Depends on.** P0…P5.

**Packages.** `engine/inspect`, `engine/inspect/index`, `internal/app/inspect_controller`,
`frontend/`.

---

## Tasks

### T-P6.1 — Tier 0: the library across every storage backend
List snapshots from all configured storage backends using only the secret-free sidecar manifests
(FR-V1). No key, no download, no decryption. Realm, capture time, size, counts, completeness and
provenance, sorted and filterable across backends at once.

Tier 0 is what makes the library usable at all, and its cost model — one small GET per snapshot —
is why the sidecar exists as a separate object rather than a header inside the bundle.

*Done when:* a library spanning disk, SFTP, S3 and Azure renders with no key held; 500 snapshots
list quickly; a snapshot whose sidecar is missing appears, marked as needing a deeper read,
rather than vanishing.

### T-P6.2 — Tier 1: open, verify, decrypt, detail
Download, verify the integrity tree, decrypt if needed, and read the envelope and full manifest
(FR-V2): realm settings, key providers with KIDs and algorithms, clients, identity providers,
federations, auth flows, external dependencies. Verification happens **before** anything is
rendered — a bundle that fails its checksum is never presented as readable content.

*Done when:* opening the `rich` snapshot renders full detail; a tampered bundle refuses to open
and says which artifact failed; a wrong passphrase is a clear message, not a parse error.

### T-P6.3 — Tier 2: the session-scoped index
Stream-parse `users-*.json` and the other entity files into a SQLite index at
`~/.portcloak/index/<snapshot-id>.sqlite`, with restricted permissions and
`PRAGMA secure_delete`. Streaming, one entity at a time, never the whole file in memory (NFR-9).
Progress is reported, because on the `large` fixture this takes real time and an unexplained wait
is worse than a slow one.

The credential boundary from [10 §10.3](../10-snapshot-inspection.md) is a hard rule enforced by
the schema itself: the index stores `hasPassword`, `passwordAlgo`, iteration count, `otpCount`,
`webauthnCount`, `recoveryCodes`, `requiredActions`, `origin`, `enabled`, `emailVerified` — and
**no hash value, no OTP seed, no passkey material, no secret**. There is no column for them.
That is what lets an operator answer "will this user's 2FA survive the move?" without the index
becoming a second copy of the crown jewels.

Small realms are indexed in memory and never touch disk at all.

*Done when:* the `large` fixture indexes with bounded memory and visible progress; the schema
provably has nowhere to put a secret; the index file is `0600`.

### T-P6.4 — Index lifecycle: destroyed on close
Built on open, destroyed on explicit close, on app exit, and swept on next launch if a crash
prevented either ([D9](../12-decisions.md), NFR-10). Deleting the whole `index/` directory at any
moment must always be safe.

The trade-off is stated plainly in the UI: re-opening pays the build again. That is accepted
deliberately — an index is a searchable copy of an organisation's entire user directory, and
leaving one on a workstation between sessions is a worse liability than a rebuild is an
inconvenience.

*Done when:* `TestIndexDestroyedOnClose`, `TestIndexDestroyedOnExit` and
`TestIndexSweptAfterCrash` pass, each asserting the file is gone from the filesystem.

### T-P6.5 — Browse, search and facet users
Paginated, sortable table with free-text search (FR-V3) and facets by enabled state, origin,
role, group, and 2FA presence (FR-V4). Single-user detail with attributes, role mappings, group
memberships and credential **presence** (FR-V5).

*Done when:* search and paging on 120,000 users stay responsive against an asserted latency
budget; facet counts match a query computed independently over the source export.

### T-P6.6 — Browse everything else
Clients (including whether a secret is carried), client scopes, roles, groups, key providers,
identity providers, LDAP federations and mappers, authentication flows, and external dependencies
(FR-V6, UC-I7, UC-I10).

*Done when:* every entity type in [07](../07-realm-carryover-manifest.md) is browsable and counts
reconcile with the manifest.

### T-P6.7 — Secret ledger and audited reveal
The ledger lists every secret by **location and kind, never by value** (UC-I8). Reveal is one
secret at a time, on an explicit action, and writes an audit entry naming what was revealed and
when (UC-I9, FR-V7). The revealed value never reaches a log, a progress event or an export.

*Done when:* `TestReveal_WritesAudit` and `TestReveal_NeverLogs` pass; the ledger is proven
value-free by the redaction suite.

### T-P6.8 — Verify without restoring
Recompute the integrity tree and confirm decryptability, on demand, without restoring (FR-V8,
UC-I11). Report per-artifact results, so a failure names the artifact rather than declaring the
whole bundle bad.

*Done when:* verification passes on a good bundle, and on a bundle with one flipped byte it names
exactly which artifact failed.

### T-P6.9 — Export an inspection view
Export the user list, client list, secret ledger or completeness report as CSV or JSON (FR-V10,
UC-I12), **redacted** — the exported ledger carries locations and kinds, never values. The export
is an audited action, since it creates a copy of directory data outside the tool.

*Done when:* every export type round-trips; the redaction suite covers exported files; each export
writes an audit entry.

### T-P6.10 — Close a snapshot
Explicit close (UC-I13): release the bundle, destroy the index, and say so. Closing is a visible
action rather than an implicit consequence of navigating away, because "is that copy of my user
directory gone?" deserves a definite answer.

*Done when:* close destroys the index and confirms it; navigating away without closing still
destroys it, and the sweep catches anything a crash left.

---

### T-P6.11 — Purge local working data
UC-O10. One deliberate action that clears everything PortCloak has accumulated locally:
inspection indexes, finished job records, and logs beyond the current session. It states what
will be removed and what will not — configured environments and storage definitions survive,
and no stored snapshot is ever touched, because purging local working data must never be a way
to accidentally destroy a backup.

Discarding an interrupted job's checkpoint (UC-O4) remains separate: this is housekeeping, not
job control.

*Done when:* purge removes indexes, finished job records and rotated logs; `config.yaml` and every
stored snapshot are provably untouched; the confirmation names exactly what will go.

---

## Testing

**Unit.** Sidecar parsing including malformed and missing. Streaming user parser against
`users-*.json` shapes, including a file with one enormous user. Index schema constraints. Facet
query correctness. Redaction over ledger and exports.

**Integration.** Full inspection of `minimal`, `rich`, `federated` and `large`. Tier 0 across all
four storage backends with no key held. Tier 1 against an encrypted bundle. Tier 2 on `large`
with latency and memory budgets asserted.

**Security.** `TestIndexSchemaHasNoSecretColumns` — a structural assertion over the schema, so
adding a secret column requires deliberately changing a test that says not to.
`TestIndexFileIsRemovedOnClose`. `TestReveal_WritesAudit`. `TestExport_IsRedacted`.

**Performance.** On `large`: index build time and peak memory; search and page latency budgets.
NFR-9 is a promise about responsiveness, and a promise without a number is not testable.

**Manual.** Open a `large` snapshot and use it as an operator would — find one user by partial
email, check their 2FA, check whether the client they authenticate through carries its secret.
The question is whether it feels like a directory browser or like a file viewer.

## Verification

| Requirement | Evidence |
|---|---|
| FR-V1 · UC-I1 | `TestLibrary_AllBackends_NoKey` — library renders across four backends with no key held. |
| FR-V2 · UC-I2 | `TestOpen_Tier1_Rich`, plus a tampered-bundle refusal test. |
| FR-V3 · UC-I4 | `TestUsers_SearchAndPage_Large` with the latency budget asserted. |
| FR-V4 · UC-I5 | `TestUsers_Facets_MatchIndependentQuery`. |
| FR-V5 · UC-I6 | `TestUser_Detail_CredentialPresence`. |
| FR-V6 · UC-I7 · UC-I10 | `TestBrowse_AllEntityTypes` — counts reconcile with the manifest. |
| FR-V7 · UC-I8 · UC-I9 | `TestReveal_WritesAudit`, `TestReveal_NeverLogs`, `TestLedger_ContainsNoValues`. |
| FR-V8 · UC-I11 | `TestVerify_GoodBundle` and `TestVerify_NamesFailingArtifact`. |
| FR-V10 · UC-I12 | `TestExport_AllViews` and `TestExport_IsRedacted`. |
| UC-I3 | `TestIndexBuild_Large` — bounded memory, visible progress. |
| UC-I13 | `TestIndexDestroyedOnClose` / `OnExit` / `SweptAfterCrash`. |
| NFR-9 | Recorded latency and memory numbers on `large`, attached to the phase record. |
| NFR-10 | `TestIndexSchemaHasNoSecretColumns` plus the three destruction tests. |
| UC-O10 | `TestPurge_RemovesWorkingDataOnly` — config and stored snapshots asserted untouched. |

## Demo

Open the library with no keys entered: every snapshot across disk, MinIO and Azurite is listed
with realm, counts and completeness. Open the `large` one — it verifies, decrypts, and builds its
index with a progress bar. Search 120,000 users for a partial email; the result is instant. Open
that user: password algorithm `pbkdf2-sha512`, two OTP enrolments, one passkey — presence, never
values. Open the secret ledger: eleven secrets, by location and kind. Reveal one; it appears once,
and an audit entry records it. Close the snapshot, then show `~/.portcloak/index/` is empty.

## Exit criteria

- [ ] The library lists across all four storage backends with no key held.
- [ ] A snapshot verifies and decrypts before any content is rendered.
- [ ] 120,000 users are searchable and pageable within the asserted budget.
- [ ] The index schema provably cannot hold a secret.
- [ ] The index is destroyed on close, on exit, and by the crash sweep.
- [ ] Reveal is audited; ledger and exports are redacted.
- [ ] Purge clears working data and provably leaves config and stored snapshots alone.

## Commits

```
feat(inspect): tier 0 library from sidecar manifests, no key required
feat(inspect): tier 1 open with verification and decryption before render
feat(inspect/index): streaming projection into a session-scoped sqlite index
feat(inspect/index): lifecycle — destroy on close, on exit, and sweep after crash
feat(inspect): user search, paging and facets
feat(inspect): browse clients, keys, federations, flows and dependencies
feat(inspect): secret ledger and audited single-secret reveal
feat(inspect): verify a snapshot without restoring it
feat(inspect): redacted CSV and JSON export of inspection views
feat(app): purge local working data without touching config or snapshots
test(inspect): schema assertion that the index cannot hold a secret
```

## Risks

**The index quietly becoming a secret store.** Someone adds `passwordHash` to help a feature.
*Mitigation:* `TestIndexSchemaHasNoSecretColumns` fails on any new column not on an allowlist,
so the addition requires editing a test that explains why not to.

**Index build time on very large realms feeling like a hang.** *Mitigation:* progress from the
first row, a visible estimate, and the ability to browse Tier 1 detail while Tier 2 builds.

**SQLite full-text search pulling in cgo.** *Mitigation:* `modernc.org/sqlite` with FTS5 built
in; if that proves inadequate, fall back to an indexed `LIKE` over a normalised lowercase column,
which the latency budget on `large` will decide — not a preference.
