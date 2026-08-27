# 09 — Workflows & Wails UI


PortCloak is a guided desktop app. The UI's job is to make `kc.sh` fluency unnecessary and to
make resilience and fidelity *visible* — progress, gaps, and secrets are shown, not hidden.

There is **no sign-in** (N8). PortCloak opens straight into the workspace: it is a single-user
local tool, and the only credentials involved are the ones each environment and storage
definition carries.

## 9.1 Information architecture

![UI information architecture](./diagrams/png/12-ui-information-architecture.png)

*Source: [`12-ui-information-architecture.puml`](./diagrams/12-ui-information-architecture.puml) · [SVG](./diagrams/svg/12-ui-information-architecture.svg)*

## 9.1b Configuration: Environments and Storage

Two configuration screens hold everything PortCloak needs to reach the outside world. Both are
list-and-detail: a list of defined entries on the left, a form on the right, and a **Test**
action that reports a concrete pass/fail rather than a silent save.

### Environments — *where Keycloak runs*

An environment is an execution context PortCloak can capture from or restore to. Four kinds,
each asking only for what that kind actually needs:

| Kind | Fields |
|------|--------|
| **Local** | **Keycloak server folder** (install root containing `bin/kc.sh`) |
| **SSH** | Host, port, user, auth (key/agent/password), optional jump host, **Keycloak server folder** on the remote |
| **Docker** | Docker endpoint (socket / `DOCKER_HOST` / over-SSH), **service or container** running Keycloak |
| **Kubernetes / OpenShift** | Kubeconfig context, **namespace**, **Deployment or StatefulSet** running Keycloak |

**Test** runs the same `Probe` the capture wizard uses ([03 §3.9](./03-capture-targets.md)), so
what you see here is exactly what a capture would find: Keycloak version, `kc.sh` location, free
space, whether an ephemeral clone can be created, and Admin API reachability.

### Storage — *where snapshots live*

A storage definition is a destination for snapshot bundles. Every kind is rooted at a
**folder / prefix**, so one bucket or one host can hold several independent snapshot trees:

| Kind | Fields |
|------|--------|
| **Disk** | Root **folder** |
| **SSH** | Host, port, user, auth, remote **folder** |
| **S3** | Endpoint, region, bucket, **prefix (folder)**, credentials |
| **Azure Blob / Azurite** | Account or endpoint, container, **prefix (folder)**, credentials |

One storage is marked **default** for new captures. Each also carries an **encryption required**
switch that removes the opt-out for snapshots written there ([08 §8.2](./08-security.md)).

### Where this is persisted

Both lists live in `~/.portcloak/config.yaml` — readable, diffable, hand-editable — with every
credential held in the **OS keychain** and referenced by handle
([02 §2.6](./02-architecture.md)). Deleting an entry that something references is warned about,
not silently allowed.

## 9.2 Capture workflow

![Capture sequence](./diagrams/png/05-capture-sequence.png)

*Source: [`05-capture-sequence.puml`](./diagrams/05-capture-sequence.puml) · [SVG](./diagrams/svg/05-capture-sequence.svg)*

1. **Choose source environment** from the configured list (Local / SSH / Docker / K8s). PortCloak **probes** and shows target
   facts (KC version, `kc.sh` path, free space, allocated free ports, whether an ephemeral clone
   can be created, admin-API reachability).
2. **Select realms.** Multi-select is allowed, but each realm produces its **own snapshot**
   (FR-S6) — the UI shows this as N queued jobs, not one bundle.
3. **Options:** users mode (`different_files` default), verification on/off (secret check +
   dependency detection), and **encryption** — the toggle is presented **on**, and turning it off
   requires an explicit confirmation that spells out the consequence.
4. **Choose storage** from the configured list; the one marked default is pre-selected.
5. **Run:** live progress per phase (probe → clone → export → fetch → verify → teardown →
   package → upload) with the **partial-failure ledger** and a Resume affordance if a connection
   drops. On Docker/K8s the clone's lifecycle is shown explicitly, so the operator can see it
   created and destroyed.
6. **Result:** completeness report, secret ledger, **external dependency list**, and where the
   snapshot landed.

