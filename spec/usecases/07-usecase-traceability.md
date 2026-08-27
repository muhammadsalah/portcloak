<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# 07 — Use Case Traceability

Every use case maps to the requirements it satisfies and the design document that specifies the
mechanism. This closes the loop in both directions: no use case is unmotivated, and no
requirement is left without a behaviour that realises it.

## Environments

| UC | Name | Covers | Design |
|----|------|--------|--------|
| UC-E1 | Add Local environment | FR-N1, FR-N3, FR-C1 | [03 §3.4](../03-capture-targets.md) |
| UC-E2 | Add SSH environment | FR-N1, FR-N3, FR-N6, FR-C2, FR-C10 | [03 §3.5](../03-capture-targets.md) |
| UC-E3 | Add Docker environment | FR-N1, FR-N3, FR-C3, FR-C9 | [03 §3.6](../03-capture-targets.md) |
| UC-E4 | Add Kubernetes/OpenShift environment | FR-N1, FR-N3, FR-C4, FR-C9 | [03 §3.7](../03-capture-targets.md) |
| UC-E5 | Test an environment | FR-N3, FR-C7, FR-C10 | [03 §3.9](../03-capture-targets.md) |
| UC-E6 | Edit an environment | FR-N4 | [02 §2.6](../02-architecture.md) |
| UC-E7 | Duplicate an environment | FR-N4, FR-N6 | [08 §8.4](../08-security.md) |
| UC-E8 | Delete an environment | FR-N4 | [02 §2.6](../02-architecture.md) |
| UC-E9 | Review environments | FR-N1, FR-N3 | [09 §9.1b](../09-workflows-and-ui.md) |

## Storage

| UC | Name | Covers | Design |
|----|------|--------|--------|
| UC-S1 | Add Disk storage | FR-N2, FR-N3, FR-S1 | [04 §4.2](../04-storage-backends.md) |
| UC-S2 | Add SSH storage | FR-N2, FR-N3, FR-S2 | [04 §4.3](../04-storage-backends.md) |
| UC-S3 | Add S3 storage | FR-N2, FR-N3, FR-S3 | [04 §4.4](../04-storage-backends.md) |
| UC-S4 | Add Azure / Azurite storage | FR-N2, FR-N3, FR-S4 | [04 §4.5](../04-storage-backends.md) |
| UC-S5 | Test a storage | FR-N3 | [04 §4.1](../04-storage-backends.md) |
| UC-S6 | Edit / delete a storage | FR-N4, FR-N5 | [09 §9.1b](../09-workflows-and-ui.md) |
| UC-S7 | Set the default storage | FR-N5 | [09 §9.1b](../09-workflows-and-ui.md) |
| UC-S8 | Browse storage contents | FR-S5, FR-V1 | [10 §10.2](../10-snapshot-inspection.md) |

## Capture

| UC | Name | Covers | Design |
|----|------|--------|--------|
| UC-C1 | Capture from Local | FR-C1, FR-C5, FR-C8, FR-C10, FR-S6 | [03 §3.4](../03-capture-targets.md) |
| UC-C2 | Capture over SSH | FR-C2, FR-C10, FR-C11, NFR-1 | [03 §3.5](../03-capture-targets.md) |
| UC-C3 | Capture from Docker (clone) | FR-C3, FR-C9, FR-C11 | [03 §3.3, §3.6](../03-capture-targets.md) |
| UC-C4 | Capture from K8s/OC (clone) | FR-C4, FR-C9, FR-C11 | [03 §3.3, §3.7](../03-capture-targets.md) |
| UC-C5 | Capture several realms | FR-S6, FR-C9, FR-C11 | [12 D5](../12-decisions.md) |
| UC-C6 | Probe an environment | FR-C7 | [03 §3.9](../03-capture-targets.md) |
| UC-C7 | Allocate free ports | FR-C10 | [03 §3.4](../03-capture-targets.md) |
| UC-C8 | Verify secrets unmasked | FR-C6, FR-F3, FR-F4 | [02 §2.5](../02-architecture.md) |
| UC-C9 | Detect external dependencies | FR-D1, FR-D2 | [07 §M](../07-realm-carryover-manifest.md) |
| UC-C10 | Encrypt (opt-in) | NFR-3, FR-F3 | [08 §8.2](../08-security.md) |
| UC-C11 | Destroy ephemeral clone | FR-C11 | [03 §3.3](../03-capture-targets.md) |
| UC-C12 | Reap orphaned clones | FR-C11, NFR-1 | [03 §3.3](../03-capture-targets.md) |

## Inspection

