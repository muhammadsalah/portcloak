<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# P3 — Remote Targets

**Goal.** The same capture pipeline reaches Keycloak wherever it actually runs: over SSH, inside
a Docker host, and inside a Kubernetes or OpenShift cluster. On Docker and Kubernetes the export
runs in an **ephemeral clone** — a parked copy of the serving workload — so the instance serving
real logins is only ever read, never disturbed. SFTP joins the storage backends.

This is the highest-risk phase in the plan. A mistake here does not corrupt a bundle; it takes
down someone's production login.

**Covers.** UC-C2, UC-C3, UC-C4, UC-C11, UC-C12 · FR-C2, FR-C3, FR-C4, FR-C9, FR-C11 · FR-S2 ·
NFR-7.

**Depends on.** P0, P1, P2.

**Packages.** `engine/target/ssh`, `engine/target/docker`, `engine/target/k8s`,
`engine/target/clone`, `engine/store/sftp`.

---

## Build the destruction before the creation

The tasks are ordered so that **teardown, orphan sweeping and label stripping exist before
anything can create a clone**. T-P3.3 writes the tests that assert nothing is ever left behind;
T-P3.4 and T-P3.5 then write the code that has to satisfy them.

This inversion is deliberate. The natural order — make a clone, get an export working, add
cleanup — produces a window, however short, in which a crashed developer session leaves parked
containers in a real cluster. Building cleanup first means that window never exists, and it
forces the label question to be answered before any pod is ever created with inherited labels.

## Tasks

### T-P3.1 — SSH executor
Full `Executor` over `golang.org/x/crypto/ssh`: key, agent and password auth, optional jump
host, remote free-port allocation, remote temp directory, and `FetchDir` streaming over SFTP
with SHA-256 computed in flight. Host-key verification is on, with a first-connection decision
surfaced to the operator — never a silent accept.

*Done when:* the `Executor` contract suite passes against an sshd fixture for all three auth
modes and through a jump host; the remote temp directory is gone on every exit path.

### T-P3.2 — SFTP storage backend
`BlobStore` over the same SSH transport, rooted at the configured folder.

*Done when:* the `BlobStore` contract suite passes unchanged against SFTP — the same table the
disk store passes, including zero-byte objects and empty prefixes.

### T-P3.3 — Clone lifecycle: teardown, sweeping, and the label rules
Written **first**, against a fake clone platform, so the guarantees are pinned before any real
platform is touched:

- `Teardown` runs from a `defer` on every exit path — success, `kc.sh` failure, fetch failure,
  context cancellation, and panic.
- The label policy: strip every inherited label, `ownerReferences`, `resourceVersion`, `uid`,
  `nodeName`, `status` and controller references; apply only `portcloak.io/job=<id>` and
  `portcloak.io/ephemeral`. Keep `imagePullSecrets`, `serviceAccountName`, DB TLS volumes,
  `securityContext`, `nodeSelector` and `tolerations` — a clone that will not schedule, or that
  OpenShift's SCC rejects, is useless.
- The orphan sweep on launch (UC-C12): find anything carrying `portcloak.io/ephemeral`, show what
  it is and how old, and offer to remove it. Offered, not automatic — the operator's cluster is
  not ours to garbage-collect without asking.

*Done when:* the teardown test is parameterised over all five exit paths and green against the
fake; the label-derivation unit test asserts the full strip list and the full keep list.

### T-P3.4 — Docker executor with ephemeral clone
Docker Engine API over the local socket, `DOCKER_HOST`, or SSH. Read the serving container's
config, derive a clone with the same image and environment, replace the entrypoint with a hang,
publish no ports, remove health checks, **strip network aliases** — the Docker equivalent of the
label trap, where a service-discovery alias would route traffic to a container that serves
nothing — then exec the export inside it and stream artifacts out while it is still alive.

*Done when:* capture succeeds against a Docker Keycloak fixture; the serving container's restart
count and start time are unchanged afterwards; no clone survives any exit path.

### T-P3.5 — Kubernetes / OpenShift executor with ephemeral clone
`client-go` against the configured context and namespace. Read the `Deployment` or `StatefulSet`
spec, derive a hung clone pod under the label rules from T-P3.3, wait for `Running`, exec the
export over SPDY with live stdout/stderr, stream the artifacts out through the exec channel, and
delete the clone. `ttlSecondsAfterFinished` is set as a backstop for the case where our own
deletion never runs because the machine died.

`Probe` is extended to check `ResourceQuota` headroom and report up front when the namespace
will refuse the clone, rather than failing halfway through a capture the operator has already
started.

*Done when:* capture succeeds against a `kind` cluster and against an OpenShift-like SCC
configuration; the label trap test passes; the serving workload's generation, pod UIDs and
restart counts are unchanged.

### T-P3.6 — Progress and clone visibility in the UI
The capture screen shows which execution mode is in use and, for clone modes, that a clone was
created and then destroyed. An operator watching a capture against production should be able to
see, without reading logs, that a clone exists right now and that it is gone at the end.

*Done when:* mode and clone lifecycle are visible live and recorded in the job's provenance.

---

## Testing

