# Changelog

All notable changes to PortCloak are recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). Tags prefixed `spec-` mark the design
record; unprefixed `v` tags mark shipped binaries.

## [0.0.1] — 2026-08-27

The first version where the whole loop closes: capture a realm, put it somewhere, read it back,
and restore it. All four target kinds, all four storage backends, inspection and restore.

### Capture
- Offline `kc.sh export` on every target, with `--users different_files` by default — which is
  what makes a very large realm survivable for the export, for a flaky link, and for the
  inspection index.
- Free-port allocation on local and SSH so an export cannot collide with a Keycloak already
  serving on 8080. The unavoidable race between releasing a port and Keycloak binding it is
  classified retryable and redone with fresh ports.
- Ephemeral clone execution on Docker and Kubernetes: a parked copy of the serving workload,
  started hung, exec'd into, and destroyed. The clone lifecycle is written once and both
  platforms plug into it, so teardown is one tested implementation rather than two — asserted
  across five exit paths including panic.
- Every inherited label stripped. A pod carrying `app=keycloak` is picked up by the production
  `Service` and receives real user traffic into a container that serves nothing.
- Several realms in one run produce several independent snapshots, sharing one clone.

### Snapshots
- Deterministic tar + zstd bundles with a per-artifact integrity tree, and both sidecars written
  beside the bundle so the library is browsable with no key at all.
- Opt-in age encryption, passphrase or recipients, verified decryptable at capture time — a
  bundle nobody can open should be found now, not eighteen months later during an incident.
- The manifest enumerates every carried category and every secret by location and kind. The
  ledger type has no value field, which is what makes it safe to read, screenshot and export.
- `outOfScope` stays distinct from `missing`, so a healthy snapshot never reads as broken.

### Storage
- Disk, SFTP, S3-compatible and Azure Blob behind one contract, all passing the same table.
- Resume driven by asking the server what it already holds — `ListParts` and `GetBlockList` —
  so an upload survives PortCloak itself being restarted.

### Resilience
- Retry, backoff with full jitter and per-endpoint circuit breaking, applied by wrapping every
  adapter rather than by retry code at call sites. An unclassified error defaults to terminal,
  so a new failure mode surfaces instead of silently looping.
- An unreplayable stream is never retried — resume from the checkpoint is the mechanism there,
  because retrying one would commit a truncated object.

### Inspection
- Tiered: the library needs no key, detail needs one, and the user index is built on open and
  destroyed on close.
- The index schema provably cannot hold a secret, asserted against a column allowlist. Small
  realms are indexed in memory and never touch disk.
- Audited single-secret reveal, redacted CSV/JSON export, and verification without contacting
  any environment.

### Restore
- Verification and decryption gate the restore before the destination is contacted at all.
- Preconditions are informative and never block; the dry run is computed for the strategy
  actually selected; merge says which path it needs rather than quietly becoming an overwrite.
- Nothing claims a rollback. Keycloak's import is not transactional, so a failure records what
  may already have reached the destination.
- Validation checks the signing key by KID rather than by count — the right number of keys with
  the wrong ids still means every existing token stops verifying.

### Application
- Wails v3 desktop shell with the eight screens from the design file. No sign-in, because there
  is no account: configuration is plain YAML in `~/.portcloak/` and every credential lives in
  the OS keychain, referenced by handle.
- Redacting `slog` handler from the first commit, with its own CI stage so a failure there is
  unmissable.

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
