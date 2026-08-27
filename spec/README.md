# PortCloak — Design Specification

> **PortCloak** is a cross-platform desktop tool (Go + [Wails v3](https://wails.io)) that
> captures **portable, high-fidelity snapshots of Keycloak realms** — users, credentials,
> clients, secrets, federations and signing keys — from a variety of source environments, and
> restores them into a target Keycloak. **One snapshot holds one realm.**
>
> The name is a play on **Keycloak**: PortCloak *ports the cloak*.

This folder is a **design-only** artifact. No implementation is included yet — the goal is a
well-constructed, reviewable spec (with PlantUML) that shows the planned modules and proves
each requirement is met before any code is written.

## What "high fidelity" means here

The export must be rich enough that an imported realm behaves as a *true clone*:
users can log in with their **existing passwords**, their **OTP/2FA/passkeys keep working**,
clients authenticate with the **same secrets**, LDAP/IdP federations reconnect, and
**tokens signed before the move remain valid** because the realm's **RSA signing keys travel
with the snapshot**.

Two things are deliberately *not* carried, and both are stated up front rather than discovered
later: **sessions** (users re-authenticate — token continuity comes from the carried keys), and
**on-disk deployment assets** like custom themes and provider JARs, which are **detected and
reported** as restore preconditions instead. See [12 — Design Decisions](./12-decisions.md).

Capture never disturbs a running Keycloak: on Docker and Kubernetes the export runs inside an
**ephemeral clone** of the workload, and on local/SSH hosts it binds **automatically allocated
free ports**.

PortCloak is a **single-user local tool with no login**. It is configured with **Environments**
(where Keycloak runs — local folder, SSH host, Docker service, or K8s namespace + workload) and
**Storage** definitions (where snapshots live — a folder on disk, over SSH, in an S3 prefix, or
an Azure container). Configuration is a readable file under `~/.portcloak/`; SQLite is used only
for throwaway snapshot indexes.

## Document index

| # | Document | Purpose |
|---|----------|---------|
| 00 | [`README.md`](./README.md) | This index |
| 01 | [`01-vision-and-requirements.md`](./01-vision-and-requirements.md) | Vision, goals/non-goals, glossary, full functional + non-functional requirements |
| 02 | [`02-architecture.md`](./02-architecture.md) | Module architecture, core interfaces, technology choices |
| 03 | [`03-capture-targets.md`](./03-capture-targets.md) | Local / SSH / Docker / Kubernetes-OpenShift adapters |
| 04 | [`04-storage-backends.md`](./04-storage-backends.md) | Disk / SSH volume / S3 / Azurite storage backends |
| 05 | [`05-resilience.md`](./05-resilience.md) | Bad-connection tolerance: retries, resume, integrity, circuit breaking |
| 06 | [`06-snapshot-and-manifest.md`](./06-snapshot-and-manifest.md) | Snapshot bundle format, packaging, encryption |
| 07 | [`07-realm-carryover-manifest.md`](./07-realm-carryover-manifest.md) | **The manifest**: every realm detail carried over, secret-by-secret |
| 08 | [`08-security.md`](./08-security.md) | Secret handling, encryption-at-rest, keychain, threat model |
| 09 | [`09-workflows-and-ui.md`](./09-workflows-and-ui.md) | Capture/restore workflows and the Wails UI |
| 10 | [`10-snapshot-inspection.md`](./10-snapshot-inspection.md) | **Viewing inside a snapshot**: details, users, entities, search, diff |
| 11 | [`11-traceability.md`](./11-traceability.md) | Requirement → module traceability matrix |
| 12 | [`12-decisions.md`](./12-decisions.md) | **Decision record** — the nine confirmed design decisions, with rationale and cost |
| — | [`usecases/`](./usecases/README.md) | **Use cases** — the complete behavioural model (60 use cases across 6 packages) with its own traceability matrix |
| — | [`rollout/`](./rollout/README.md) | **Rollout plan** — the nine phases that build this, with coding tasks, tests, verification evidence and the 0.0.1 release gate |
| — | [`lunacy/`](./lunacy/README.md) | **Screen designs** — the Lunacy document index, design tokens, and which screen covers which use case |

## Diagrams

Sources live in [`diagrams/`](./diagrams) as `.puml`. Rendered output is committed alongside
them so the docs display without any tooling: [`diagrams/svg/`](./diagrams/svg) and
[`diagrams/png/`](./diagrams/png). Markdown embeds the PNGs; each figure links its source and SVG.

| Diagram | Renders | Used in |
|---------|---------|---------|
| [`01-context`](./diagrams/01-context.puml) — system context (C4 L1) | [PNG](./diagrams/png/01-context.png) · [SVG](./diagrams/svg/01-context.svg) | [01](./01-vision-and-requirements.md) |
| [`02-containers`](./diagrams/02-containers.puml) — container / runtime view (C4 L2) | [PNG](./diagrams/png/02-containers.png) · [SVG](./diagrams/svg/02-containers.svg) | [02](./02-architecture.md) |
| [`03-components`](./diagrams/03-components.puml) — engine component detail (C4 L3) | [PNG](./diagrams/png/03-components.png) · [SVG](./diagrams/svg/03-components.svg) | [02](./02-architecture.md) |
| [`04-domain-model`](./diagrams/04-domain-model.puml) — snapshot + manifest model | [PNG](./diagrams/png/04-domain-model.png) · [SVG](./diagrams/svg/04-domain-model.svg) | [06](./06-snapshot-and-manifest.md) |
| [`05-capture-sequence`](./diagrams/05-capture-sequence.puml) — capture sequence | [PNG](./diagrams/png/05-capture-sequence.png) · [SVG](./diagrams/svg/05-capture-sequence.svg) | [03](./03-capture-targets.md), [09](./09-workflows-and-ui.md) |
| [`06-restore-sequence`](./diagrams/06-restore-sequence.puml) — restore / import | [PNG](./diagrams/png/06-restore-sequence.png) · [SVG](./diagrams/svg/06-restore-sequence.svg) | [09](./09-workflows-and-ui.md) |
| [`07-storage-class`](./diagrams/07-storage-class.puml) — storage & transfer contracts | [PNG](./diagrams/png/07-storage-class.png) · [SVG](./diagrams/svg/07-storage-class.svg) | [04](./04-storage-backends.md) |
| [`08-resilience-state`](./diagrams/08-resilience-state.puml) — transfer state machine | [PNG](./diagrams/png/08-resilience-state.png) · [SVG](./diagrams/svg/08-resilience-state.svg) | [05](./05-resilience.md) |
| [`09-engine-overview`](./diagrams/09-engine-overview.puml) — single-binary overview | [PNG](./diagrams/png/09-engine-overview.png) · [SVG](./diagrams/svg/09-engine-overview.svg) | [02](./02-architecture.md) |
| [`10-packaging-pipeline`](./diagrams/10-packaging-pipeline.puml) — seal & upload pipeline | [PNG](./diagrams/png/10-packaging-pipeline.png) · [SVG](./diagrams/svg/10-packaging-pipeline.svg) | [06](./06-snapshot-and-manifest.md) |
| [`11-bundle-layout`](./diagrams/11-bundle-layout.puml) — `.pck` contents & sidecars | [PNG](./diagrams/png/11-bundle-layout.png) · [SVG](./diagrams/svg/11-bundle-layout.svg) | [06](./06-snapshot-and-manifest.md) |
| [`12-ui-information-architecture`](./diagrams/12-ui-information-architecture.puml) — UI IA | [PNG](./diagrams/png/12-ui-information-architecture.png) · [SVG](./diagrams/svg/12-ui-information-architecture.svg) | [09](./09-workflows-and-ui.md) |
| [`13-inspect-sequence`](./diagrams/13-inspect-sequence.puml) — tiered snapshot inspection | [PNG](./diagrams/png/13-inspect-sequence.png) · [SVG](./diagrams/svg/13-inspect-sequence.svg) | [10](./10-snapshot-inspection.md) |
| [`14-inspection-model`](./diagrams/14-inspection-model.puml) — inspection index model | [PNG](./diagrams/png/14-inspection-model.png) · [SVG](./diagrams/svg/14-inspection-model.svg) | [10](./10-snapshot-inspection.md) |
| [`15-ephemeral-capture`](./diagrams/15-ephemeral-capture.puml) — ephemeral clone execution | [PNG](./diagrams/png/15-ephemeral-capture.png) · [SVG](./diagrams/svg/15-ephemeral-capture.svg) | [03](./03-capture-targets.md) |

### Re-rendering after editing a `.puml`

```bash
cd spec/diagrams
plantuml -tsvg -o svg *.puml
plantuml -tpng -o png *.puml
```

Requires `plantuml` and Graphviz (`brew install plantuml graphviz`). The `@startuml <name>` id
in each file matches its filename, so output names stay stable.

## Reading order

1. Start with **01** for the problem framing and requirements.
2. Read **02** for the module map, then **07** for the definitive list of what gets carried.
3. **03–06** are the "how", **08** is the "safely", **09–10** are the "as a user".
4. **11** closes the loop: every requirement points at the module(s) that satisfy it.
5. **12** records the decisions that shaped all of the above — read it if you want the *why*
   behind the scope boundaries.
6. **[`usecases/`](./usecases/README.md)** is the behavioural view: every interaction, with its
   alternate flows and failure paths. Read it when you want *what the tool actually does*
   rather than how it is built.
7. **[`rollout/`](./rollout/README.md)** is the build view: the order everything above comes into
   existence, what code gets written in each phase, and how each requirement is proved before
   the next phase starts. Read it when you are ready to start writing code.
