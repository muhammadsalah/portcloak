<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# 12 — Design Decisions

The nine open questions raised at the end of the first design pass have been answered. This is
the decision record: what was decided, why it holds, and what it costs. Each entry names the
documents that implement it.

---

## D1 — Sessions are out of scope

**Decision.** PortCloak does not capture or replay online or offline sessions. Users
re-authenticate after a restore.

**Rationale.** Sessions live in Infinispan caches rather than in the realm representation, are
cluster-topology dependent, and do not reliably survive recreation on a different instance. A
best-effort implementation would fail precisely in the situations people depend on it.

**What replaces it.** The need behind "session portability" is usually **token continuity** —
tokens minted before the move still being accepted after it. That is delivered properly by
carrying the realm's **signing keys** (FR-F4), which is both more dependable and fully within
reach of the offline export.

**Consequences.** The Admin API's role shrinks to verification and dependency detection; there is
no `SessionHarvester`. The completeness report gains an `outOfScope` category so a healthy
snapshot never reads as partially broken.

**Implemented in:** [01 N5](./01-vision-and-requirements.md), [07 §L](./07-realm-carryover-manifest.md), [02 §2.5](./02-architecture.md)

---

## D2 — Offline `kc.sh export` is the default, run in an ephemeral clone

**Decision.** Offline `kc.sh export` is the primary capture mode on every target.

- **Docker / Kubernetes / OpenShift** — the export runs inside an **ephemeral clone**: a new
  container or Job created from the **same image and configuration**, started **hung**
  (`sleep infinity`), exec'd into, and destroyed afterwards. The serving instance is **never**
  exec'd into, written to, or made to compete for resources.
- **Local / SSH** — no clone is applicable, so isolation comes from **automatically allocated
  free ports** (`--http-port`, `--https-port`, `--http-management-port`), preventing the port
  collision that makes offline export exit non-zero when an instance is already running.

**Rationale.** Offline export boots its own Keycloak runtime. Doing that inside a live container
means CPU/memory contention, port collisions, writes into a serving filesystem, and exposure to
liveness probes killing the container mid-export.

**Consequences.** Two new modules (`Ephemeral Clone Manager`, `Port Allocator`) and a raised
privilege floor on the source: Kubernetes needs `create`/`delete` on jobs/pods and `create` on
`pods/exec`. That is more than read-only, and it is the stated price of not touching production.
Two traps are called out explicitly: **selector labels must be stripped** from a cloned spec or
the production Service will route live traffic into the hung pod, and **teardown must be
unconditional** or a clone holding DB credentials is left parked.

**Implemented in:** [03 §3.1, §3.3](./03-capture-targets.md), diagram [`15-ephemeral-capture`](./diagrams/15-ephemeral-capture.puml)

---

## D3 — No selective restore

**Decision.** Restore is whole-realm. Individual users or clients cannot be cherry-picked out of
a snapshot.

**Rationale.** Partial restores produce partial state — a user without their role mappings, a
client without its scopes — and the resulting failures are subtle and hard to attribute.

**Consequences.** The inspection feature stays read-only: it explains a snapshot, it does not
become a staging area for partial imports.

**Implemented in:** [01 N6](./01-vision-and-requirements.md), [09 §9.3](./09-workflows-and-ui.md)

---

## D4 — Themes and provider JARs are detected and reported, never migrated

**Decision.** Custom themes, provider/SPI JARs and referenced keystore files are **identified and
reported** as restore preconditions. PortCloak does not attempt to move them.

**Rationale.** These are deployment artifacts, not realm data. Migrating them would mean shipping
binaries between environments — a different problem with different risks.

**Why reporting matters.** A realm referencing a missing theme or a missing authenticator SPI
**imports successfully and then fails at login**. That is the worst failure mode available, and
surfacing the dependency list before import is what prevents it.

**Consequences.** New `DependencyScanner`, a `dependencies.json` in the bundle, and an
informational step in the restore wizard. It reports rather than gates: the Operator manages
these environments and is assumed to know what is deployed where.