**Unit.** Clone spec derivation: the full strip list, the full keep list, and a golden-file test
per platform so an accidental change to what is kept shows up as a diff. Teardown across all
five exit paths against the fake. Orphan-sweep matching and age reporting.

**Contract.** `Executor` (local, ssh, docker, k8s). `BlobStore` (disk, sftp).

**Integration.** Capture the `rich` fixture through each of the four targets and assert the four
bundles carry identical inventories — the target must not change what is captured. Plus the
do-no-harm suite from [01 §1.6](./01-test-strategy.md):

- **The label trap test.** A `Service` selects the serving workload. Run a capture. Assert the
  clone never appears in that `Service`'s endpoints, at any point.
- **Serving workload untouched.** Generation, pod UIDs and restart counts unchanged.
- **Teardown always**, on real platforms, for all five exit paths.
- **Leak sweep** at the end of the run: any surviving `portcloak.io/ephemeral` object fails the
  build, whichever test left it.

**Fault injection.** Drop the SSH connection mid-fetch. Kill the kubelet connection mid-exec.
Kill PortCloak itself mid-capture, restart it, and assert the orphan sweep finds and offers to
remove the clone (this is the UC-C12 acceptance test, and the reason the sweep exists).

**Manual.** Run a capture against a Kubernetes namespace while watching `kubectl get pods -w`
in another window. The clone appears, does nothing, and disappears. Log in to the serving
Keycloak throughout and confirm nothing about it changes.

## Verification

| Requirement | Evidence |
|---|---|
| FR-C2 | `TestCapture_SSH_Rich`; `Executor` contract suite green for SSH across three auth modes and a jump host. |
| FR-C3 | `TestCapture_Docker_Rich` plus `TestDocker_ServingContainerUntouched`. |
| FR-C4 | `TestCapture_K8s_Rich` on `kind` and `TestCapture_OpenShiftSCC`. |
| FR-C9 | `TestCloneSpec_Derivation` golden files (docker, k8s) and `TestLabelTrap_ServiceNeverRoutesToClone`. |
| FR-C11 | `TestTeardown_AllExitPaths` (5 paths × 2 platforms) and the end-of-run leak sweep. |
| FR-S2 | `BlobStore` contract suite green for SFTP. |
| NFR-7 | `TestServingWorkloadUntouched` — generation, pod UIDs and restart counts asserted unchanged across every clone capture. |
| UC-C11 | `TestTeardown_AllExitPaths` plus the recorded `kubectl get pods -w` walkthrough. |
| UC-C12 | `TestOrphanSweep_AfterCrash` — kill mid-capture, restart, sweep offers the orphan. |
| Target parity | `TestCaptureParity_AllTargets` — identical inventories from the same realm through all four targets. |

## Demo

Point PortCloak at a production-shaped Kubernetes namespace where Keycloak is serving traffic.
Start a capture with `kubectl get pods -w` on screen. A clone pod appears, stays `Running` and
serves nothing; the export streams live into the progress panel; the clone disappears. Throughout,
log in against the real Keycloak — unaffected. Then kill PortCloak mid-capture and relaunch: it
notices the orphaned clone, says how old it is, and offers to remove it.

## Exit criteria

- [ ] All four target kinds capture the `rich` fixture to identical inventories.
- [ ] The label trap test passes on Kubernetes and its Docker network-alias equivalent passes too.
- [ ] Teardown is proven on all five exit paths on both clone platforms.
- [ ] The end-of-run leak sweep is clean.
- [ ] The serving workload is provably untouched.
- [ ] SFTP passes the same `BlobStore` contract suite as disk.

## Commits

```
feat(target/ssh): executor over ssh with jump host and remote port allocation
feat(store/sftp): sftp blob store over the shared ssh transport
test(target/clone): teardown across every exit path, against a fake platform
feat(target/clone): label stripping rules and clone spec derivation
feat(target/clone): orphan sweep on launch
feat(target/docker): ephemeral clone execution over the engine API
feat(target/k8s): ephemeral clone execution with SPDY exec streaming
feat(target/k8s): quota headroom check during probe
feat(ui): execution mode and clone lifecycle visible during capture
test(target): capture parity across all four targets
```

## Risks

**The label trap is easy to reintroduce.** A later change that copies "just the useful labels"
brings it straight back. *Mitigation:* the golden-file derivation test plus the endpoint
assertion, and a note in the clone package's doc comment explaining why the strip list is total
rather than selective.

**OpenShift is not Kubernetes.** SCCs reject pods that plain Kubernetes accepts, and the default
service account differs. *Mitigation:* an OpenShift-like SCC fixture in the matrix from the start,
and `securityContext` preserved rather than normalised.

**Exec streaming is fiddly.** SPDY exec can deadlock on large stdout while stdin is unread.
*Mitigation:* stream artifacts as a tar over the exec channel with bounded buffering, and cover
it with the `large` fixture, where the deadlock would actually manifest.

**Clone capacity.** A tight `ResourceQuota` refuses the clone. *Mitigation:* `Probe` reports
headroom before the operator starts, so the refusal is a sentence at step one rather than a
failure at step four.
