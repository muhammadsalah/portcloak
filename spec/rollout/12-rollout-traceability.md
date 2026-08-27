# 12 — Rollout Traceability

Every use case and every requirement mapped to the phase that delivers it, plus the reverse
check that no phase exists without something to deliver. [11 — Traceability](../11-traceability.md)
maps requirements to *modules*; this maps them to *time*.

**How to read the phase column.** Where two phases appear, the first delivers the capability and
the second completes its proof — most often P2 producing something and P7 proving it survives a
round trip.

## 12.1 Use cases → phase

### Environments (9)

| UC | Title | Phase |
|----|-------|:-----:|
| UC-E1 | Add a Local environment | P1 |
| UC-E2 | Add an SSH environment | P1 |
| UC-E3 | Add a Docker environment | P1 |
| UC-E4 | Add a Kubernetes / OpenShift environment | P1 |
| UC-E5 | Test an environment | P1 |
| UC-E6 | Edit an environment | P1 |
| UC-E7 | Duplicate an environment | P1 |
| UC-E8 | Delete an environment | P1 |
| UC-E9 | Review environments at a glance | P1 |

### Storage (8)

| UC | Title | Phase |
|----|-------|:-----:|
| UC-S1 | Add Disk storage | P1 |
| UC-S2 | Add SSH storage | P1 · P3 |
| UC-S3 | Add S3-compatible storage | P1 · P5 |
| UC-S4 | Add Azure Blob / Azurite storage | P1 · P5 |
| UC-S5 | Test a storage | P1 |
| UC-S6 | Edit or delete a storage | P1 |
| UC-S7 | Set the default storage | P1 |
| UC-S8 | Browse the contents of a storage | P5 |

*S2, S3 and S4 are definable and testable in P1; the working transfer path lands with the
backend itself (SFTP in P3, S3 and Azure in P5).*

### Capture (12)

| UC | Title | Phase |
|----|-------|:-----:|
| UC-C1 | Capture a realm from a Local environment | P2 |
| UC-C2 | Capture a realm over SSH | P3 |
| UC-C3 | Capture a realm from Docker via an ephemeral clone | P3 |
| UC-C4 | Capture a realm from Kubernetes / OpenShift via an ephemeral clone | P3 |
| UC-C5 | Capture several realms in one run | P2 |
| UC-C6 | Probe an environment before capture | P1 |
| UC-C7 | Isolate the export with free ports | P2 |
| UC-C8 | Verify exported secrets are unmasked | P8 |
| UC-C9 | Detect external dependencies | P8 |
| UC-C10 | Encrypt a snapshot (opt-in) | P5 |
| UC-C11 | Destroy the ephemeral clone | P3 |
| UC-C12 | Reap orphaned clones | P3 |

### Inspection (13)

| UC | Title | Phase |
|----|-------|:-----:|
| UC-I1 | Browse the snapshot library | P6 |
| UC-I2 | Open a snapshot and view its details | P6 |
| UC-I3 | Build the inspection index | P6 |
| UC-I4 | Search users within a snapshot | P6 |
| UC-I5 | Filter and facet users | P6 |
| UC-I6 | View a single user's detail | P6 |
| UC-I7 | Browse clients, keys, federations and flows | P6 |
| UC-I8 | View the secret ledger | P6 |
| UC-I9 | Reveal a single secret | P6 |
| UC-I10 | Review external dependencies | P6 · P8 |
| UC-I11 | Verify a snapshot without restoring | P6 |
| UC-I12 | Export an inspection view | P6 |
| UC-I13 | Close a snapshot | P6 |

### Restore (8)

| UC | Title | Phase |
|----|-------|:-----:|
| UC-R1 | Restore a snapshot into an environment | P7 |
| UC-R2 | Review restore preconditions | P7 |
| UC-R3 | Preview changes with a dry run | P7 |
| UC-R4 | Choose an import strategy | P7 |
| UC-R5 | Apply the import | P7 |
| UC-R6 | Validate after restore | P7 |
| UC-R7 | Restore into a freshly provisioned Keycloak | P7 |
| UC-R8 | Cancel a restore | P7 |

### Operations (10)

| UC | Title | Phase |
|----|-------|:-----:|
| UC-O1 | Monitor running work | P4 |
| UC-O2 | Resume an interrupted job | P4 |
| UC-O3 | Cancel a job | P4 |
| UC-O4 | Discard an interrupted job | P4 |
| UC-O5 | Understand a failure | P4 |
| UC-O6 | Survive a flaky connection | P4 |
| UC-O7 | Edit configuration outside the app | P0 |
| UC-O8 | Review the audit log | P4 |
| UC-O9 | Start the application | P0 |
| UC-O10 | Purge local working data | P6 |

