<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# Changelog

All notable changes to PortCloak are recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). Tags prefixed `spec-` mark the design
record; unprefixed `v` tags mark shipped binaries.

## [Unreleased]

### Added
- **A command line.** `pcloak` is a second binary driving the same engine the app drives, reading
  the same `~/.portcloak`. Environments, storage and keys configured in either are visible to
  both, and a snapshot captured from a terminal appears in the app's library. It exists because
  the places a realm migration actually happens are frequently places a desktop application
  cannot go: a CI job seeding a test realm, a maintenance window run from a runbook, a jump box
  with no display. Everything the loop needs is there — capture, the snapshot library and what is
  inside a snapshot, restore with its dry run, job control, probes, and key management — and none
  of it is a second implementation: each command calls the same controller the equivalent screen
  calls, so the gate that refuses an unacknowledged unencrypted capture and the confirmation
  before overwriting a live realm are the same code on both surfaces rather than two copies that
  drift.
- Results go to stdout and everything else to stderr, so a run can be piped while it is still
  narrating its phases. `--json` prints the same structures the app's screens are built from.
  Every prompt has a flag that satisfies it, and with no terminal an unmet prompt is a refusal
  naming that flag rather than a wait that nothing will answer. The exit code distinguishes
  *partial*, *precondition* and *busy*, because those are the three a script branches on: a
  three-realm capture where one realm fails produced two real, restorable snapshots, and calling
  that "failed" would send somebody looking for damage that is not there.
- Nothing secret is ever a command-line argument, where `ps` would show it to every user on the
  machine. `--key` names a key already on this machine — an age key contributes its recipient,
  which is public; a passphrase key contributes its secret from the keychain — and otherwise a
  passphrase comes from a file, from stdin, from `PORTCLOAK_PASSPHRASE`, or from a prompt with no
  echo, typed twice and compared when sealing, because a snapshot sealed with a typo cannot be
  opened by anybody afterwards.
- `pcloak env add` and `pcloak storage add` define what a capture needs, one subcommand per kind —
  `local`, `ssh`, `docker`, `kubernetes`; `disk`, `ssh`, `s3`, `azure` — so `--help` lists only the
  flags that apply rather than twenty-five of which twenty are wrong. Neither contacts anything;
  `env probe` and `storage test` remain the separate, explicit act that finds out whether a
  definition works. `--replace` makes a provisioning script re-runnable without either failing on
  the second pass or silently overwriting on the first. `env remove` and `storage remove` forget a
  definition and its keychain entries — removing a storage does **not** empty it, because a
  definition is cheap to recreate and a snapshot is not.
- `pcloak key` creates and manages age keys. It is there because `--key` is how a secret stays off the command line and it is a
  dead flag on a machine that cannot make a key: a CI runner has no window to generate one in.
  Creating a key is refused under `--no-keychain`, where the secret would go nowhere and
  `config.yaml` would be left naming a handle with nothing behind it — a key that lists as
  present, seals a snapshot, and cannot open it.

  These three — environments, storage and keys — are all the configuration the command line
  writes. Preferences are not, and the test is the one that let the others in: nothing is blocked
  on a preference, because every one of them is a default that a flag already overrides.

### Changed
- **Two PortCloaks can now share one `~/.portcloak`, and the rules for it are explicit.** Both
  the window and the command line take an advisory claim on the folder saying they are there.
  Capturing, restoring, job control and every read run beside each other and beside the app: two
  captures at once are safe, because each writes its own job record and its own staging directory
  and one snapshot holds one realm. Two things need the folder to themselves and take it only for
  as long as they last — the startup sweep, and a change to `config.yaml`, where both writers
  would read the file before either wrote and the second would silently drop the first one's
  change. A refusal names who is holding it, since when, and what still works.
- The startup sweep no longer runs while another PortCloak is using the folder. It rewrites every
  running job to interrupted and deletes the working directories of snapshots it cannot see are
  open, so run beside a live capture it would have marked that capture interrupted and deleted
  the staging directory it was still writing into. The guard is structural: the destructive part
  is unreachable except through the claim.
