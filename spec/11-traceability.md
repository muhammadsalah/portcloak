<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# 11 — Requirements Traceability Matrix

Every requirement from [01](./01-vision-and-requirements.md) maps to the module(s) that
satisfy it and the document that designs it. This closes the loop: no requirement is
orphaned, and no module exists without a requirement.

## 11.1 Functional — Capture

| Req | Description | Module(s) | Design doc |
|-----|-------------|-----------|-----------|
| FR-C1 | Capture from local KC | Target/Local, Kc CLI Driver | [03](./03-capture-targets.md) |
| FR-C2 | Capture over SSH | Target/SSH, Resilience | [03](./03-capture-targets.md), [05](./05-resilience.md) |
| FR-C3 | Capture from Docker | Target/Docker | [03](./03-capture-targets.md) |
| FR-C4 | Capture from K8s/OpenShift | Target/K8s | [03](./03-capture-targets.md) |
| FR-C5 | Users in separate files (scale) | Kc CLI Driver, Packager | [03 §3.8](./03-capture-targets.md), [06](./06-snapshot-and-manifest.md) |
| FR-C6 | Admin-API secret verification | Admin API Verifier | [02 §2.5](./02-architecture.md) |
| FR-C7 | Detect version + kc.sh path | Executor.Probe | [03 §3.9](./03-capture-targets.md) |
| FR-C8 | Offline export is the default mode | Kc CLI Driver | [03 §3.1](./03-capture-targets.md) |
| FR-C9 | Ephemeral clone on Docker/K8s | Ephemeral Clone Manager | [03 §3.3](./03-capture-targets.md) |
| FR-C10 | Free-port isolation on local/SSH | Port Allocator | [03 §3.4–3.5](./03-capture-targets.md) |
| FR-C11 | Unconditional teardown + orphan sweep | Ephemeral Clone Manager, Orchestrator | [03 §3.3](./03-capture-targets.md) |

## 11.2 Functional — Fidelity

| Req | Description | Module(s) | Design doc |
|-----|-------------|-----------|-----------|
| FR-F1 | Users + password hashes | Kc CLI Driver, Manifest Builder | [07 G](./07-realm-carryover-manifest.md) |
| FR-F2 | OTP/2FA + passkeys | Kc CLI Driver, Manifest Builder | [07 G](./07-realm-carryover-manifest.md) |
| FR-F3 | Unmasked client secrets | Kc CLI Driver, Secret Verifier, Crypto | [07 C](./07-realm-carryover-manifest.md), [08 §8.3](./08-security.md) |
| FR-F4 | RSA/EC/HMAC/AES keys incl. private | Kc CLI Driver, Secret Verifier | [07 B](./07-realm-carryover-manifest.md) |
| FR-F5 | LDAP/Kerberos federation + bind creds | Kc CLI Driver, Manifest Builder | [07 H](./07-realm-carryover-manifest.md) |
| FR-F6 | IdP federations + secrets | Kc CLI Driver, Manifest Builder | [07 I](./07-realm-carryover-manifest.md) |
| FR-F7 | Clients/scopes/roles/groups/authz | Kc CLI Driver, Manifest Builder | [07 C–F](./07-realm-carryover-manifest.md) |
| FR-F8 | Auth flows + authenticator configs | Kc CLI Driver, Manifest Builder | [07 J](./07-realm-carryover-manifest.md) |
| FR-F9 | Realm settings + SMTP + theme selection | Kc CLI Driver, Manifest Builder | [07 A/K](./07-realm-carryover-manifest.md) |
| ~~FR-F10~~ | *Withdrawn — sessions out of scope (D1)* | — | [07 L](./07-realm-carryover-manifest.md), [12 D1](./12-decisions.md) |

## 11.3 Functional — Storage / Restore / Manifest

| Req | Description | Module(s) | Design doc |
|-----|-------------|-----------|-----------|
| FR-S1 | Disk storage backend | Store/Disk | [04 §4.2](./04-storage-backends.md) |
| FR-S2 | SSH/SFTP volume storage backend | Store/Sftp | [04 §4.3](./04-storage-backends.md) |
| FR-S3 | S3-compatible storage backend | Store/S3 | [04 §4.4](./04-storage-backends.md) |
| FR-S4 | Azure Blob / Azurite storage backend | Store/Azure | [04 §4.5](./04-storage-backends.md) |
| FR-S5 | List/retrieve/delete | BlobStore, Snapshot lib | [04 §4.1](./04-storage-backends.md), [09](./09-workflows-and-ui.md) |
| FR-S6 | One realm per snapshot | Packager, Orchestrator | [06 §6.1](./06-snapshot-and-manifest.md), [12 D5](./12-decisions.md) |
| FR-D1 | Detect + report themes / provider JARs | Dependency Scanner, Manifest Builder | [07 M](./07-realm-carryover-manifest.md) |
| FR-D2 | Dependencies surfaced before import (informative) | Snapshot Inspector, UI | [09 §9.3](./09-workflows-and-ui.md) |
| FR-R1 | Restore via kc.sh import / Admin API | Kc CLI Driver, Admin | [09 §9.3](./09-workflows-and-ui.md) |
| FR-R2 | Dry-run diff + preview | Manifest Builder, UI | [07 §7.4](./07-realm-carryover-manifest.md), [09 §9.3](./09-workflows-and-ui.md) |
| FR-R3 | Overwrite/skip/merge | Orchestrator, Admin | [09 §9.3](./09-workflows-and-ui.md) |
| FR-R4 | Verify + decrypt before restore | Integrity, Crypto | [06 §6.2](./06-snapshot-and-manifest.md), [08 §8.6](./08-security.md) |
| FR-M1 | Per-snapshot manifest + secret ledger | Manifest Builder | [07](./07-realm-carryover-manifest.md) |
| FR-M2 | Completeness report | Manifest Builder | [07 §7.1/L](./07-realm-carryover-manifest.md) |
| FR-M3 | Machine + human manifest | Manifest Builder, UI | [07](./07-realm-carryover-manifest.md), [09 §9.5](./09-workflows-and-ui.md) |

