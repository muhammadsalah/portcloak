<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# Changelog

All notable changes to PortCloak are recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). Tags prefixed `spec-` mark the design
record; unprefixed `v` tags mark shipped binaries.

## [0.0.1] — 2026-08-28

The first version where the whole loop closes: capture a realm, put it somewhere, read it back,
and restore it. All four target kinds, all four storage backends, inspection and restore.

Everything below *Added* arrived after that list was written, and everything below *Fixed* came
out of the first run against real Docker. Those faults were found by driving the built
application against live Keycloak containers rather than by reading the code, and each one is
written up in [`spec/notes/`](./spec/notes/README.md) with the test that keeps it from
returning.

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
- Resume driven by asking the destination what it already holds — `ListParts` and
  `GetBlockList` on S3 and Azure, the file itself on disk and SFTP — so an upload survives
  PortCloak being restarted. A checkpoint is a hint; the destination is the authority on where
  a transfer actually got to, and it positions the source reader to match.
- A resumed upload commits a prefix it never sent, so the checkpoint carries the rolling SHA-256
  state alongside the offset and the finished object is verified against the digest computed
  before the transfer — the same check a fresh upload gets. Where no usable state survives, the
  prefix is re-read rather than trusted.

### Resilience
- Retry, backoff with full jitter and per-endpoint circuit breaking, applied by wrapping every
  adapter rather than by retry code at call sites. An unclassified error defaults to terminal,
  so a new failure mode surfaces instead of silently looping.
- An unreplayable stream is never retried — resume from the checkpoint is the mechanism there,
  because retrying one would commit a truncated object.
- A retry starts from the last checkpoint the failed attempt reached rather than from zero.
  Re-sending 400 MB because an upload dropped at 390 MB is what the checkpoint exists to
  prevent.

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

### Testing
- One contract table per seam, run against every implementation: `BlobStore` across disk, SFTP,
  S3 and Azure, and `Executor` across local, SSH, Docker and Kubernetes. A divergence is a bug
  in the newest adapter rather than a reason to fork the table.
- Offset resume is its own table, so disk and SFTP prove the same guarantees rather than one
  being tested and the other assumed.
- Integration tests sit behind `-tags=integration` against real MinIO, Azurite, sshd and Docker.
  A missing container reads as "not run", never as a silent pass, and CI fails if any ephemeral
  clone outlives the run.
- Every use case and requirement in the rollout matrix names the test that would fail if it
  stopped being met, and CI checks those names still resolve.

### Application
- Wails v3 desktop shell with the eight screens from the design file. No sign-in, because there
  is no account: configuration is plain YAML in `~/.portcloak/` and every credential lives in
  the OS keychain, referenced by handle.
- Redacting `slog` handler from the first commit, with its own CI stage so a failure there is
  unmissable.

### Licence
- **Apache License 2.0.** `LICENSE` is the Apache Software Foundation's text byte-identical to
  the canonical copy, placeholder appendix and all, so it can be diffed against the published one
  and shown to be unaltered; the attribution is in `NOTICE`, which is where section 4 puts it.
  Both ship inside every artifact `build/package.sh` produces — a `.app`, a `.zip` and a tarball
  handed to someone are each a redistribution — and the terms are reachable from the About panel,
  for the people who never see the repository.
- Every file that can carry a comment names its terms: `SPDX-License-Identifier: Apache-2.0`. A
  page lifted out of here says what it is without the reader having to find the root.

### Added
- The frontend is React with a styled-components design system. It replaced thirty lines of
  `h()` standing in for a framework, which the machine did not mind and a person reading
  `views/inspector.ts` did: one screen was one function that cleared a container and rebuilt it.
  The screens are the same screens — every validation rule and every gate moved across unchanged.