- A folder named with `--home` is reported as what it is rather than as the default, and cannot
  be moved from Settings — there is nowhere to record a different choice that the flag would not
  override on the next run. `--config` names a file to read and never one to create, so a typo is
  reported instead of quietly starting an empty PortCloak.

### Development
- `internal/app` no longer imports Wails. The window, the menu, the event bridge and the service
  registry moved to a new `internal/desktop`; the composition root and all nine controllers
  stayed. Nothing had to be exported to do it — the seam was already exactly where the unexported
  methods stop. On Linux this means working on the engine, the controllers or the CLI no longer
  needs GTK and WebKit headers installed, and `CGO_ENABLED=0 GOOS=linux go build ./internal/app`
  works for the first time. A test reads the source and fails on a Wails import below
  `internal/desktop`, and a CI job builds the command line for three platforms on a runner with
  no toolkit at all, so the rule is enforced twice and in two different ways.

## [0.0.3] — 2026-08-29

### Added
- Every table inside a snapshot pages, and the page is addressable. The users list, the clients,
  keys, user federations, identity providers, authentication flows and external dependencies, and
  the secret ledger. Numbered pages rather than one step at a time, with the first and the last
  always reachable, and the number of rows on a page is the reader's: 25, 50, 100 or 200. A realm
  that issues a client per integration has thousands of them, and a service-account client is still
  a client; reaching the eleventh page of anything used to mean pressing the same arrow ten times
  with nothing on screen saying how many presses were left. Changing the page size keeps the row
  you were on in view rather than returning you to the top.
- Activity shows what to do when nothing has run. It is the screen an operator returns to, and on a
  new install it said only that nothing had happened yet. It now shows the same two steps the
  snapshot list shows when it has nothing to list: a Keycloak to read from, and somewhere to put
  what is read.
- Every button carries an icon, primary and secondary alike. Capture, restore, save, add, import,
  verify, inspect, test, refresh, back and next each have their own mark, so a row of buttons can
  be told apart at a glance instead of read word by word.

### Changed
- Test works before you save, on both the environment and the storage editors. The button read the
  definition on disk, so a definition had to be saved before it could be tested, and testing it
  then answered a question nobody was asking: whether the thing already committed works. It now
  tests what is on screen, including a password typed but not yet stored. Nothing is written and
  nothing is gated: a definition that fails its test can still be saved, because a target that is
  down this minute is not a definition that is wrong.
- Add an environment and Add a storage open the editor. Both used to land on the list with the
  editor still a click away, including the two buttons on the first-run screen whose whole purpose
  is to start one.
- Times are written with AM or PM rather than on a 24-hour clock, in this desktop's own zone and
  with the zone named.
- A phase that was skipped is drawn in yellow with a dash rather than in green with a tick. A
  restore whose destination could not be read reports that validation was not performed, and used
  to tick the validation step in the same green as one that had passed, contradicting the sentence
  printed beside it.
- Snapshots is called Snapshots everywhere. The screen was labelled one thing and named `library`
  in the route, the navigation key and the icon set.

### Development
- The pages are arranged by how they are reached rather than as eleven sibling folders: capture,
  restore and inspection now sit under the snapshot list that opens all three, and browsing under
  the storage editor that opens it. Imports resolve through `@/`, so a screen four levels down
  names what it imports instead of counting steps back to it.
- The playground works again, end to end. Its LDAP image had been unpublished from under it when
  Bitnami moved its catalogue; the seeding script died on macOS's bash before it loaded anything,
  and its check for a stopped stack never fired; the realm it generated was one Keycloak refused to
  import, in three separate ways; and the federation it configured resolved nobody, because a
  provider imported with a realm does not get the mappers the admin API would have given it. The
  OpenShift half also grants its directories the SCC they need, and publishes its SFTP on 2223,
  where CRC is not already listening.

## [0.0.2] — 2026-08-29

The release that came out of one failure. A capture of a realm federated to a slow LDAP directory
died after exactly five minutes and reported `kc.sh export exited with code 1` — which reads like a
disk problem and is not one. Everything under *Fixed* is what that sentence turned out to be hiding,
and most of what is under *Added* is what it needed in order not to happen again.

