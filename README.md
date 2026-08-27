# PortCloak

A desktop tool for moving Keycloak realms between environments with full fidelity — users and
their password hashes, OTP and passkey enrolments, unmasked client secrets, RSA/EC/HMAC/AES key
providers including private material, LDAP and IdP federations, authentication flows and realm
settings.

Named after the product it serves. Go + [Wails v3](https://wails.io), single binary, no server
component, no account.

> **Status: design complete, implementation not started.** This repository currently holds the
> specification, the use-case model, the rollout plan and the screen designs. There is no
> working binary yet. See [`spec/rollout/`](./spec/rollout/README.md) for the nine phases that
> build it.

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

## Licence

Not yet chosen.