## 12.2 Functional requirements → phase

| Req | Phase | Req | Phase |
|-----|:-----:|-----|:-----:|
| FR-C1 | P2 | FR-S1 | P2 |
| FR-C2 | P3 | FR-S2 | P3 |
| FR-C3 | P3 | FR-S3 | P5 |
| FR-C4 | P3 | FR-S4 | P5 |
| FR-C5 | P2 | FR-S5 | P5 |
| FR-C6 | P8 | FR-S6 | P2 |
| FR-C7 | P1 · P2 | FR-D1 | P8 |
| FR-C8 | P2 | FR-D2 | P7 |
| FR-C9 | P3 | FR-R1 | P7 |
| FR-C10 | P2 | FR-R2 | P7 |
| FR-C11 | P3 | FR-R3 | P7 |
| FR-F1…FR-F9 | P2 · P7 | FR-R4 | P7 |
| FR-N1 | P1 | FR-M1 | P2 |
| FR-N2 | P1 | FR-M2 | P2 |
| FR-N3 | P1 | FR-M3 | P2 |
| FR-N4 | P1 | FR-V1…FR-V8 | P6 |
| FR-N5 | P1 | FR-V10 | P6 |
| FR-N6 | P0 | | |

Withdrawn and therefore delivered by nobody: **FR-F10** (sessions, [D1](../12-decisions.md)) and
**FR-V9** (snapshot comparison). Both are verified negatively — the tool reports them as out of
scope rather than silently omitting them ([01 §1.10](./01-test-strategy.md)).

*FR-F1…F9 are captured in P2 and proved in P7: the capture-side assertion shows the bundle holds
the material; the round-trip assertion shows the destination can use it. Only the second proves
the promise.*

## 12.3 Non-functional requirements → phase

| Req | Phase | Where it is proved |
|-----|:-----:|--------------------|
| NFR-1 Resilience | P4 | Fault-injection matrix, including resume across an app restart. |
| NFR-2 Integrity | P2 | `TestIntegrityTree_DetectsSingleByteFlip`; restore refusal in P7. |
| NFR-3 Security | P0 · P5 | Redaction CI stage (P0), encryption and labelling tests (P5). |
| NFR-4 Portability | P0 | Three platform binaries launching standalone in CI. |
| NFR-5 Observability | P0 · P4 | Structured log (P0); audit log and progress (P4). |
| NFR-6 Performance | P2 · P5 | Bounded-memory tests on 2 GB inputs, packaging and encryption. |
| NFR-7 Least privilege | P1 · P3 | `TestProbe_IsReadOnly`; `TestServingWorkloadUntouched`. |
| NFR-8 Idempotence | P2 · P4 | `TestPackager_Deterministic`; `TestResume_Converges`. |
| NFR-9 Inspection responsiveness | P6 | Recorded latency and memory numbers on the `large` fixture. |
| NFR-10 No inspection residue | P6 | Three index-destruction tests plus the schema assertion. |
| NFR-11 File-based configuration | P0 | `TestConfigRoundTrip`; SQLite used only for inspection indexes. |

## 12.4 Reverse check — every phase earns its place

| Phase | Delivers | Would anything be lost by cutting it? |
|-------|----------|---------------------------------------|
| P0 | 2 UC, FR-N6, NFR-4, NFR-11, and the redaction floor | Everything. Also the only point at which redaction can be established before secrets exist. |
| P1 | 17 UC, FR-N1…N5, and `Probe` | The tool could not reach anything, and `Test` would drift from capture. |
| P2 | 3 UC, 18 FR, 3 NFR | The entire capture pipeline. |
| P3 | 5 UC, 6 FR, NFR-7 | Every target except the operator's own laptop. |
| P4 | 7 UC, 3 NFR | The tool would be unusable over any real network. |
| P5 | 2 UC, 3 FR, NFR-3 | Cloud storage and encryption. |
| P6 | 14 UC, 9 FR, 2 NFR | The ability to know what is in a snapshot before trusting it. |
| P7 | 8 UC, 5 FR | The other half of the product. |
| P8 | 2 UC, 2 FR | The guarantee that a carried secret is a real secret. |

Every use case appears exactly once as an owner. Every non-withdrawn requirement has a phase.
No phase delivers nothing.

## 12.5 Coverage totals

| | Count | Covered |
|---|---:|---:|
| Use cases | 60 | 60 |
| Functional requirements (active) | 50 | 50 |
| Functional requirements (withdrawn) | 2 | verified negatively |
| Non-functional requirements | 11 | 11 |
