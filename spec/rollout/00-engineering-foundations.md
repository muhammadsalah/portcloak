<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# 00 — Engineering Foundations

Decisions that apply to every phase: where code lives, what it is built with, how packages are
allowed to depend on one another, and how work is recorded. Settled once here so no phase
document has to relitigate them.

## 0.1 Repository layout

The module map in [02 §2.3](../02-architecture.md) becomes directories directly. A reader who
knows the architecture document can find any component without searching.

```
portcloak/
  cmd/portcloak/           # main package — Wails v3 app bootstrap, nothing else
  internal/
    app/                   # Wails bindings: Capture/Restore/Config/Snapshot/Inspect controllers
    engine/
      orchestrator/        # per-job state machine, progress emission
      target/              # Executor implementations
        local/
        ssh/
        docker/
        k8s/
        clone/             # ephemeral clone derivation + teardown (docker, k8s)
        ports/             # free-port allocation
      kc/                  # kc.sh export/import command building and output parsing
      admin/               # Admin REST client — secret verification, dependency detection
      manifest/            # realm inventory + completeness report
      snapshot/            # tar+zstd packaging, integrity tree, envelope
      crypto/              # age / AES-256-GCM, opt-in
      store/               # BlobStore implementations
        disk/ sftp/ s3/ azure/
      resil/               # retry, backoff, circuit breaker, checkpoints
      config/              # ~/.portcloak/ — config.yaml, jobs/, keychain handles
      obs/                 # slog setup, redaction, audit log, progress events
      inspect/             # tiered open, query API
        index/             # session-scoped SQLite projections
  frontend/                # Wails v3 frontend (see 0.3)
  testdata/                # fixtures — realm exports, malformed bundles, fault scripts
  spec/                    # this specification
  build/                   # packaging, icons, notarisation scripts
```

`internal/` is used throughout: nothing in PortCloak is a library for anyone else, and making
that explicit stops accidental API-shaped design.

## 0.2 Dependency rule

Dependencies point **inward and downward only**:

```
cmd  →  internal/app  →  internal/engine/orchestrator  →  engine/{target,store,kc,...}
                                                       →  engine/{obs,resil,config}
```

Three rules with teeth:

1. **No engine package imports `internal/app`.** The engine must be drivable from a test binary
   with no Wails runtime present. This is what lets the whole capture pipeline be tested headlessly.
2. **`orchestrator` imports only interfaces**, never a concrete adapter. Concrete adapters are
   selected by a registry wired in `internal/app`. This is the seam that makes "add a `podman`
   target later" additive rather than surgical.
3. **`obs` imports nothing from the engine.** Logging must be usable from the lowest layers
   without an import cycle.

A `go test ./internal/engine/...` run must pass with no network, no Docker, and no Keycloak
present. If it does not, a fake is missing.

## 0.3 Toolchain

| Concern | Choice | Why |
|---------|--------|-----|
| Language | Go (module `portcloak`) | Per [12 D6](../12-decisions.md). |
| Desktop shell | **Wails v3** | [D6](../12-decisions.md). Single binary, no server component (NFR-4). |
| Frontend | Wails v3 default stack, plain TypeScript + Vite | The UI is forms, tables and progress. A heavy framework buys little and costs binary size and build complexity. |
| Styling | Hand-written CSS using the Keycloak/PatternFly tokens from the [design file](../lunacy/) | Matches the design system without pulling PatternFly's full React library into a non-React app. |
| SQLite | `modernc.org/sqlite` | **Pure Go, cgo-free.** Keeps cross-compilation for three platforms simple, which a cgo driver would not. Only ever used for throwaway inspection indexes (NFR-11). |
| Compression | `github.com/klauspost/compress/zstd` | Streaming, no cgo. |
| Encryption | `filippo.io/age` | Small, audited, no key-management ceremony. Opt-in per [D8](../12-decisions.md). |
| Keychain | `github.com/zalando/go-keyring` | macOS Keychain / Windows Credential Manager / libsecret behind one API. |
| SSH/SFTP | `golang.org/x/crypto/ssh` + `github.com/pkg/sftp` | |
| S3 | `github.com/aws/aws-sdk-go-v2` | Endpoint-overridable, so MinIO works with the same code path. |
| Azure Blob | `github.com/Azure/azure-sdk-for-go/sdk/storage/azblob` | Points at Azurite by connection string. |
| Kubernetes | `k8s.io/client-go` | Also gives us the exec/SPDY streaming path. |
| Docker | Docker Engine API over the local socket | Avoids shelling out to a CLI that may not be installed. |
| Logging | `log/slog` + a custom redacting handler | Standard library; the handler is ours because redaction is a correctness requirement (NFR-3). |
| Linting | `golangci-lint` | |