One thing is taken away rather than added: a snapshot no longer exports. The user list and the
secret ledger could each be written out as a redacted file, and every view they wrote was already
presence rather than values — so the file said nothing the screen had not, and left a copy of an
audited reading outside the session that made it.

Nothing changes about what a snapshot carries or how it is sealed, so a 0.0.1 bundle opens and
restores under 0.0.2 unchanged. See
[`spec/rollout/13-release-0.0.2.md`](./spec/rollout/13-release-0.0.2.md) for what to know before
upgrading.

### Added
- The Activity screen re-reads a job's output from the engine on every refresh, rather than
  showing only what it happened to hear on the event stream. It could not have heard the rest: the
  output arrives over a Docker or Kubernetes exec stream, which neither platform stores, from a
  clone that is usually destroyed by the time anyone reloads — so the engine records it as it goes
  past and keeps the last 10,000 lines per job. The screen still folds the live stream so a line
  appears the instant it is said, and reconciles against the engine by cursor, asking only for what
  it has not already been given.
- A capture option, and the same option on restore, that lets kc.sh's transactions run without a
  time limit — for a realm too large or too slow to move inside the server's. Transactions
  themselves cannot be turned off: Keycloak's export is written as a sequence of them, and
  `--transaction-xa-enabled` chooses XA against local datasources rather than switching them off.
  So the option lifts the limit that cancels one, through
  `QUARKUS_TRANSACTION_MANAGER_DEFAULT_TRANSACTION_TIMEOUT=0` in the invocation's environment.
  Off unless asked for, and shown in the logged command line: that limit is also what
  bounds a run that has stopped making progress, and without it a stalled one holds a connection to
  the database open until the clone is destroyed. It matters more on restore than on capture — an
  export cancelled part-way leaves nothing behind, an import leaves a half-applied realm.
- Users per file is set per capture, in the wizard, anywhere from 10 to 1,000. kc.sh exports one
  page of users per transaction, so the number bounds both the file size and the transaction: a
  realm whose users come from LDAP is re-read through the provider one user at a time inside that
  transaction, and a page of a thousand can outrun the server's transaction limit and kill the
  export five minutes in. The range is enforced in the engine as well as the wizard.

### Removed
- A snapshot exports nothing. The user list and the secret ledger could each be written out as
  CSV or JSON, redacted by the same rules as the screen and audited as they went — which made the
  export safe, and never made it useful. Every inspection view is already presence rather than
  values, so the file held no fact the screen had not shown; what it added was a second copy of an
  audited reading, living outside the session that produced it and outside the shredding that
  closes one, and from then on somebody's to keep track of. Where a reading has to be evidence,
  the audit log is the record — it is the artefact the export was defending, and it is already
  kept. `FR-V10` and `UC-I12` are marked withdrawn in the spec rather than deleted from it: they
  were built and shipped in 0.0.1, and the design record should say so.

### Fixed
- The row menu on a table was cut off at the edge of the card it opened in. Every table in the app
  sits in a box that scrolls sideways, and CSS computes `overflow-y` to `auto` alongside an explicit
  `overflow-x: auto` — so the box clips vertically whether or not anything asked it to, and a menu
  positioned inside it is cut off no matter what it is stacked above. Stacking order was never the
  problem, which is why raising it would not have helped. The list is now drawn into the document
  body at the trigger's viewport coordinates, where there is nothing left to clip it, and re-placed
  while anything scrolls beneath it. It still flips upwards near the bottom of the window, still
  runs the item that was chosen, and still closes on a click elsewhere — the three things moving it
  out of the row could have cost, and the three the new tests hold down.
- An Azure endpoint that does not name the storage account said `ResourceNotFound (HTTP 404)` and
  left the operator to work out the rest. Azure carries the account in the host name and an
  emulator carries it as the first path segment, so an endpoint that stops at the port leaves the
  container name sitting where the account name should be — and it is read as one. The container is
  never looked for, which is why the 404 does not say `ContainerNotFound`, and why nothing is
  created in response to it: PortCloak will make a container that is missing, but not one it has no
  account to make it in. The message now says an account was not found at the endpoint, that the
  container was therefore never looked up, and where the account belongs. `ResourceNotFound` also
  joins the codes that are never retried, being a configuration error rather than a passing one.