- **Keys** — a screen, a `keys:` section in `config.yaml`, and a place for the material to live.
  Generate or import an age keypair, or remember a passphrase by name; the secret half goes to
  this machine's keychain and the file carries only the name, the kind, the public half and a
  handle. Captures seal to a key by name instead of to a pasted public key. Deleting one asks for
  its name to be typed, because a key is in use by every snapshot ever sealed with it — in
  backends PortCloak may not even be configured for.
- **Restore and Inspect open a snapshot with a stored key without asking.** The attempts happen
  while reading the envelope, which is the cheapest proof a key works: nothing first, then
  whatever was typed, then each stored key — identities before passphrases, because an identity
  attempt is free and scrypt deliberately is not. Whichever key worked is named on the screen and
  in the audit log; the key field remains as an override.
- **`kcPath` on Docker and Kubernetes environments** — where `kc.sh` actually lives inside the
  container. PortCloak derived it from `KEYCLOAK_HOME` and otherwise assumed the official images'
  path, which a custom build makes wrong; the failure landed deep inside the export.
- Key lifecycle in the audit log: created, imported, revealed, deleted.
- [`spec/notes/`](./spec/notes/README.md) — the gotchas that reached working code, each with its
  symptom, its cause, the rule, and the guard test that fails if it comes back.
- Real `kc.sh export --help-all` output from four Keycloak versions in `testdata/kc-help`, which
  is what the option-support tests run against.
- Empty states on Environments and Storage, saying what the thing is and where the file lives.
- **Settings** — a screen for the things PortCloak does to itself. Where it keeps its files, the
  orphaned-clone sweep and the local working-data purge moved here off the audit screen, which
  had become a record of what happened with four buttons in it that make things happen. Audit is
  a record again, full width, with a time range beside the action filter.
- **The folder PortCloak keeps its files in can be moved.** `config.yaml`, the audit log, job
  checkpoints, logs and working files go with it; this machine's keychain and every stored
  snapshot do not. The move is refused before a byte shifts if a snapshot is open or a job is in
  flight, and the running application follows the folder rather than asking to be restarted.
  Resolution order is `PORTCLOAK_HOME` → the chosen folder → `~/.portcloak`, and the note
  recording the choice lives in the OS's per-application settings folder, because a note saying
  where the folder went cannot live in the folder that went.
- Icons in the navigation rail, one per screen, drawn on the thing the screen acts on — a tray
  with an arrow into it for Capture, the same tray with the arrow coming out for Restore.
- **A logo** — [`assets/logo/`](./assets/logo/README.md). A padlock whose shackle is a restore
  arc: sealed, and put back. Extracted verbatim from the design source in `spec/design/`, with
  the five variants the sheet calls for and the rules around them. It is the masthead, the first
  run's empty state and the favicon. Its two colours are brand tokens rather than interface ones,
  because the interface stays on the Keycloak admin console palette it was built to sit beside —
  the mark is the one place the teal appears.
- `homeMoved` in the audit log. Every entry above it was written somewhere else.

### Fixed
- **An empty configuration never got past its loading spinner.** Nil slices reached the frontend
  as `null`, and the first `.length` threw after the spinner went up and before anything replaced
  it. Lists now cross the bridge as `[]` and maps as `{}`, enforced once at the boundary rather
  than at each construction site.
- **Every configuration field arrived under its Go name.** `config.Environment`, `Storage` and
  `Preferences` were tagged for YAML only, so the environments list rendered the right number of
  rows with every field blank. Inbound worked, which is what hid it.
- **`kc.sh export` was passed options it does not have.** `--http-port` and `--https-port` are
  options of neither `export` nor `import` on any Keycloak measured, and `--http-management-port`
  exists only between 25.0 and 26.3. The invocation is now built from what the binary reports,
  because that answer changed twice in three minor releases.
- **The restore wizard asked for no decryption key.** It sent an empty one to both the pre-flight
  open and the apply. The key is collected on the step that promises decryption runs first, and
  carried to the import — which opens the bundle again on its own.
