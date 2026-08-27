# Changelog

All notable changes to PortCloak are recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). Tags prefixed `spec-` mark the design
record; unprefixed `v` tags mark shipped binaries.

## [spec-0.0.1] — 2026-08-27

The complete design record for PortCloak. No code — this is the specification that the first
implementation is built against.

### Specification
- Vision, goals, non-goals and the full requirement set: 50 active functional requirements
  (2 withdrawn and verified negatively) and 11 non-functional requirements.
- Module architecture with the `Executor`, `BlobStore`, `ResumableStore` and `Doer` seams.
- Four capture targets — local, SSH, Docker, Kubernetes/OpenShift — including ephemeral clone
  execution and the label-stripping rules that keep a production `Service` from routing live
  traffic into an export pod.
- Four storage backends — disk, SFTP, S3-compatible, Azure Blob/Azurite.
- Snapshot bundle format, integrity tree, opt-in encryption, and the realm carry-over manifest
  enumerating every category and every secret by location and kind.
- Resilience model: bounded retry with jitter, circuit breaking, disk checkpoints, convergent
  resume across application restarts.
- Tiered snapshot inspection with a session-scoped SQLite index that is destroyed on close.
- 20 rendered PlantUML diagrams.

### Use cases
- 60 use cases across environments, storage, capture, inspection, restore and operations, each
  with alternate flows, exceptions and postconditions, plus a traceability matrix.

### Rollout
- Nine build phases (P0–P8) plus a release gate, with per-phase coding tasks, test strategy and
  a verification table mapping every requirement to the test that would fail if it broke.
- Test strategy organised around the four failure classes that matter here: fidelity loss,
  collateral damage, silent corruption, secret leakage.

### Design
- 20 screens in Keycloak/PatternFly styling covering all 60 use cases, with a design-token board.

### Decisions
- D1 sessions out of scope; token continuity via carried signing keys.
- D2 offline `kc.sh export` by default; ephemeral clones on Docker/K8s; free ports on local/SSH.
- D3 no selective restore. · D4 themes and provider JARs reported, never migrated.
- D5 one snapshot is one realm. · D6 Wails v3. · D7 SQLite for the inspection index only.
- D8 encryption opt-in, prominently promoted. · D9 index built on open, destroyed on close.

### Withdrawn during design
- Session portability (FR-F10), snapshot comparison (FR-V9), storage mirroring, and retention
  policies. The pre-restore dry-run diff against a live target realm remains.