- A storage backend listed the folder above the one it was configured with. The scan fell back to
  the parent whenever the configured root could not be stat'd, which was meant for a prefix naming
  a key stem rather than a directory — every object store treats a prefix that way — but it fired
  just as readily when the root itself was missing. What came back was the contents of somewhere
  else, keyed against a root they had never been under, so the inspector reported them as objects
  PortCloak had not written: true, and true of a folder nobody had pointed it at. The fallback now
  applies only where it was meant to, to an explicit prefix, and a root that is not there lists
  nothing. Both the remote folder and the disk backend had it.
- A destination that does not exist yet is created rather than reported as unreachable, on all four
  backends. Naming a folder, a bucket or a container that nobody has made by hand is the ordinary
  case, not a mistake, and `unreachable` is what PortCloak says when it cannot talk to the endpoint
  at all — so a server answering perfectly well presented as a broken connection. The first probe
  creates it: `MkdirAll` on a folder or an SFTP host, `CreateBucket` on S3, `CreateContainer` on
  Azure Blob. Only what cannot be created is a failure, and it says which of the two it was. The
  absence is the one condition treated this way; a rejected credential or a denied listing is still
  the operator's to see, and must not arrive dressed as something that had simply not been made yet.
  On S3 a name already owned by another account stays an error for the same reason.
- An ephemeral clone belonging to a running job was reported as orphaned, with a button offering to
  remove it. A clone a capture is exporting through and one a crashed session abandoned are
  indistinguishable by inspection — same image, same labels, same name — and the only thing that
  tells them apart is whether anything is still driving it. The sweep now excludes clones whose job
  this process is running, and removal re-checks at the moment it is asked for, because the list an
  operator is looking at was accurate when it loaded.
- The job ledger's columns are laid out rather than left to the content. An outcome can be a whole
  sentence — the restore path writes one into the field that usually holds `failed` — and inside a
  pill, which does not wrap, it grew until it left the card and squeezed the error column into a
  ribbon one word wide.
- The snapshots a restore applies are recorded on the job. The Activity screen renders from the job
  list alone and never opens a bundle, so a restore card could name the realm and the destination
  but not which of two captures a fortnight apart it was applying.
- The test buttons in the environment and storage editors no longer sit under a heading repeating
  their own label, with the result panel flush against them.
- The Kubernetes adapter ran commands without the environment they carried. The exec subresource
  has no environment field, and unlike the other three adapters this one did nothing about it, so
  a `Command` with `Env` set ran without it. It is applied with `env(1)` in front of the command.
- Keycloak's own warnings and errors never reached the job ledger or the failure message. The log
  levels were matched at the start of the line, which catches the launcher's bare `ERROR:` and
  nothing a running server logs, because those lines all open with a timestamp. Every failure the
  server explained in prose was therefore reported as its exit code — an export the transaction
  reaper had rolled back arrived as `kc.sh export exited with code 1`, which reads like a disk
  problem and is not one. That failure is now named, with the setting that addresses it.

### Changed
- Every date in the interface is written out in full, with the month as a word and the zone named —
  `28 August 2026 at 15:52 GMT+3`. `03/04` is two different days depending on who is reading, and a
  time with no zone cannot be lined up against a Keycloak server log without someone guessing the
  offset. The ordering of the parts is the reader's locale's; the clock is 24-hour everywhere.
- The Snapshots table is where a restore starts. Restore left the navigation rail — it was the one
  item that could not act on its own, since every restore is *of a particular snapshot* — and is now
  the filled button on each row, and in the inspector's header. The realm opens the snapshot; the
  rest of the row's actions fold into a menu, with Delete in red and Close where there is a session
  to close. Activity leads the rail, because it is the screen an operator returns to.
- The environment and storage a snapshot names are links while they still exist. A snapshot is a
  record of something that happened, not a foreign key: what it names can be renamed or removed
  afterwards and the snapshot stays as true as it was, so the name is always shown and only the link
  comes and goes. Where the engine says nothing about them, nothing is claimed in either direction.