## 11.4 Functional — Inspection & browsing

| Req | Description | Module(s) | Design doc |
|-----|-------------|-----------|-----------|
| FR-V1 | Snapshot library across storage backends, no keys needed | Snapshot Inspector (Tier 0), BlobStore | [10 §10.2](./10-snapshot-inspection.md) |
| FR-V2 | View full snapshot details | Snapshot Inspector (Tier 1) | [10 §10.2](./10-snapshot-inspection.md) |
| FR-V3 | Browse/search users in a snapshot | Snapshot Inspector, Index Store | [10 §10.3–10.4](./10-snapshot-inspection.md) |
| FR-V4 | Filter/facet users | Index Store (facets) | [10 §10.4](./10-snapshot-inspection.md) |
| FR-V5 | Individual user detail + credential presence | Snapshot Inspector, Index Store | [10 §10.3–10.4](./10-snapshot-inspection.md) |
| FR-V6 | Browse clients/keys/IdPs/LDAP/flows/external deps | Snapshot Inspector, Index Store | [10 §10.5](./10-snapshot-inspection.md) |
| FR-V7 | Audited, explicit secret reveal | Snapshot Inspector, Crypto Vault, Observability | [10 §10.6](./10-snapshot-inspection.md), [08](./08-security.md) |
| FR-V8 | Verify integrity without restoring | Integrity Service, Crypto Vault | [10 §10.7](./10-snapshot-inspection.md) |
| ~~FR-V9~~ | *Withdrawn — snapshot comparison out of scope; the pre-restore dry-run stays under FR-R2* | — | [09 §9.3](./09-workflows-and-ui.md) |
| ~~FR-V10~~ | *Withdrawn — a snapshot exports nothing; the inspector reads it on screen* | — | [10 §10.8](./10-snapshot-inspection.md) |

## 11.5 Non-functional

| Req | Description | Module(s) | Design doc |
|-----|-------------|-----------|-----------|
| NFR-1 | Bad-connection tolerance + resume | Resilience Layer | [05](./05-resilience.md) |
| NFR-2 | Integrity everywhere | Integrity Service, Crypto | [06](./06-snapshot-and-manifest.md), [08](./08-security.md) |
| NFR-3 | Security / **opt-in** encryption / no secret logs | Crypto Vault, Redaction, Keychain | [08 §8.2](./08-security.md), [12 D8](./12-decisions.md) |
| NFR-4 | Single-binary desktop | Wails shell | [02 §2.8](./02-architecture.md) |
| NFR-5 | Observability + audit | Observability | [05 §5.5](./05-resilience.md), [09 §9.4](./09-workflows-and-ui.md) |
| NFR-6 | Streaming + concurrency | Packager, Store, Orchestrator | [02 §2.7](./02-architecture.md), [06](./06-snapshot-and-manifest.md) |
| NFR-7 | Least privilege / separation of duties | Config Store | [08 §8.7](./08-security.md) |
| NFR-8 | Idempotence / convergent resume | Orchestrator, Resilience | [02 §2.7](./02-architecture.md), [05](./05-resilience.md) |
| NFR-9 | Inspection responsiveness (bounded memory, fast paging) | Index Store (SQLite), Snapshot Inspector | [10 §10.3](./10-snapshot-inspection.md) |
| NFR-10 | No inspection residue (index destroyed on close) | Index Store | [10 §10.3](./10-snapshot-inspection.md), [12 D9](./12-decisions.md) |
| NFR-11 | Transparent, file-based configuration (SQLite only for throwaway indexes) | Config Store, Index Store | [02 §2.6](./02-architecture.md), [12 D7](./12-decisions.md) |

## 11.6 Decisions

All nine open questions from the first design pass have been answered and are recorded, with
rationale and consequences, in **[12 — Design Decisions](./12-decisions.md)**:

| # | Decision | Doc |
|---|----------|-----|
| D1 | Sessions out of scope; token continuity via carried signing keys | [12 D1](./12-decisions.md) |
| D2 | Offline export default; ephemeral clone on Docker/K8s; free ports on local/SSH | [12 D2](./12-decisions.md) |
| D3 | No selective restore | [12 D3](./12-decisions.md) |
| D4 | Themes/provider JARs detected and reported, not migrated | [12 D4](./12-decisions.md) |
| D5 | One snapshot = one realm | [12 D5](./12-decisions.md) |
| D6 | Wails v3 | [12 D6](./12-decisions.md) |
| D7 | SQLite for the inspection index | [12 D7](./12-decisions.md) |
| D8 | Encryption opt-in, prominently promoted | [12 D8](./12-decisions.md) |
| D9 | Inspection index built on open, destroyed on close | [12 D9](./12-decisions.md) |

Nothing remains blocking. Three items to revisit once there is running code are listed in
[12 — Decisions still open](./12-decisions.md#decisions-still-open).

## 11.7 From requirement to running code

This matrix answers *which module satisfies this requirement*. The companion question — *when is
it built, and what proves it* — is answered by
**[rollout/12 — Rollout traceability](./rollout/12-rollout-traceability.md)**, which maps the same
requirements onto the nine build phases and names the test or artifact that verifies each one.