## 9.3 Restore workflow

![Restore sequence](./diagrams/png/06-restore-sequence.png)

*Source: [`06-restore-sequence.puml`](./diagrams/06-restore-sequence.puml) · [SVG](./diagrams/svg/06-restore-sequence.svg)*

1. **Pick a snapshot** from the library (sidecar manifest previews without decrypting).
2. **Pick destination environment** (separate, higher-privilege).
3. **Decrypt (if encrypted) + verify** integrity; **preview manifest**; run a **dry-run diff**
   vs the target.
4. **Review preconditions** — the **external dependency list** (themes, provider JARs) is shown
   for information, because a realm referencing a missing theme or authenticator SPI imports
   cleanly and then fails at login (FR-D2). It does not gate the wizard: the Operator manages
   these environments and is assumed to know what is deployed where.
5. **Choose strategy:** overwrite / skip / merge (`partialImport` for merge on a running server).
   Restore is **whole-realm** — there is no cherry-picking (N6).
6. **Apply** via `kc.sh import` (offline) or Admin API (running); stream the import log.
7. **Post-check:** re-read target, compare counts + key KIDs to the manifest, report drift and
   restate what was out of scope (sessions — users will re-authenticate).

## 9.3b Inspect workflow

Snapshots are browsable artifacts, not black boxes: the library lists every snapshot across all
storage backends without decryption keys, and opening one reveals its realm detail, a searchable user
table, clients/keys/federations, and the secret ledger. Full design in
[10 — Snapshot Inspection](./10-snapshot-inspection.md).

Key UI affordances:
- **Library** — realm, date, source, counts, completeness badge (no keys needed).
- **Users tab** — paginated, searchable, faceted (enabled, origin, role, group, 2FA type).
- **Entities tabs** — clients (secret present?), keys (private carried?), IdPs, LDAP, flows,
  external dependencies.
- **Verify** — integrity check without touching any target.
- **Reveal** — explicit, audited, per-secret; redacted everywhere by default.

## 9.4 Making resilience visible

- Every long op shows **phase, item, attempts, last error, outcome** (the ledger from
  [05 §5.5](./05-resilience.md)).
- An interrupted job appears in **Activity** as `Interrupted — Resume` (survives app restart),
  not as a lost job.
- Circuit-breaker "paused, retrying in Ns" is shown rather than a spinning hang.

## 9.5 Making fidelity visible

- Completeness verdict badge: **Full / Partial / Gaps**, with `missing` (something went wrong)
  kept visually distinct from `outOfScope` (a design decision — sessions, theme files), so a
  healthy snapshot never looks broken.
- Secret ledger panel: counts by type, all value-free.
- Key panel: KIDs carried + a **"token continuity preserved"** indicator when the active signing
  key travels with the snapshot — this is the feature that stands in for session portability, so
  it is surfaced prominently rather than buried in the manifest.
- **Encryption badge:** encrypted snapshots show a lock; unencrypted ones carry a persistent
  warning that they hold unmasked secrets and private keys in the clear.

## 9.6 Wails specifics

- **Wails v3** is the target shell.
- **Bindings:** `CaptureController`, `RestoreController`, `SnapshotController`, `InspectController`,
  `ConfigController` expose Go methods to the frontend; nothing blocks — work runs in goroutines.
- **Events:** progress/log/ledger updates stream via `runtime.EventsEmit`; the frontend
  subscribes and renders live.
- **Cancellation:** UI cancel maps to `context.Context` cancellation down the whole stack.
- **Cancellation** must also tear down an in-flight ephemeral clone — the cancel path runs
  `Teardown`, it does not merely abandon the job.
- **Single binary:** no external `ssh`/`kubectl`/`docker`/`aws` CLIs required (pure-Go SDKs,
  including a cgo-free SQLite), though CLI fallbacks are offered where a socket/API isn't exposed.

## 9.7 Requirement coverage

Fulfills **FR-R2/FR-R3** (dry-run + strategies), **FR-D2** (dependencies surfaced before import),
**FR-M3** (human-rendered manifest), **NFR-5** (observability/audit), and **G7** (usability), and
is the visible surface of the resilience and fidelity guarantees.
