# P1 — Configuration

**Goal.** An operator can describe their world to PortCloak: the environments where Keycloak
runs and the storage where snapshots will live. Both are list-and-detail screens with a **Test**
action that makes a real round trip and reports a concrete result. At the end of this phase
nothing has been captured, but the tool knows how to reach everything it will later need — and,
critically, it can tell you *before* a capture whether it can reach it.

**Covers.** UC-E1…UC-E9, UC-S1…UC-S7 · FR-N1, FR-N2, FR-N3, FR-N4, FR-N5.

**Depends on.** P0.

**Packages.** `engine/config` (extended), `engine/target/*` (`Probe` only), `engine/store/*`
(reachability only), `internal/app/config_controller`, `frontend/`.

---

## Why probing lands here and not in P2

`Test` is specified to run **the same `Probe` the capture wizard uses**
([09 §9.1b](../09-workflows-and-ui.md)). That is a promise about shared code, and the cheapest
way to keep it is to build `Probe` first, in the phase whose only consumer is `Test`. When P2
adds capture, it calls something that already works and is already tested against every target
kind. The inverse order — capture first, then retrofit a `Test` button — is how the two drift
into reporting different things.

`Probe` is also the phase's natural risk sink. Discovering that `kc.sh --version` writes to
stderr, or that an OpenShift context needs a different exec path, costs a day here and would
cost a week wedged inside the capture pipeline.

## Tasks

### T-P1.1 — `Executor.Probe` and `TargetFacts`
Define `TargetFacts` — Keycloak version, `kc.sh` path, writable temp location, free space,
whether an ephemeral clone can be created, Admin API reachability — and implement `Probe` for
all four kinds. `Prepare`/`Run`/`FetchDir`/`Teardown` are declared and return
`ErrNotImplemented`; they arrive in P2 and P3.

Probe is **read-only on the target**, without exception (NFR-7). It reads a version, stats a
path, checks a permission. It never writes, never restarts anything, and never creates the clone
it reports as feasible.

*Done when:* all four kinds return populated facts against their fixture, and a probe against a
Keycloak the operator lacks permission on reports exactly which permission is missing.

### T-P1.2 — Environment definitions and CRUD
The four kinds with the fields from [09 §9.1b](../09-workflows-and-ui.md), each validated for
its own kind only — an SSH environment is never asked for a namespace. Create, edit, duplicate
and delete (UC-E1…E4, E6, E7, E8). Duplicate copies the definition and **not** the credential
handle, so a cloned environment prompts for its own credential rather than silently reusing
another environment's.

*Done when:* each kind round-trips through `config.yaml`; per-kind validation rejects the wrong
field; delete warns when something references the entry rather than silently allowing it.

### T-P1.3 — Storage definitions and CRUD
The four kinds, each rooted at a folder or prefix so one bucket can hold several independent
trees. Includes the **default** flag (UC-S7, FR-N5) — setting one clears the others as a single
atomic config write — and the **encryption required** switch, which is stored now and enforced
in P5.

*Done when:* each kind round-trips; exactly one storage can be default; the encryption-required
flag persists and is visible in the list.

### T-P1.4 — Storage reachability test
`BlobStore.Probe`: resolve credentials, confirm the root exists, and prove write access by
writing and deleting a marker object under the configured prefix. Read-only credentials are a
legitimate configuration for browsing, so the result distinguishes "reachable, read-only" from
"reachable and writable" instead of collapsing both into a failure.

*Done when:* each of the four kinds reports a correct three-way result against its fixture, and
a wrong credential produces a sentence naming what was wrong rather than a wrapped SDK error.

### T-P1.5 — Configuration screens
The two screens from the design file: list on the left, kind-specific form on the right, `Test`
in the form footer. `Test` reports the concrete facts it found — version, path, free space,
clone feasibility — not a green tick. A green tick answers "did it connect"; an operator needs
to know "will my capture work", and those are different questions.

*Done when:* both screens match the design file, `Test` renders the full fact set, and failures
render the actionable sentence rather than a stack trace.

### T-P1.6 — Environments at a glance
The list shows each environment's last probe result and when it was taken (UC-E9), with staleness
made obvious. A cached "reachable" from three weeks ago is worse than no information, because it
is believed.