- Encryption is one component in one place, said the same way on all five screens that ask: green
  for sealed, red for in the clear, and no mode appended. Two of the five had drifted into saying
  `Encrypted · recipients` while three did not.
- The Activity card says what a job is in the terms of the thing it moves: the kind as a glyph and a
  word, then the facts, each labelled by its own icon rather than strung together with an arrow —
  an arrow only works if the reader knows which side is the source, and that flips with the kind.
  The phases are a numbered stepper down the left of the card rather than a wrapping row of ticks,
  and a restore names which snapshot it is applying and when the run began.
- Dropdowns are drawn by the app rather than by the operating system. A native `<select>` can be
  styled down to its border and no further — the list it opens is an OS menu in the platform's
  font at the platform's size, ignoring every design token — so it is a listbox PortCloak paints
  and owns: keyboard operation including type-ahead, a combobox in the accessibility tree,
  dismissal that leaves focus somewhere sensible, and flipping upwards rather than opening off the
  bottom of the window. Form controls are taller with it, and the filters above a table wider.
- A snapshot carries its user files under padded numbers — `acme-users-000.json` rather than
  `acme-users-0.json` — all the same width, widened past three digits where the export needs it.
  kc.sh numbers them 0, 1, … 10, which anything ordering names as text reads as 0, 1, 10, 2.
  Keycloak's own `import` is indifferent either way: measured on 24.0, 26.3 and 26.5.0 it matches
  `-users-[0-9]+\.json` and iterates in filesystem order, so padded names are found exactly as
  unpadded ones were. It is everything that lists names alphabetically around it that this is for.
- The Activity screen re-reads the job list once a second rather than every two, so the elapsed
  time on a running job counts up evenly instead of advancing in jumps of one second and two.

### Development
- A playground: `playground/storage` runs the three storage backends that are not a folder, and
  `playground/target` runs three Keycloaks with Postgres and two LDAP directories, in Docker and on
  CRC. Capture from one, restore into another, compare against the third — one instance can prove
  an export runs and cannot prove a realm arrived somewhere else intact. `playground/seed` generates
  the realms: four client shapes, nested groups, composite roles, OTP and passkey enrolments, and
  one user in forty carrying a quote, a plus, a backslash or a character outside ASCII — because the
  realms this tool has trouble with are not big, they are various.
- One formatter per language, both enforced in CI. `.editorconfig` settles indentation and endings
  for every language, `frontend/.prettierrc` pins the TypeScript style at an exact prettier version,
  and Go gets no configuration because gofmt takes none — CI runs `gofmt -l`, which it never did.
  Nothing in the repository had decided, so an IDE and a formatter had been quietly reformatting the
  same files back and forth.
- The published release notes say what changed. `build/ci/changelog-section.sh` lifts a version's
  section out of this file, and the release workflow fails when there is no section for the version
  being cut — a release describing how it was verified and nothing about what is in it is worse than
  one that goes out tomorrow.
- The Docker client is `github.com/moby/moby/client`, with its API types in
  `github.com/moby/moby/api`, rather than `github.com/docker/docker`. Four advisories stood against
  the old module and none could be answered by a version bump: v28.5.2 is the last release ever
  published on that path, three of the four name no fixed version at all, and the fourth points at
  29.3.1 — which exists only under the module Docker split its client into. All four describe the
  daemon rather than the SDK, and PortCloak imports only the client, so none of the vulnerable code
  was ever in this binary; what was in it was an import that had stopped receiving fixes. The call
  surface is reshaped rather than renamed — every method takes an options struct and returns a
  result struct, the exec calls lose their `Container` prefix, `filters` gives way to
  `client.Filters`, `stdcopy` moves under `api/pkg` — and each site behaves as it did.
  `govulncheck` now reports nothing against this code, down from two, and the dependency tree loses
  eight indirect modules.
- The per-file coverage floor on the frontend's decision-making modules is 75% rather than 100%.
  The list of modules is unchanged and still sits far above the global floor, so a change that
  stops covering one of them fails there rather than being averaged away by the untested pages
  around it. What it no longer does is demand that every incidental guard added to one of those
  files arrive with a test before the suite will go green.
