<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

<img src="assets/logo/mark.svg" width="72" alt="">

# PortCloak

**Move a Keycloak realm from one environment to another, and have it still work when it lands.**

Passwords, 2FA enrolments, passkeys, client secrets, LDAP bind credentials, identity-provider
secrets and the keys that sign your tokens all travel with it. PortCloak is a desktop app for
macOS, Windows and Linux, and a command line for everywhere a desktop app cannot go. No server to
run, no account to create, no sign-in.

> **Status: early (0.0.4).** The full loop works end to end from either the app or the command
> line: capture a realm, put it somewhere, browse it, restore it. It has not yet been through a
> long life in production, and no release carries an Apple or Windows platform signature yet
> ([see below](#installing)).

## The problem

A stock realm export is usually good enough for configuration and quietly wrong about everything
else. It masks client secrets. It leaves signing keys behind. Depending on how you run it, users
and their credentials come out incomplete or not at all.

The realm imports cleanly at the destination, so the migration looks like it worked. Then:

- nobody can log in, because password hashes did not travel;
- everyone with 2FA is locked out, because OTP seeds and passkeys did not travel;
- every integration breaks, because the client secrets are `**********`;
- every token issued before the move is rejected, because the realm signs with new keys.

You find this out in production, at the first login.

PortCloak exists to make that failure impossible to have by accident. It captures through
Keycloak's own `kc.sh export`, which is the authoritative mechanism, fills the gaps that leaves
via the Admin API, and then **writes down exactly what it carried**. If something could not be
carried, it is reported, not dropped in silence.

## What it does

Four things, in a loop:

**Capture.** Pick a Keycloak, pick a realm, get a snapshot. PortCloak probes the target first and
tells you what it found (Keycloak version, where `kc.sh` lives, free space, whether it can work
there at all) before anything runs.

**Store.** Snapshots go to a local folder, a remote SSH volume, an S3-compatible bucket, or Azure
Blob. Optionally sealed with a key you control.

**Inspect.** A snapshot is a browsable artifact, not an opaque blob. Search the users inside it,
list its clients, see which keys carried private material, read its secret ledger, all without
restoring anything and without touching a live server.

**Restore.** Pick a snapshot, pick a destination, see a dry-run diff of what will change, choose
how to handle collisions, then apply and watch the import log. Afterwards PortCloak re-reads the
destination and reports any drift from what the snapshot said.

## What travels with a realm

| | Carried |
|---|---|
| **Users** | Accounts, attributes, role mappings, group memberships, required actions, federated identity links, service accounts |
| **Credentials** | Password hashes with their algorithm, iterations and salt, so existing passwords keep working |
| **2FA** | OTP/TOTP seeds, WebAuthn and passkey credentials, recovery codes |
| **Clients** | Definitions, redirect URIs, protocol mappers, scopes, service-account roles, full authorization-services model, and **unmasked client secrets**, verified through the Admin API to be real values rather than `**********` |
| **Signing keys** | RSA, EC, HMAC and AES key providers *including private material*, with their KIDs, priorities and algorithms, so **tokens minted before the move still verify after it** |
| **User federation** | LDAP and Kerberos providers, all their mappers, sync settings, and the LDAP bind credential |
| **Identity providers** | OIDC, SAML and social IdPs, their mappers, and their client secrets |
| **Authentication** | Flows, subflows, executions, bindings, authenticator configs (and the secrets inside them), required actions, client policies and profiles |
| **Realm settings** | Token lifespans, session settings, password policy, brute-force settings, OTP and WebAuthn policy, roles, groups, client scopes, localization, SMTP config with its password |

**Sessions do not travel, on purpose.** They live in Infinispan caches, depend on cluster topology,
and do not survive being recreated elsewhere in any dependable way. After a restore, users
re-authenticate. What people usually actually want from session portability is that tokens issued
before the move are still accepted afterwards, and PortCloak delivers that properly instead, by
carrying the realm's signing keys.

**Theme files and provider JARs do not travel either.** They are deployment artifacts, not realm
data. PortCloak detects them, lists them, and shows you that list *before* a restore, because a
realm pointing at a theme or an authenticator that isn't deployed at the destination imports
successfully and then fails at login.

Every snapshot ships with a manifest saying which of these were found and carried, so a completeness
verdict is something you read rather than something you assume. The full item-by-item table is in
[`spec/07-realm-carryover-manifest.md`](./spec/07-realm-carryover-manifest.md).

## Where it can read from

Four kinds of environment, one workflow:

| | What you point it at |
|---|---|
| **Local** | A Keycloak install folder on this machine |
| **SSH** | A host (optionally through a jump host) and the install folder on it |
| **Docker** | A socket, `DOCKER_HOST`, or Docker-over-SSH, and the container or service running Keycloak |
| **Kubernetes / OpenShift** | A kubeconfig context, namespace, and the Deployment or StatefulSet running Keycloak |

**The instance serving real logins is never disturbed.** On Docker and Kubernetes the export runs
inside an *ephemeral clone*: a parked copy of your workload, started idle, exec'd into, then
destroyed. You watch it get created and torn down. On Local and SSH targets, PortCloak binds
automatically-allocated free ports so it cannot collide with the running server.

No `kubectl`, `docker`, `ssh` or `aws` CLI is required. PortCloak speaks to all of them directly,
and falls back to a CLI only where a socket or API isn't exposed.

## Where it can write to

| | |
|---|---|
| **Disk** | A folder on this machine |
| **SSH** | A folder on a remote host |
| **S3** | Any S3-compatible endpoint: AWS, MinIO, and friends |
| **Azure Blob** | Azure, or Azurite for local work |

Each is rooted at a folder or prefix, so one bucket or one host can hold several independent
snapshot trees. One is marked default for new captures. Any of them can be marked
*encryption required*, which removes the option to write an unsealed snapshot there.

## Things that matter when the network is bad

Slow and flaky links are treated as the normal case, not the exception:

- Every remote operation **retries with backoff and jitter**, and transfers **checkpoint to disk**.
- An interrupted job shows up as **Interrupted**, with a **Resume** action, and resuming works
  *across an application restart*. It converges on one complete snapshot, never a duplicate and
  never a half-written file that looks whole.
- While a job runs you see the phase, the item, the attempt count and the last error, including
  Keycloak's own output, streamed live and kept afterwards, so a failure names itself instead of
  arriving as an exit code.
- Cancelling actually tears down the ephemeral clone rather than abandoning it.

## Snapshots and secrets

An unencrypted snapshot holds unmasked client secrets and private signing keys **in the clear**. It
has to, because those are exactly the values Keycloak accepts on import, and carrying them is what
makes the migration work at all.

So: **encryption is opt-in, and presented on.** Turning it off takes a deliberate action that spells
out the consequence, an unencrypted bundle is labelled unmistakably everywhere it appears, and
PortCloak never expires it. Where the file ends up afterwards is yours to decide.

Two ways to seal one:

- **A passphrase**, with AES-256-GCM over a strong KDF.
- **Recipients**, as `age`/X25519 public keys, so the people who capture and the people who restore
  need not be the same people.

Keys are managed in the app: generate or import one, give it a name, and its secret half goes to
the **OS keychain** (macOS Keychain, Windows Credential Manager, libsecret), never to a config
file. Captures seal to a key *by name*; restores and inspections open a snapshot with what is
already stored instead of prompting you.

Every connection credential PortCloak needs works the same way. `~/.portcloak/config.yaml` is
readable, diffable and hand-editable, and holds no secrets, only handles into the keychain. That
folder can be moved onto an encrypted volume or an external disk from Settings, without a restart.

Every reveal of a secret is explicit, per-secret, and written to an audit log you can read and
filter but never edit from inside the app. The threat model is in
[`spec/08-security.md`](./spec/08-security.md).

## Installing

Download the build for your platform from the
[Releases page](https://github.com/muhammadsalah/portcloak/releases):

| Platform | Artifact |
|---|---|
| **macOS** (Apple silicon + Intel) | `PortCloak-<version>-macos-universal.zip`, a universal `.app` |
| **Windows** (amd64, arm64) | `PortCloak-<version>-windows-<arch>.zip` |
| **Linux** (amd64, arm64) | `portcloak-<version>-linux-<arch>.tar.gz` |
| **Command line**, any platform | `pcloak-<version>-<os>-<arch>.tar.gz` (`.zip` on Windows) |

The command line is its own download: a CI runner or a headless server has no use for an archive
carrying an embedded webview, and on macOS it is not yet inside the signed `.app`, so expect the
keychain to ask once per entry the first time `pcloak` reads a credential the app stored.

**No release is signed by Apple or Microsoft yet.** macOS Gatekeeper and Windows SmartScreen will
both object, and you will have to allow it through by hand. What *is* in place for every artifact
on every platform: `SHA256SUMS` signed with Sigstore, and a build-provenance attestation binding
each file to this repository at a specific commit. Every release publishes the exact commands to
verify both, and given the above, it is worth running them. Details in
[`CODE_SIGNING.md`](./CODE_SIGNING.md).

On Linux the desktop binary needs GTK 3 and WebKitGTK 4.1 present (`libgtk-3-0` and
`libwebkit2gtk-4.1-0` on Debian/Ubuntu, or your distribution's equivalents). `pcloak` needs
nothing at all — it is a static binary with no toolkit and no C library behind it, which is the
point of it.

### What you need on the other end

- **Keycloak** reachable through one of the four environment kinds above, with an install you can
  run `kc.sh` in. Measured against **24.0, 25.0, 26.3 and 26.5**. PortCloak asks the binary what
  it supports rather than consulting a version table, so nearby versions generally work.
- **An Admin API account** is optional but recommended: it is what verifies that client secrets are
  real values rather than masked ones, and what detects your custom themes and provider JARs.
  Self-signed certificates behind a private CA are supported explicitly.

## Getting started

1. **Environments.** Add the Keycloak you want to capture from. Press **Test**: it runs the exact
   same probe a capture does, and reports what it actually found. Test works before you save, so
   you can check a definition while you're still typing it.
2. **Storage.** Add somewhere for snapshots to live, and mark one default.
3. **Keys** *(recommended)*. Generate one, so encryption is a name you pick rather than a
   passphrase you retype on every machine.
4. **Capture**, from Snapshots. Choose the realm, the options, the destination; watch it run.
   Selecting several realms queues several jobs, because one snapshot is always exactly one realm.
5. **Restore**, from a snapshot's row or from the button in its inspector. Pick the destination,
   read the dry-run diff, choose overwrite / skip / merge, apply.

The app opens straight into the workspace; there is nothing to sign into. It is a single-user local
tool, and the only credentials involved are the ones your own environment and storage definitions
carry.

## From the command line

`pcloak` is the same engine, on the same `~/.portcloak`. Anything configured in the app is visible
to it and the other way round, and snapshots it captures appear in the app's library. A machine
with no display can go from nothing to a sealed snapshot without one:

```bash
pcloak env add docker prod-docker --container keycloak   # point it at a Keycloak
pcloak storage add disk out --folder ./snapshots --default
pcloak key generate ci-2026                       # one key, kept in this machine's keychain
pcloak env probe prod-docker                      # would a capture work here?
pcloak capture -e prod-docker -r corp-a --key ci-2026
pcloak snapshot list                              # no key needed; the library is keyless
pcloak restore 01J8F2 --env staging --dry-run     # see the diff before writing anything
```

It exists because the places a realm migration actually happens are frequently places a desktop
application cannot go: a CI job seeding a test realm, a maintenance window run from a runbook, a
jump box with no display.

Some things worth knowing before you script it:

- **Results go to stdout, everything else to stderr**, so a run can be piped while it narrates.
  `--json` prints the same structures the app's own screens are built from.
- **Every prompt has a flag.** With no terminal, an unmet prompt is a refusal naming that flag
  rather than a wait. Exit codes distinguish *partial*, *precondition* and *busy*, because those
  are the three a script actually branches on.
- **Nothing secret is ever an argument.** `--key` names a key already on the machine; passphrases
  come from a file, stdin, `PORTCLOAK_PASSPHRASE`, or a prompt with no echo.
- **It runs beside the app.** Both hold a claim on the folder saying so; only the startup sweep
  and a change to `config.yaml` need it to themselves.
- **Ctrl-C tears down.** A capture may be holding an ephemeral clone in your cluster, and
  cancelling destroys it rather than abandoning it. If you have to kill the process anyway, it
  tells you what was left behind first.

Environments, storage and keys are all definable from here — `pcloak env add --help` and
`pcloak storage add --help` have one subcommand per kind, so you see only the flags that apply.
Nothing they do contacts anything; `env probe` and `storage test` are what find out whether a
definition works. Preferences are the one thing left to the app, and nothing is blocked on them:
every preference is a default that a flag already overrides.

## What it deliberately is not

Not a replication or HA-sync tool, since snapshots are point-in-time. Not a version upgrader. Not a
database backup tool: it works at the realm level, not on raw dumps. Not a secrets manager. Restore
is whole-realm, with no cherry-picking. `pcloak` is not a daemon and does not run anything in the
background: a capture blocks until it finishes, because the alternative would leave a clone running
in your cluster. The reasoning behind each of these is in
[`spec/12-decisions.md`](./spec/12-decisions.md), and the full list of what a release does not do
is in its release notes.

## Licence

Apache License 2.0. See [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE).

    Copyright 2026 Muhammad Salah <muhammadsalahmasoud@icloud.com>

## Building it, and contributing

[`README.dev.md`](./README.dev.md) covers building from source, running the test suites, how
releases are produced and signed, and the design record in [`spec/`](./spec/README.md): 60 use
cases, the architecture, the nine-phase rollout, and a log of every fault that reached working code
with the test that keeps it from coming back.