**Deliberately not used:** an ORM (there is no persistent relational model — see NFR-11); a
DI framework (the registry in `internal/app` is thirty lines); a workflow engine (the
orchestrator's state machine is small and needs to be readable in a crash report).

## 0.4 Error and context conventions

- Every function that touches the network, a filesystem or a subprocess takes `ctx context.Context`
  as its first parameter. Cancellation is how UC-O3 (cancel a job) and UC-R8 (cancel a restore)
  are implemented; a function without a context is a function that cannot be cancelled.
- Errors wrap with `%w` and carry enough to render an operator-facing sentence. The engine
  produces a typed error with a `Retryable() bool` and a `Hint() string`; the hint is what UC-O5
  ("understand a failure") shows.
- **`Teardown` is called from a `defer` that runs on every exit path, including panic.** This is
  a convention, not a suggestion: see [P3](./05-phase-p3-remote-targets.md) for the test that
  enforces it.

## 0.5 Configuration and secret handling

`~/.portcloak/config.yaml` never contains a secret. It contains a `credentialRef` of the form
`keychain://portcloak/<kind>/<name>`, resolved at use time. Consequences the code must respect:

- Config can be read, diffed and committed by the operator (NFR-11) without leaking anything.
- A config file copied to another machine will fail to connect until credentials are re-entered.
  That is the correct behaviour and the UI says so rather than failing obscurely.
- Tests never touch the real keychain: `config` depends on a `CredentialStore` interface with an
  in-memory fake.

## 0.6 Commit and branch conventions

Trunk-based on `main`. Phases are built on short-lived `phase/pN-<slug>` branches merged with
`--no-ff`, so the phase boundary stays visible in history.

Conventional-commit prefixes, scoped to the package:

```
feat(target/k8s): derive an ephemeral clone spec from a serving workload
fix(store/s3): resume a multipart upload after the process restarts
test(kc): cover the masked-secret export from Keycloak 22
docs(spec): record the decision to drop session portability
chore(build): notarise the macOS bundle
```

The subject says what changed; the body says **why**, and names the requirement or use case when
one motivated the change. A commit body that repeats the diff in prose is noise; a commit body
that explains why the obvious approach was wrong is the reason history is worth keeping.

Commit granularity is *one reviewable idea* — typically one task from a phase document, or one
task split into "interface + fake" then "implementation". A commit that touches five packages
for five unrelated reasons gets split.

## 0.7 Continuous integration

CI runs on every push. It is intentionally cheap enough to run on every push:

| Stage | Command | Gate |
|-------|---------|------|
| Build | `go build ./...` for darwin/linux/windows | must pass |
| Vet & lint | `go vet ./...`, `golangci-lint run` | must pass |
| Unit | `build/ci/coverage.sh` — `go test ./internal/... -race` with a coverage profile | must pass, no network; coverage must not fall below the floor |
| Redaction | `go test ./internal/engine/obs -run TestRedaction` | must pass — separate stage so a failure is unmissable |
| Integration | `go test ./... -tags=integration` against the Keycloak fixture matrix and service containers (MinIO, Azurite, sshd) | must pass on `main` and on phase branches |
| Frontend | `npm ci && npm run check && npm run test:coverage && npm run build` | must pass; coverage must not fall below the floor |

Integration tests are tagged, not skipped by a runtime probe, so a missing Docker daemon
produces "not run" rather than a silent pass. The distinction matters: a green board that
silently skipped every Keycloak test is worse than a red one.

Both suites publish a coverage figure into the job summary, so a run says how much of the code it
exercised rather than only that nothing failed. Both floors are ratchets set just under what the
suite currently reaches: they can be raised in a commit that says why, and lowering one is a
change a reviewer is meant to notice. The Go measurement uses `-coverpkg=./internal/...` rather
than `go test -cover`, because the default grades each package against its own tests and reports
0.0% for a package exercised entirely through a sibling — see the comment at the top of
[`build/ci/coverage.sh`](../../build/ci/coverage.sh).

## 0.8 Versioning

Semantic versioning from 0.0.1. While the major is 0, the **bundle schema version** carried in
the snapshot envelope ([06 §6.4](../06-snapshot-and-manifest.md)) is the compatibility contract
that actually matters — a 0.0.1 bundle must remain readable by later 0.x builds, because bundles
outlive the tool that wrote them. Any change to the envelope requires a schema version bump and
a read path for the previous version.
