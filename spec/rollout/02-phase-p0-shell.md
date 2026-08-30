<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# P0 — Shell & Foundations

**Goal.** PortCloak becomes a program you can launch. It opens a window, creates and owns
`~/.portcloak/`, reads and writes a `config.yaml` it can validate, resolves credentials through
the OS keychain, and writes a structured log that cannot contain a secret. Nothing is captured
and nothing is stored yet — but every later phase inherits these foundations, and the two that
are hardest to retrofit (redaction and file-based state) are settled here.

**Covers.** UC-O7, UC-O9 · FR-N6 · NFR-3 (partial: logging + keychain), NFR-4, NFR-5 (partial),
NFR-11 · N8 (no login).

**Depends on.** Nothing.

**Packages.** `cmd/portcloak`, `internal/app`, `engine/config`, `engine/obs`, `frontend/`.

---

## Tasks

### T-P0.1 — Wails v3 application shell
Bootstrap the Wails v3 app in `cmd/portcloak`: one window, the eight-item navigation rail from
the design file, and empty routed views. Wire the frontend build into `go build` so a single
`go build ./cmd/portcloak` produces a runnable binary on all three platforms.

*Done when:* the binary launches on macOS, Linux and Windows, the nav renders with Keycloak
styling, and no view yet claims to do anything it cannot.

### T-P0.2 — `~/.portcloak/` bootstrap
Create the directory tree from [02 §2.6](../02-architecture.md) on first run —
`config.yaml`, `jobs/`, `logs/`, `index/` — with `0700` on the root and `0600` on files.
Missing directories are recreated on every start, not only the first, so deleting one does not
brick the app.

*Done when:* a fresh account launches the app and gets a valid tree; deleting the tree and
relaunching regenerates it; a pre-existing tree is left alone.

### T-P0.3 — Config schema, load, validate, save
Define the `config.yaml` schema (environments, storage, preferences) as Go types with explicit
YAML tags. Loading validates and returns **all** problems at once with line numbers, not the
first one. Saving writes atomically — temp file, `fsync`, rename — because a half-written
config on a crash would lose every environment the operator has defined.

Unknown fields are preserved on round-trip rather than dropped: a config written by a newer
build and opened by an older one must not silently lose entries.

*Done when:* a hand-edited config round-trips byte-stably; a malformed config produces a list of
human sentences naming the line; a crash during save (simulated) leaves the previous config intact.

### T-P0.4 — Credential store over the OS keychain
`CredentialStore` interface with a `go-keyring` implementation and an in-memory fake for tests.
Handles are `keychain://portcloak/<kind>/<name>` and resolve at use time only. A missing
credential is a distinct, recognisable error — "the credential for `prod-eu` is not in this
machine's keychain", which is exactly what an operator sees after copying a config between
machines.

*Done when:* store/fetch/delete round-trips on the host OS; the fake satisfies the same contract
test; no test run touches the real keychain.

### T-P0.5 — Redacting `slog` handler
The handler from [01 §1.8](./01-test-strategy.md#redaction): redaction by key name and by value
shape, recursing into groups, structs and wrapped errors. Log rotation with a size cap. This is
built **before** anything that handles a secret exists, so there is never a build in which a
secret could be logged.

*Done when:* the redaction suite passes, including the hostile case where a client's *name* looks
like a PEM block and must survive intact.

### T-P0.6 — Progress and event plumbing
The `obs` progress event type and the Wails event-bus bridge that carries it to the frontend,
with a fake sink for headless tests. No job produces events yet; the pipe is built and tested
so P2 can emit into something that already works.

*Done when:* a synthetic event emitted in Go renders in the UI; the same code path runs under
`go test` with no Wails runtime present.

### T-P0.7 — First-run and empty states
The empty states for the snapshot library and both configuration screens, each pointing at the
one action that makes sense next. An operator's first thirty seconds decide whether the tool
seems trustworthy; an empty grid with no explanation does not.

*Done when:* a fresh install shows a first-run state that names the next step (UC-O9).

---

## Testing

**Unit.** Config validation across a table of malformed files (unknown kind, duplicate name,
missing required field per kind, two storages marked default). Atomic-save crash simulation.
Credential store contract test, run against both the real and fake implementations. Redaction
suite (§1.8).

**Contract.** `CredentialStore` — one table, two implementations.

**Integration.** Launch-and-quit on all three platforms in CI; assert the tree is created with
the right permissions and the log file is written.

**Manual.** Delete `~/.portcloak/`, launch, and observe the first-run experience. Hand-edit
`config.yaml` in an editor, restart, confirm the change is picked up and nothing was reformatted
(UC-O7).

## Verification

| Requirement | Evidence |
|---|---|
| FR-N6 · NFR-11 | `config.yaml` exists after first run, is valid YAML, is readable, and round-trips byte-stably under `TestConfigRoundTrip`. |
| NFR-3 (logging) | The redaction CI stage is green, including `TestRedaction_PEMShapedClientName`. |
| NFR-3 (credentials) | `TestCredentialStore_Contract` passes on the host keychain; `grep -r` over `testdata/` and a produced `config.yaml` finds no secret material. |
| NFR-4 | Three platform binaries built in CI, each launching standalone with no runtime dependency. |
| NFR-5 (partial) | A structured log file with a synthetic progress event, attached to the phase record. |
| N8 | No sign-in surface exists — verified by inspection of the routed views; there is no session, user or role type in the codebase. |
| UC-O7 | Manual walkthrough recorded: hand-edit, restart, change visible. |
| UC-O9 | Manual walkthrough recorded: fresh launch, first-run state. |

## Demo

Delete `~/.portcloak/`. Launch the binary. The window opens on an empty snapshot library that
explains what to do first. Quit, open `~/.portcloak/config.yaml` in an editor, add a preference
by hand, relaunch — the preference is there. Open `logs/portcloak.log` and show it is structured
and readable.

## Exit criteria

- [ ] Binary builds and launches on macOS, Linux and Windows.
- [ ] `~/.portcloak/` is created, permissioned, and self-heals.
- [ ] Config loads, validates with useful messages, and saves atomically.
- [ ] Credentials resolve through the OS keychain; tests use the fake.
- [ ] The redaction CI stage exists and is green.
- [ ] Progress events flow from Go to the UI.

## Commits

```
chore(build): wails v3 shell and cross-platform build
feat(config): ~/.portcloak layout and atomic config persistence
feat(config): credential handles resolved through the OS keychain
feat(obs): redacting slog handler
test(obs): redaction property suite and the PEM-shaped-name case
feat(obs): progress events over the wails event bus
feat(ui): navigation rail, routed views and first-run empty states
```

## Risks

**Wails v3 maturity.** v3 is newer than v2; an API may move under us. *Mitigation:* the Wails
surface is confined to `cmd/portcloak` and `internal/desktop`. The engine never imports it, so a
breaking change costs one adapter, not a rewrite. If v3 proves unworkable, falling back to v2 is
a change to two packages.

**Keychain behaviour differs by platform.** Linux needs a running secret service, which headless
CI does not have. *Mitigation:* the contract test runs against the fake everywhere and against
the real store only on developer machines and on CI runners that provide one; a missing service
produces a clear "not run", never a silent pass.
