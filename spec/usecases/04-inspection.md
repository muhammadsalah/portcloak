<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# 04 — Inspection

> A snapshot is a **queryable artifact**, not a black box. Inspection is **tiered**: listing needs
> no key at all, details need decryption, and browsing builds a **throwaway SQLite index** that is
> destroyed when the snapshot is closed.

![Inspection & restore use cases](./diagrams/png/uc-04-inspection-restore.png)

*Source: [`uc-04-inspection-restore.puml`](./diagrams/uc-04-inspection-restore.puml) · [SVG](./diagrams/svg/uc-04-inspection-restore.svg)*

| Tier | Trigger | Needs a key? |
|------|---------|:------------:|
| **0 — Listing** | Opening the library | **No** — served from the non-secret sidecar manifest |
| **1 — Detail** | Opening one snapshot | Yes — decrypt + verify once |
| **2 — Index** | Opening Users, or searching | Yes (already unlocked) — builds `~/.portcloak/index/<id>.sqlite` |

---

## UC-I1 — Browse the snapshot library

**Goal.** See every snapshot across every configured storage.
**Preconditions.** At least one storage is defined.

**Main success scenario**
1. Operator opens *Snapshots*.
2. PortCloak lists each storage's folder/prefix and reads the **non-secret sidecar manifests**.
3. Rows show realm, capture time, source environment and execution mode, user count,
   completeness badge, encryption state and storage.
4. **No decryption key is requested** — the whole library is browsable without one.

**Alternate flows**
- **A1 — Filter** by realm, storage, completeness or encryption state.
- **A2 — Unencrypted snapshots** carry a persistent warning badge.

**Exceptions**
- **E1 — A storage is unreachable.** Its snapshots are omitted and the storage is shown as
  *unreachable* — the list is never silently short.
- **E2 — Sidecar missing or corrupt.** The entry is listed as *unreadable metadata*; it can still
  be opened (UC-I2), which will verify integrity properly.

**Postconditions.** Read-only.
**Covers.** FR-V1, FR-S5.

---

## UC-I2 — Open a snapshot and view its details

**Goal.** Read the full manifest for one snapshot.

**Main success scenario**
1. Operator opens a snapshot.
2. PortCloak fetches the bundle (resumable), **decrypts** it if encrypted, and **verifies the
   integrity tree**.
3. It stream-parses the envelope and realm manifest.
4. It shows realm settings, key providers (KIDs, whether private material travelled), clients,
   client scopes, roles, groups, identity providers, LDAP federation, auth flows, the secret
   ledger, external dependencies, completeness and provenance.
5. The **token continuity** indicator is shown when the active signing key travelled.

**Alternate flows**
- **A1 — Unencrypted snapshot.** No key requested; the warning badge stays visible.
- **A2 — Passphrase or age identity required.** Prompted once per open.

**Exceptions**
- **E1 — Wrong passphrase / no matching identity.** Reported plainly; **non-retryable**.
- **E2 — Integrity mismatch.** The snapshot opens in a **read-only, clearly-flagged degraded
  state** for diagnosis, and **restore is blocked** (UC-R1 E1).
- **E3 — Download interrupted.** Resumable; the snapshot does not open until complete.

**Postconditions.** Snapshot open for this session. A Tier-1 view is available.
**Covers.** FR-V2, FR-R4.

---

## UC-I3 — Build the inspection index

**Goal.** Make a realm with 100k+ users browsable without loading it into memory.
**Trigger.** Operator opens the *Users* tab or searches, on a snapshot open at Tier 1.

**Main success scenario**
1. PortCloak **stream-parses** `users-*.json` one user object at a time — bounded memory
   regardless of realm size.
2. It writes flattened projection rows into a **new SQLite file** at
   `~/.portcloak/index/<snapshot-id>.sqlite`, separate from tool configuration.
3. It indexes clients, roles, groups, keys and dependencies alongside.
4. Progress is reported like any other job and the build is **cancellable**.
5. Facets and counts are computed during the build.

**Alternate flows**
- **A1 — Small realm.** Indexed entirely in memory; nothing touches disk.
- **A2 — Index already built this session.** Reused immediately.

**Exceptions**
- **E1 — Cancelled mid-build.** The partial index file is deleted; the tab returns to its
  un-indexed state rather than showing partial results as if complete.
- **E2 — Disk full.** Build fails with the space required; the partial file is removed.

**Postconditions.** A session-scoped index exists. **It records credential *presence* only** —
`hasPassword` and algorithm, OTP count, passkey count, recovery codes — and **never** hash
values, OTP seeds or passkey material.
**Covers.** NFR-9, NFR-11.

---

## UC-I4 — Search users within a snapshot

**Goal.** Find a specific account.

**Main success scenario**
1. Operator types into the search box on the Users tab.
2. PortCloak queries the index across username, email, first/last name and user ID.
3. Matching rows are paged back with a total count.

**Alternate flows**
- **A1 — Search combined with facets.** Filters and the query intersect.
- **A2 — No matches.** Stated explicitly, with the active filters listed so an over-narrow filter
  set is obvious.

**Postconditions.** Read-only.
**Covers.** FR-V3, NFR-9.

---

## UC-I5 — Filter and facet users

**Goal.** Answer questions about population, not just individuals.

**Main success scenario**
1. Facets are shown with counts: enabled/disabled, origin (local vs LDAP-federated), realm role,
   client role, group, second factor (none / OTP / passkey), pending required action.
2. Operator selects facets; the table and total update; active filters appear as removable chips.