- **A push into an ephemeral clone landed unreadable, when it landed at all.** The clone path
  created no parent directory where local and SSH always had, and wrote the realm as `root:root`
  into an image that runs as `keycloak`. Both are fixed; the mode stays 0600, because the answer
  is the right owner rather than a looser file.
- **A restore could leak an ephemeral clone.** Its teardown `defer` was registered after
  `Prepare`'s error check, so a `Prepare` that failed after creating the clone abandoned one
  holding the serving instance's database credentials. The capture path was fixed for this;
  restore was not.
- **Only one snapshot could be browsed per run.** The inspection index is one database per
  snapshot, and the on-disk form always was — but the in-memory form used for small realms opened
  a shared-cache database under a constant name, so every index in the process was the same one
  and the second snapshot's Users tab failed with `table users already exists`. Related:
  re-opening a snapshot that was already open replaced its session without closing the one it
  displaced, orphaning a decrypted working directory.
- **A run that was happening did not look like one.** Phases were announced to the event stream
  and nowhere else, so the job record never learned which phase it was in and anything re-reading
  it drew a pipeline with no live step. A batch of realms reported its shared probe and clone
  under the first realm's job id alone, leaving every other card blank through the slowest part
  of the run. The clone phase was started and never completed — on a local target, which creates
  no clone, permanently. And the Activity screen drew itself once and patched three elements, so
  a finished capture still looked stuck until the screen was left and reopened.
- **The Admin API user and credential were invisible while being filled in.** They appeared only
  once a base URL had been typed, and nothing on that form redraws while you type, so they turned
  up only after leaving the screen and coming back.
- A second Name field on the Kubernetes tab bound to the same value as the one above it. It is
  now the kubeconfig path, which the model has carried all along with nowhere to enter it.
- A view that threw left its spinner on screen forever. Views are now invoked through a guard
  that puts the failure on the pane instead.
- **Every Kubernetes capture exported successfully and then failed collecting the files.** The
  clone was read with `tar cf -` over the exec channel, which is how `kubectl cp` works — and the
  official Keycloak image has no tar. It is assembled on ubi-micro, which ships neither tar nor
  gzip. So `kc.sh` logged `Export finished successfully`, the pod was healthy, the clone was torn
  down cleanly, and the job died at *"The stream from the clone ended unexpectedly"*: the exec
  channel closing on a binary that does not exist. Nothing said which binary, because that
  command's stderr was going to `io.Discard`.

  PortCloak now streams the directory itself, framed as `PCF <size> <name>` and the bytes, using
  only `sh`, `find`, `wc`, `cat` and `printf` — a far smaller contract than tar's, and one no
  image can be missing. The restore path's `tar xf -` is gone the same way. Stderr is kept and
  put in the failure, so the next missing binary names itself. A non-zero exit is now terminal
  rather than retryable: a directory that is not there does not become there on the fourth
  attempt. And `hasTar` is no longer asserted `true` on Kubernetes without anyone having looked —
  it was the assumption underneath all of this.

  Docker was never affected: it reads through the daemon's own archive endpoint, which tars
  outside the container.
- **An interrupted encrypted capture could not be resumed at all.** The job recorded `encrypted:
  true` and nothing else, so resuming rebuilt the configuration as "on, mode unset" and the run
  was refused before it started with *"Encryption is on but no mode was chosen"* — an internal
  complaint about a field the operator never filled in, and no way forward but capturing the
  whole realm again. The job now carries the mode and the recipients, which are public keys and
  belong on disk. The passphrase does not and never will: that one is asked for again on resume,
  and the Activity screen knows to prompt rather than discovering it from a rejection. A job
  written before the mode was recorded says so and points at the Capture screen.
