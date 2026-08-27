<img src="assets/logo/mark.svg" width="72" alt="">

# PortCloak

A desktop tool for moving Keycloak realms between environments with full fidelity — users and
their password hashes, OTP and passkey enrolments, unmasked client secrets, RSA/EC/HMAC/AES key
providers including private material, LDAP and IdP federations, authentication flows and realm
settings.

Named after the product it serves. Go + [Wails v3](https://wails.io), single binary, no server
component, no account.

> **Status: 0.0.1 implemented.** The whole loop closes — capture a realm, put it somewhere,
> read it back, and restore it — across all four target kinds and all four storage backends.
> The [rollout plan](./spec/rollout/README.md) describes how it was built; the
> [release notes](./spec/rollout/11-release-0.0.1.md#what-001-does-not-do) are honest about
> what 0.0.1 does not do.

## The problem it solves

A realm export that loses a client secret, an OTP enrolment or a signing key produces a
destination that imports cleanly and then fails at the first login. PortCloak treats that as the
failure to design against: `kc.sh export` is the single authoritative capture mechanism, every
carried category is enumerated in a per-snapshot manifest, and anything that could not be carried
is reported rather than quietly dropped.

Two constraints shape most of the design:

- **The instance serving real logins is never disturbed.** On Docker and Kubernetes the export
  runs inside an *ephemeral clone* — a parked copy of the workload, started hung, exec'd into,
  then destroyed. On local and SSH targets it binds automatically-allocated free ports so it
  cannot collide with a running server.
- **Bad connections must not cost the whole job.** Every remote operation retries with backoff
  and jitter, transfers checkpoint to disk, and a job resumes across an application restart —
  converging on one complete object, never a duplicate.

## Where to start

| If you want | Read |
|---|---|
| The problem, goals and full requirement set | [`spec/01-vision-and-requirements.md`](./spec/01-vision-and-requirements.md) |
| The module map and core interfaces | [`spec/02-architecture.md`](./spec/02-architecture.md) |
| Exactly what a snapshot carries, secret by secret | [`spec/07-realm-carryover-manifest.md`](./spec/07-realm-carryover-manifest.md) |
| What the tool actually does, as behaviour | [`spec/usecases/`](./spec/usecases/README.md) — 60 use cases |
| How it gets built, tested and verified | [`spec/rollout/`](./spec/rollout/README.md) — 9 phases |
| What it looks like | [`spec/lunacy/`](./spec/lunacy/README.md) — 20 screens |
| Why the scope boundaries are where they are | [`spec/12-decisions.md`](./spec/12-decisions.md) |
| The mark, and the rules around it | [`assets/logo/`](./assets/logo/README.md) |
| How the code is laid out | [`spec/rollout/00-engineering-foundations.md`](./spec/rollout/00-engineering-foundations.md) |

## What it deliberately does not do

Sessions are out of scope — users re-authenticate after a move, and token continuity is delivered
instead by carrying the realm's signing keys, so tokens issued before the move still verify after
it. Themes and provider JARs are detected and reported, never migrated. Restore is whole-realm.
One snapshot is one realm. It is not a replication tool, a version upgrader, a database backup
tool, or a secrets manager.

The full list, with reasoning, is in
[`spec/rollout/11-release-0.0.1.md`](./spec/rollout/11-release-0.0.1.md#what-001-does-not-do).

## One thing to know before using it

Snapshot encryption is **opt-in**. That is a deliberate decision, not an oversight — but it means
an unencrypted bundle holds unmasked client secrets and private signing keys in the clear. It has
to, because Keycloak accepts exactly those values on import, and that is what makes the migration
work at all. PortCloak labels such a bundle unmistakably and never expires it. Where the file ends
up afterwards is yours to decide.

## Building it

The frontend is embedded in the binary, so it is built first:

```bash
npm --prefix frontend ci
npm --prefix frontend run build
go build -ldflags "-X main.version=0.0.1" -o portcloak ./cmd/portcloak
```

The engine is testable without any of that — no network, no Docker, no Keycloak,
no Node toolchain:

```bash
go test ./internal/... -race
```

If a test in `internal/engine` needs a real target, a fake is missing rather than
the test being justified. Tests that genuinely need a service container are
behind `-tags=integration`, so a missing MinIO reads as "not run" and never as a
silent pass.

## Licence

Not yet chosen.