**Alternate flows**
- **A1 — Origin facet.** Makes visible how many users are **LDAP-federated**, which matters
  because those users are not duplicated into the export and need LDAP reachable at the
  destination.

**Postconditions.** Read-only.
**Covers.** FR-V4.

---

## UC-I6 — View a single user's detail

**Goal.** Understand one account fully.

**Main success scenario**
1. Operator opens a user row.
2. PortCloak shows attributes, realm and client role mappings, group memberships, federated
   identity links, required actions, and **credential presence**: has password and which hashing
   algorithm, OTP enrolments, passkey count, recovery codes.
3. Every credential is presented as presence and metadata. **No credential value is ever shown**,
   and there is no action that would reveal one.

**Postconditions.** Read-only.
**Covers.** FR-V5.

---

## UC-I7 — Browse clients, keys, federations and flows

**Goal.** Inspect the non-user parts of the realm.

**Main success scenario**
1. Operator switches tabs: Clients, Client scopes, Roles, Groups, Identity providers, User
   federation, Keys, Auth flows, External dependencies.
2. Each shows the decision-relevant column: **`secretPresent`** for clients, **`privateCarried`**
   for keys (the token-continuity signal), `bindCarried` for LDAP, `secretCarried` for IdPs,
   and *provision manually* for dependencies.

**Postconditions.** Read-only.
**Covers.** FR-V6.

---

## UC-I8 — View the secret ledger

**Goal.** Audit exactly which secrets a snapshot carries.

**Main success scenario**
1. Operator opens *Secret ledger*.
2. Every carried secret is listed by **type and location** — client secret, LDAP bind, IdP secret,
   SMTP password, key private material, authenticator config — with `carried` and `masked` flags.
3. **No values are shown.** The ledger is safe to read, screenshot and export.

**Postconditions.** Read-only; nothing sensitive disclosed.
**Covers.** FR-M1, FR-V6.

---

## UC-I9 — Reveal a single secret

**Goal.** Read one actual secret value, deliberately.

**Preconditions.** Snapshot open at Tier 1; reveal not disabled in preferences.

**Main success scenario**
1. Operator chooses *Reveal* on one ledger entry and optionally gives a reason.
2. PortCloak decrypts **just that field**.
3. The value is shown transiently with copy-to-clipboard, and auto-hides.
4. An **audit entry** records what was revealed, when, and the reason. There is no user account
   to record (N8) — the entry captures the action and the machine.

**Alternate flows**
- **A1 — Reveal disabled in preferences.** The action is absent, so a snapshot can be inspected
  without secret extraction being possible.

**Exceptions**
- **E1 — Snapshot unencrypted.** Values are already in the clear; the UI says so rather than
  implying the reveal added protection.

**Postconditions.** One value disclosed and audited.
**Covers.** FR-V7, NFR-3, NFR-5.

---

## UC-I10 — Review external dependencies

**Goal.** Know what must exist at the destination before importing.

**Main success scenario**
1. Operator opens *External dependencies*.
2. Each detected theme, provider JAR and keystore file is listed with its type, name, detected
   path and the action *provision manually at destination*.
3. The consequence is stated: a realm referencing a missing theme or authenticator SPI **imports
   cleanly and then fails at login**.

**Alternate flows**
- **A1 — Detection was skipped at capture.** Shown as *not checked*, never as *none*.

**Postconditions.** Read-only; feeds UC-R2.
**Covers.** FR-D2, FR-V6.

---

## UC-I11 — Verify a snapshot without restoring

**Goal.** Answer "is this backup good?" safely.

**Main success scenario**
1. Operator chooses *Verify*.
2. PortCloak fetches (or reuses) the bundle, decrypts it, recomputes the **integrity tree** and
   compares every artifact digest and the root.
3. It reports pass/fail per artifact. **No environment is contacted.**

**Exceptions**
- **E1 — Mismatch.** The failing artifacts are named and the snapshot is marked as failing
  verification; restore is blocked until re-verified.

**Postconditions.** Verification result recorded. Nothing changed anywhere.
**Covers.** FR-V8, NFR-2.

---

## UC-I12 — Export an inspection view

**Goal.** Get evidence out for a ticket or an audit.

**Main success scenario**
1. Operator chooses *Export* on a table view and picks CSV or JSON.
2. PortCloak writes the **currently filtered** rows, applying **the same redaction rules as the
   UI** — presence, never values.
3. The export action is itself audited.

**Alternate flows**
- **A1 — Secret ledger export.** Types and locations only.

**Exceptions**
- **E1 — Destination not writable.** Reported with the path.

**Postconditions.** A redacted file exists; exporting never becomes a secret-exfiltration path.
**Covers.** FR-V10.

---

## UC-I13 — Close a snapshot

**Goal.** Leave nothing behind after inspecting.

**Trigger.** Operator closes the snapshot, or quits the app.

**Main success scenario**
1. PortCloak drops the index tables and **securely deletes** `~/.portcloak/index/<id>.sqlite`.
2. Decrypted working files are shredded.
3. The library returns to Tier 0.

**Alternate flows**
- **A1 — App quit with a snapshot open.** Same teardown runs on exit.
- **A2 — Crash.** The orphaned index file is swept on next launch; deleting the whole `index/`
  directory is always safe.

**Postconditions.** **No usernames, emails or group data persist on disk between sessions.**
Re-opening pays the index build again — accepted deliberately (NFR-10).
**Covers.** NFR-10, NFR-11.