**Implemented in:** [01 N7, FR-D1/D2](./01-vision-and-requirements.md), [07 §M](./07-realm-carryover-manifest.md)

---

## D5 — One snapshot contains exactly one realm

**Decision.** No multi-realm bundles. Capturing several realms produces several independent
snapshots.

**Rationale.** Each realm becomes independently restorable, verifiable, retainable and
access-controllable. It also removes a whole class of partial-failure handling: one realm failing
cannot damage another's bundle.

**Consequences.** Bundle layout flattens (no `realms/<name>/` nesting); the storage backend key prefix
partitions cleanly per realm; `--realm` is always passed to the export.

**Implemented in:** [01 FR-S6](./01-vision-and-requirements.md), [06 §6.1](./06-snapshot-and-manifest.md), [04 §4.1](./04-storage-backends.md)

---

## D6 — Wails v3

**Decision.** Target Wails v3.

**Rationale.** Newer application and multi-window APIs, and building on v3 now avoids a migration
later for a project that has not shipped yet.

**Implemented in:** [02 §2.8](./02-architecture.md), [09 §9.6](./09-workflows-and-ui.md)

---

## D7 — SQLite for the inspection index

**Decision.** The snapshot inspection index is SQLite, via the pure-Go `modernc.org/sqlite`
driver.

**Rationale.** Indexed queries and **FTS** are what make substring search across 100k+ usernames
and emails feel instant — the capability that distinguishes real browsing from a paged dump. The
pure-Go driver keeps the desktop binary cgo-free and cross-compilable.

**Implemented in:** [02 §2.8](./02-architecture.md), [10 §10.3](./10-snapshot-inspection.md)

---

## D8 — Encryption is opt-in, and promoted

**Decision.** Snapshot encryption is **not** enabled by default. It is presented prominently —
the capture wizard shows the toggle **on** and requires a deliberate action to disable it — but
it can be declined.

**Rationale.** Key management is a real operational burden, and forcing it would push users
toward workarounds. The choice belongs to the operator.

**What this obliges PortCloak to do.** An unencrypted `.pck` holds unmasked client secrets, LDAP
bind credentials, IdP secrets, SMTP passwords and **RSA private signing keys in the clear** —
possession of it is equivalent to possession of the realm. So the decision is made *visibly*:
explicit confirmation at capture, a persistent warning badge on the snapshot, a banner on open
and restore, and the choice recorded in the audit log. The storage backend also stops being untrusted, and
the docs say so rather than implying blanket protection. Organizations that want the choice
removed can mark encryption **mandatory** on a storage definition.

**Implemented in:** [06 §6.3](./06-snapshot-and-manifest.md), [08 §8.2](./08-security.md), [04 §4.6](./04-storage-backends.md)

---

## D9 — The inspection index is built on open and destroyed on close

**Decision.** The index is session-scoped: created when a snapshot is opened, dropped and
securely deleted when it is closed. No cross-session cache.

**Rationale.** An index is a searchable copy of a realm's entire user directory — usernames,
emails, group memberships. Leaving one on an operator's workstation between sessions is a
standing liability.

**Trade-off, accepted deliberately.** Re-opening the same snapshot pays the index build again.
The build streams and reports progress, so the cost is visible and bounded — a better bargain
than persistent PII on disk. Small realms may be indexed entirely in memory.

**Implemented in:** [01 NFR-10](./01-vision-and-requirements.md), [10 §10.3](./10-snapshot-inspection.md), [08 §8.8](./08-security.md)

---

## Decisions still open

None blocking. Items that will want revisiting once there is running code:

- **Index build performance** at the 500k-user end — whether streaming insert batching is enough
  or the build needs parallel parsing across `users-*.json` files.
- **Clone resource sizing** — whether to inherit the serving pod's requests/limits verbatim or
  apply an export-tuned resource preset (the export JVM has a different memory shape than a serving one).
- **Restore-side dependency verification** — whether PortCloak should actively *check* the
  destination for the reported themes/JARs rather than only listing them for the operator.