- **Every Kubernetes capture died at the clone step.** The `created-at` label was written as
  RFC 3339, which renders `2026-08-27T18:54:58Z`; a label value may not contain a colon, so the
  API server rejected the whole pod — over a field nothing reads back, since both platforms take
  a clone's age from the object's own creation timestamp. The label is now basic ISO 8601
  (`20260827T185458Z`): same instant, still sortable, still legible under `--show-labels`.
  The sanitiser was only ever applied to the realm name, so the fix is that **every** label value
  goes through it on the way out — the next label added there cannot reintroduce this by
  forgetting a call. It also truncated to 63 characters *after* trimming separators, so a long
  realm whose 63rd character was a `-` produced the same rejection in a rarer disguise; the order
  is now truncate, then trim. A value with nothing legal left in it is dropped rather than
  replaced with a placeholder. The test that should have caught this asserted the created-at
  label equalled the exact string the cluster refuses; it now checks every value against the
  API server's own regex, quoted from its rejection message.
- **A self-signed Admin API certificate reported itself as an unreachable server.** An internal
  Keycloak behind a self-signed or private-CA certificate — CRC, OpenShift Local, anything with
  its own ingress CA — is the deployment this tool is most often pointed at, and the failure said
  only that the URL could not be reached. It is now named for what it is, with which way the
  certificate failed (unknown authority, wrong host name, expired) and the setting that accepts
  one. It is also terminal rather than retryable: a certificate this machine does not trust will
  not become trusted on the fourth attempt, and the retry budget was being spent proving that.
- **Accept a self-signed certificate** — a per-environment switch on the Admin API block. The
  engine has carried `adminInsecureTls` and wired it to the transport all along; nothing in the
  UI could set it, so it could only be turned on by hand-editing `config.yaml`. Off by default,
  never inferred from a failed handshake, and it says what it costs while it is on. It applies to
  that one environment's Admin API and to nothing else — a snapshot's integrity is checked by its
  own digests and its encryption is unaffected.
- **A probe that could not reach the Admin API now says why.** `Reachable` answers a bool, which
  is all a capture needs; the environment editor gets the reason, because "not reachable" over a
  URL the operator can open in a browser diagnoses nothing.
- **An environment the engine refused to save gave no reason and lost the form.** The failing
  path re-entered `renderEnvironments`, which builds a fresh state from the configuration on
  disk — throwing away the message, because it had just been written to the state object being
  replaced, and the operator's draft, because the file is the version without their edits. A
  Kubernetes workload typed as `kc-a` rather than `deployment/kc-a` therefore looked like a Save
  button that did nothing and then blanked the form, with the sentence naming the value and the
  fix already computed and discarded. A failed save now redraws the state it has, as the Storage
  editor always did; only a save that succeeded re-reads the file.
- **A rejected edit is reported as the problems, not as the file.** `Fail` renders a validation
  error as "<path> has 2 problems:" followed by indented lines, which is right for the launch
  banner about a hand-edited `config.yaml` and wrong over a form. Saving an environment or a
  storage now returns one problem per line, and a notice renders those as separate lines — so an
  entry wrong in two places is fixed in one pass rather than one save at a time.
- **A correct Docker or Kubernetes environment was labelled "Not usable yet".** Readiness demanded
  a `dockerEndpoint` or a kubeconfig `context`, but blank is how a working configuration says
  "DOCKER_HOST or the local socket" and "whatever kubectl is pointed at" — the adapters fall
  through to `client.FromEnv` and to client-go's default loading rules, and the Docker error path
  already calls it "the default endpoint". The Docker banner was worse than merely wrong: it
  offered `runtime` as the way to become ready, and nothing in the engine reads that field. The
  rule is now what it should always have been — a field belongs in readiness only if `Validate`
  lets it be empty *and* the adapter has no default for it, which is true of an SSH key and an S3
  access key and of nothing on those two kinds.
- **Applying a restore opened a dialog announcing that the restore had started.** It then
  navigated to Activity, where the progress is the announcement, so the dialog put a dismissal
  between the operator and the thing they had asked to watch. It is gone.
- **The inspector's key prompt had a "Back to snapshots" button that went nowhere.** Declining
  dismissed the modal and left the screen on its spinner. It goes back.

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