| UC | Name | Covers | Design |
|----|------|--------|--------|
| UC-I1 | Browse the snapshot library | FR-V1, FR-S5 | [10 §10.2](../10-snapshot-inspection.md) |
| UC-I2 | Open a snapshot | FR-V2, FR-R4 | [10 §10.2](../10-snapshot-inspection.md) |
| UC-I3 | Build the inspection index | NFR-9, NFR-11 | [10 §10.3](../10-snapshot-inspection.md) |
| UC-I4 | Search users | FR-V3, NFR-9 | [10 §10.4](../10-snapshot-inspection.md) |
| UC-I5 | Filter and facet users | FR-V4 | [10 §10.4](../10-snapshot-inspection.md) |
| UC-I6 | View a user's detail | FR-V5 | [10 §10.3](../10-snapshot-inspection.md) |
| UC-I7 | Browse clients, keys, federations | FR-V6 | [10 §10.5](../10-snapshot-inspection.md) |
| UC-I8 | View the secret ledger | FR-M1, FR-V6 | [07 §7.2](../07-realm-carryover-manifest.md) |
| UC-I9 | Reveal a secret | FR-V7, NFR-3, NFR-5 | [10 §10.6](../10-snapshot-inspection.md) |
| UC-I10 | Review external dependencies | FR-D2, FR-V6 | [07 §M](../07-realm-carryover-manifest.md) |
| UC-I11 | Verify without restoring | FR-V8, NFR-2 | [10 §10.7](../10-snapshot-inspection.md) |
| UC-I12 | Export an inspection view | FR-V10 | [10 §10.8](../10-snapshot-inspection.md) |
| UC-I13 | Close a snapshot | NFR-10, NFR-11 | [10 §10.3](../10-snapshot-inspection.md) |

## Restore

| UC | Name | Covers | Design |
|----|------|--------|--------|
| UC-R1 | Restore into an environment | FR-R1, FR-R4, N6 | [09 §9.3](../09-workflows-and-ui.md) |
| UC-R2 | Review preconditions (informative) | FR-D2, FR-R2 | [07 §M](../07-realm-carryover-manifest.md) |
| UC-R3 | Dry-run diff | FR-R2 | [09 §9.3](../09-workflows-and-ui.md) |
| UC-R4 | Choose an import strategy | FR-R3, N6 | [09 §9.3](../09-workflows-and-ui.md) |
| UC-R5 | Apply the import | FR-R1, FR-R3 | [09 §9.3](../09-workflows-and-ui.md) |
| UC-R6 | Validate after restore | FR-R1, FR-M2 | [07 §7.4](../07-realm-carryover-manifest.md) |
| UC-R7 | Restore to a fresh Keycloak | FR-R1, G1 | [01 §1.2](../01-vision-and-requirements.md) |
| UC-R8 | Cancel a restore | FR-C11, NFR-5 | [03 §3.3](../03-capture-targets.md) |

## Operations

| UC | Name | Covers | Design |
|----|------|--------|--------|
| UC-O1 | Monitor running work | NFR-5 | [09 §9.4](../09-workflows-and-ui.md) |
| UC-O2 | Resume an interrupted job | NFR-1, NFR-8, FR-C11 | [05 §5.2](../05-resilience.md) |
| UC-O3 | Cancel a job | FR-C11, NFR-1 | [05 §5.4](../05-resilience.md) |
| UC-O4 | Discard an interrupted job | NFR-1 | [05 §5.2](../05-resilience.md) |
| UC-O5 | Understand a failure | NFR-5, NFR-1 | [05 §5.5](../05-resilience.md) |
| UC-O6 | Survive a flaky connection | NFR-1, NFR-2 | [05 §5.2, §5.3](../05-resilience.md) |
| UC-O7 | Edit configuration outside the app | FR-N6, NFR-11 | [02 §2.6](../02-architecture.md) |
| UC-O8 | Review the audit log | NFR-5, N8 | [08 §8.5](../08-security.md) |
| UC-O9 | Start the application | N8, NFR-11, FR-C11 | [02 §2.6](../02-architecture.md) |
| UC-O10 | Purge local working data | NFR-10, NFR-3 | [08 §8.8](../08-security.md) |

## Reverse check — every requirement has a use case

| Requirement group | Realised by |
|-------------------|-------------|
| **FR-C1..C11** capture | UC-C1 … UC-C12, UC-E1 … UC-E5 |
| **FR-F1..F9** fidelity | UC-C1 (export), UC-C8 (verify), UC-I2/I6/I7 (evidence) |
| **FR-D1..D2** dependencies | UC-C9, UC-I10, UC-R2 |
| **FR-N1..N6** configuration | UC-E1 … UC-E9, UC-S1 … UC-S7, UC-O7 |
| **FR-S1..S6** storage | UC-S1 … UC-S8, UC-C1 (one realm per snapshot) |
| **FR-R1..R4** restore | UC-R1 … UC-R8 |
| **FR-M1..M3** manifest | UC-I2, UC-I8, UC-R6 |
| **FR-V1..V10** inspection | UC-I1 … UC-I13, UC-S8 (FR-V9 withdrawn) |
| **NFR-1** resilience | UC-O2, UC-O3, UC-O5, UC-O6, UC-C12 |
| **NFR-2** integrity | UC-I11, UC-O6 |
| **NFR-3** security | UC-C10, UC-I9, UC-O10, UC-E2/E7 |
| **NFR-5** observability | UC-O1, UC-O5, UC-O8 |
| **NFR-8** idempotence | UC-O2 |
| **NFR-9/NFR-10** inspection perf & residue | UC-I3, UC-I13, UC-O10 |
| **NFR-11** file-based config | UC-O7, UC-O9, UC-I3 |
| **N5** sessions out of scope | UC-R1 step 10, UC-R6 step 4 |
| **N6** no selective restore | UC-R1, UC-R4 |
| **N7** assets not migrated | UC-C9, UC-I10, UC-R2 (reported, not enforced) |
| **N8** no user accounts | UC-O8, UC-O9, UC-I9 |

Nothing in the requirement set is left without a behaviour, and no use case introduces
capability the requirements do not ask for.