- The integration suite records host keys in a temporary file rather than the operator's own
  `~/.ssh/known_hosts`. It accepts a first connection deliberately — the hosts it connects to are
  throwaway containers — but it was writing that decision into the file every ssh client on the
  machine reads, so a suite run left a trust the operator had not granted and would not have known
  to look for.
- The storage playground publishes its sshd on 2223 rather than 2222. CRC binds `127.0.0.1:2222`
  for the OpenShift VM's own SSH, and a bind to a specific address beats Docker's wildcard, so on
  any machine running the Kubernetes target playground alongside this one every IPv4 connection to
  2222 reached CRC instead — which answers, rejects the playground's credentials, and looks for all
  the world like a broken SFTP backend. CI keeps 2222, where nothing competes for it.

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
- The frontend has a suite of its own — Vitest over jsdom, with the engine mocked at
  `src/api.ts`, the frontend's only door to Go. It covers what
  [`spec/rollout/01` §1.9](./spec/rollout/01-test-strategy.md) says is worth covering and no
  more: wizard step validity for both wizards, the job progress reducer behind the Activity
  screen, the user table's paging and facets, and the shared pieces every screen leans on — the
  loading hook, the error boundary, the progress subscription and the formatters.
- Both suites report coverage, so a green run says how much it exercised rather than only that
  nothing failed. The Go figure is measured with `-coverpkg=./internal/...`, because
  `go test -cover` grades each package against its own tests and reports 0.0% for one exercised
  entirely through a sibling. Each has a floor set just under what it currently reaches: raising
  one is a commit, and lowering one is a change a reviewer is meant to notice.

### Application
- Wails v3 desktop shell with the eight screens from the design file. No sign-in, because there
  is no account: configuration is plain YAML in `~/.portcloak/` and every credential lives in
  the OS keychain, referenced by handle.
- Redacting `slog` handler from the first commit, with its own CI stage so a failure there is
  unmissable.
- Vite 8, TypeScript 7 and Wails v3.0.0-beta.15 on both sides of the bridge. The Wails Go module
  and its JavaScript runtime are bumped together and pinned to the same beta, because they are
  two halves of one binding protocol and a version skew between them fails at the call rather
  than at the build. `erasableSyntaxOnly` is on: Vite and Vitest strip types rather than compile
  them, so TypeScript syntax that has to emit code would type-check and then be missing at
  runtime. The Go module graph moved with it — current AWS and Azure SDKs among them — and the
  suite, `go vet` and `golangci-lint` are green on the result.

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
- **A release pipeline that signs what it publishes.** Pushing a `v*` tag builds all five
  artifacts, signs and notarises the macOS bundle with a Developer ID under the hardened
  runtime, has the Windows executables Authenticode-signed by SignPath Foundation, and opens a
  draft release for the gate review to meet actual bytes. CI no longer builds: it is tests, and
  packaging costs three runners to say nothing the suite has not.
- **Every artifact is tied to this repository, not just to a certificate.** `SHA256SUMS` is
  signed with Sigstore cosign in keyless mode — the identity is the release workflow's OIDC
  token, so there is no private key to store, rotate or leak — and each artifact carries a
  build-provenance attestation. A Developer ID signature says Apple knows who paid for the
  certificate; these say the bytes came from this source tree at this commit, which is the
  question a person downloading a tool that handles realm secrets is actually asking. It is also
  why Linux needs no scheme of its own.
- `package.sh` gained `--stage-only`, `--archive-only` and `--linux-docker`. The first two are a
  seam: a signature goes on the bundle, not on the zip around it, so the release signs between
  the two passes and the credentials stay out of a script a pull request can run. The third
  forces the container's controlled sysroot on a Linux host, where the script otherwise prefers
  a fast native build against whatever GTK the machine happens to carry.

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
- **A resume test read the previous attempt's result.** `ResumeUpload` starts a goroutine and
  returns, and the harness counted `interrupted` as settled — which it has to, because that is
  how a dropped upload legitimately ends — so the assertions ran against the job as it was
  before the resume. It passed for the same reason it was wrong: the window was narrow. Coverage
  instrumentation widened it enough to fail every time.
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