*Done when:* the list shows last-probe state and age; results older than the staleness threshold
are visibly marked as stale.

---

## Testing

**Unit.** Per-kind validation tables. Default-storage exclusivity, including the concurrent-edit
case. Duplicate-drops-credential. Delete-with-references warning. Probe result rendering for
every failure mode (host unreachable, auth rejected, `kc.sh` absent, permission denied,
namespace not found, workload not found).

**Contract.** `Executor.Probe` — one table, four implementations. `BlobStore.Probe` — one table,
four implementations.

**Integration.** Probe against every fixture: local install, sshd container, Docker Keycloak,
`kind` cluster. Storage probe against a disk folder, sshd, MinIO and Azurite, each with a valid
credential, a wrong credential, and a read-only credential.

**Manual.** Define one environment of each kind and one storage of each kind from a clean
config, using only the UI. Confirm the resulting `config.yaml` is something you would be happy
to commit — readable, no secrets, no noise.

## Verification

| Requirement | Evidence |
|---|---|
| FR-N1 | Four environment kinds persisted and reloaded — `TestConfig_EnvironmentKinds_RoundTrip`. |
| FR-N2 | Four storage kinds persisted, each with its folder/prefix — `TestConfig_StorageKinds_RoundTrip`. |
| FR-N3 | `TestProbe_Contract` (4 executors) and `TestStorageProbe_Contract` (4 stores), each covering success, wrong credential and unreachable. |
| FR-N4 | `TestConfig_CRUD` covering create, edit, duplicate, delete, including the duplicate-drops-credential and delete-with-references cases. |
| FR-N5 | `TestConfig_DefaultStorageIsExclusive`. |
| UC-E1…E4 | Manual walkthrough: one environment of each kind created through the UI, screenshots attached. |
| UC-E5 · UC-S5 | Probe integration runs against the fixture matrix, output attached. |
| UC-E6…E8 | `TestConfig_CRUD` plus the manual walkthrough. |
| UC-E9 | Screenshot of the environments list showing last-probe state and a deliberately stale entry. |
| UC-S1…S4, S6, S7 | Storage round-trip tests plus the manual walkthrough. |
| NFR-7 | `TestProbe_IsReadOnly`: probe runs against a fixture whose filesystem is asserted unchanged and whose workload generation is unchanged afterwards. |

## Demo

From an empty config, define a local environment and a disk storage, and press **Test** on each.
Then define a Kubernetes environment pointing at a `kind` cluster and press **Test**: it reports
the Keycloak version, where `kc.sh` lives, that a clone can be created, and that the Admin API is
reachable. Break the namespace name and press **Test** again — it says which namespace it could
not find. Open `config.yaml` and show that all of it is legible and none of it is secret.

## Exit criteria

- [ ] All four environment kinds and all four storage kinds can be created, tested, edited,
      duplicated and deleted through the UI.
- [ ] `Test` runs the real `Probe` and reports concrete facts, not a boolean.
- [ ] Exactly one storage can be default.
- [ ] `config.yaml` contains no secret material; every credential is a keychain handle.
- [ ] Probe is proven read-only on the target.

## Commits

```
feat(target): Executor.Probe and the TargetFacts model
feat(target/local,ssh,docker,k8s): probe implementations for all four kinds
feat(config): environment definitions with per-kind validation and CRUD
feat(config): storage definitions, folder rooting and the default flag
feat(store): reachability probe distinguishing read-only from writable
feat(ui): environments and storage configuration screens
feat(ui): last-probe state and staleness in the environments list
test(target): probe contract suite across all four executors
```

## Risks

**Probe over-reaching.** The temptation is to make `Test` "just try a small export", which would
write to a production target. *Mitigation:* `TestProbe_IsReadOnly` asserts the target is
unchanged, and the reviewer's checklist for this phase names it explicitly.

**Kubeconfig variety.** Contexts, exec-plugin auth, OpenShift token auth and proxies vary more
than any other input in the tool. *Mitigation:* delegate entirely to `client-go`'s standard
loading rules rather than parsing kubeconfig ourselves, and treat an auth failure as a reportable
fact with the underlying reason surfaced, not as a bug to work around.
