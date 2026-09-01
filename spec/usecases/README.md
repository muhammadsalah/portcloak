<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# Use Cases

The complete behavioural model for PortCloak: every interaction the tool supports, written as a
structured use case with preconditions, a main success scenario, alternate flows, and
exceptions. These are the bridge between the requirements in
[`../01-vision-and-requirements.md`](../01-vision-and-requirements.md) and the module design in
[`../02-architecture.md`](../02-architecture.md).

## Actors

PortCloak is a **single-user local tool with no login** (N8), reached through a window or a
terminal. There is exactly one human actor, and the systems it talks to are secondary actors.

| Actor | Type | Description |
|-------|------|-------------|
| **Operator** | Primary, human | The platform/IAM engineer at the machine. There is no account, no role, no sign-in — whoever runs the app is the Operator. |
| **Source Keycloak** | Secondary | The Keycloak whose realm is being captured, reached through an **Environment**. |
| **Target Keycloak** | Secondary | The Keycloak a snapshot is restored into, also an **Environment**. |
| **Container / Cluster API** | Secondary | Docker Engine API or Kubernetes/OpenShift API — used to create and destroy the **ephemeral clone**. |
| **Storage Backend** | Secondary | Disk, SSH host, S3-compatible store, or Azure Blob / Azurite, addressed through a **Storage** definition. |
| **OS Keychain** | Secondary | macOS Keychain / Windows Credential Manager / libsecret. Holds every credential; `config.yaml` only holds handles. |

> Because there is no user model, **no use case involves authentication, authorisation, roles,
> sharing or ownership**. Authority is whatever the configured credentials grant.

## Document index

| # | Package | Covers | Use cases |
|---|---------|--------|-----------|
| 01 | [Environments](./01-environments.md) | Defining where Keycloak runs | UC-E1 … UC-E9 |
| 02 | [Storage](./02-storage.md) | Defining where snapshots live | UC-S1 … UC-S8 |
| 03 | [Capture](./03-capture.md) | Producing snapshots | UC-C1 … UC-C12 |
| 04 | [Inspection](./04-inspection.md) | Reading inside snapshots | UC-I1 … UC-I13 |
| 05 | [Restore](./05-restore.md) | Importing snapshots | UC-R1 … UC-R8 |
| 06 | [Operations](./06-operations.md) | Jobs, resilience, config, audit | UC-O1 … UC-O10 |
| 07 | [Traceability](./07-usecase-traceability.md) | Use case → requirement → module | — |
| 08 | [Command line](./08-cli.md) | Driving all of it from a terminal | UC-L1 … UC-L13 |

## Diagrams

| Diagram | Renders |
|---------|---------|
| [`uc-01-overview`](./diagrams/uc-01-overview.puml) — actor/use-case overview | [PNG](./diagrams/png/uc-01-overview.png) · [SVG](./diagrams/svg/uc-01-overview.svg) |
| [`uc-02-configuration`](./diagrams/uc-02-configuration.puml) — environments & storage | [PNG](./diagrams/png/uc-02-configuration.png) · [SVG](./diagrams/svg/uc-02-configuration.svg) |
| [`uc-03-capture`](./diagrams/uc-03-capture.puml) — capture package | [PNG](./diagrams/png/uc-03-capture.png) · [SVG](./diagrams/svg/uc-03-capture.svg) |
| [`uc-04-inspection-restore`](./diagrams/uc-04-inspection-restore.puml) — inspection & restore | [PNG](./diagrams/png/uc-04-inspection-restore.png) · [SVG](./diagrams/svg/uc-04-inspection-restore.svg) |
| [`uc-05-job-lifecycle`](./diagrams/uc-05-job-lifecycle.puml) — job state across use cases | [PNG](./diagrams/png/uc-05-job-lifecycle.png) · [SVG](./diagrams/svg/uc-05-job-lifecycle.svg) |

Re-render with:

```bash
cd spec/usecases/diagrams
plantuml -tsvg -o svg *.puml && plantuml -tpng -o png *.puml
```

## Format

Each use case is written as:

- **Goal** — what the Operator is trying to achieve
- **Preconditions** — what must already be true
- **Trigger** — what starts it
- **Main success scenario** — the numbered happy path
- **Alternate flows** — legitimate variations (`A1`, `A2`, …)
- **Exceptions** — failure handling (`E1`, `E2`, …)
- **Postconditions** — what is true afterwards
- **Covers** — the requirement IDs satisfied

Alternate flows and exceptions are where most of PortCloak's real design lives: bad connections,
ephemeral clone teardown, masked secrets, and missing external dependencies are all failure
paths, and they are specified here rather than left to implementation.

## Conventions

- **Environment** = a configured Keycloak execution context (was "profile").
- **Storage** = a configured snapshot destination (was "sink").
- Every capture produces **one snapshot containing one realm** (FR-S6).
- The **serving Keycloak instance is never exec'd into**; Docker and Kubernetes captures run in
  an ephemeral clone (FR-C9).
- Sessions are **out of scope** (N5); themes and provider JARs are **detected and reported**,
  never migrated (N7).
- There are two front ends over one engine: the window and `pcloak`. They share one
  `~/.portcloak` and each holds an advisory claim on it saying so. Only the startup sweep and a
  change to `config.yaml` need the folder to themselves; everything else runs concurrently
  (UC-L10).
