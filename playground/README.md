<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# Playground

Somewhere to point PortCloak that is not somebody's Keycloak.

Everything here exists to make the failures worth catching *reachable*: a realm
whose users come from a directory, a restore into a server that has never seen
them, a storage backend that speaks a real protocol, and a clone that has to be
scheduled by a real cluster. None of it is production-shaped and none of it holds
a credential worth protecting — the passwords are in the files on purpose,
because inventing a secret store for a laptop would only suggest otherwise.

| Directory | What it is |
|---|---|
| [`storage/`](./storage) | MinIO, Azurite and an SSH server — the three storage backends that are not a folder |
| [`target/docker/`](./target/docker) | Three Keycloaks, one Postgres, two directories |
| [`target/openshift/`](./target/openshift) | The same shape on CRC, where clones are scheduled by a real cluster |
| [`seed/`](./seed) | The generator that fills them with a realm worth capturing |

## Storage

```bash
docker compose -f playground/storage/compose.yml up -d
```

| Backend | Where | Credentials |
|---|---|---|
| S3 | `http://127.0.0.1:9000`, bucket `portcloak` | `portcloak` / `portcloak-test-secret` |
| Azure Blob | `http://127.0.0.1:10000/devstoreaccount1` | Azurite's well-known development account |
| SFTP | `127.0.0.1:2223` | `portcloak` / `portcloak-test-secret` |
| Disk | any folder | — |

The images and credentials are the ones `.github/workflows/ci.yml` uses. That is
deliberate: a playground that authenticates differently from CI is a playground
that proves something CI does not. The SFTP port is the one exception — CI
publishes 2222, and this publishes 2223, because CRC holds `127.0.0.1:2222` for
the OpenShift VM's own SSH and would answer in this container's place. Drop a
public key into `storage/keys/` to exercise key authentication beside passwords.

MinIO's console is at <http://127.0.0.1:9001> with the same credentials, for
looking at what a capture actually wrote.

### Filling the storage form

What follows is every field, per backend, against the stack above. A field not
listed is left empty. None of the destinations need to exist first: the probe
creates the folder, the bucket or the container on the first Test.

**Disk**

| Field | Value |
|---|---|
| Folder | any path, e.g. `~/portcloak-snapshots` |

**S3**, against MinIO:

| Field | Value |
|---|---|
| Endpoint | `http://127.0.0.1:9000` |
| Region | `us-east-1` |
| Bucket | `portcloak` |
| Prefix | optional, e.g. `snapshots` |
| Path style | **on** |
| Credential | `portcloak:portcloak-test-secret` |

Path style is not optional here. MinIO addresses a bucket as a path segment —
`127.0.0.1:9000/portcloak` — where AWS puts it in the hostname, and an IP
address has no room for a bucket subdomain. The credential is one field holding
both halves, `key:secret`.

**Azure Blob**, against Azurite:

| Field | Value |
|---|---|
| Container | `portcloak` |
| Prefix | optional, e.g. `snapshots` |
| Credential | the connection string below |

```
DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;
```

Leave **Account** and **Endpoint** empty. The connection string carries its own
`BlobEndpoint`, which is what points the client at Azurite rather than at
`*.blob.core.windows.net`, and filling Account instead selects the shared-key
path that builds the URL out of those two fields.

If you do want that path — an account key rather than a connection string —
both fields have to be right together: Account `devstoreaccount1`, and Endpoint
`http://127.0.0.1:10000/devstoreaccount1` **including the account segment**.
Azurite lays its URLs out as `host:port/<account>/<container>`, so an endpoint
that stops at the port makes the container name be read as an account name. The
result is `ResourceNotFound (HTTP 404)` on a container that is present and a
credential that is correct — and it is not a missing container, so PortCloak
will not create one in response to it.

**SFTP**

| Field | Value |
|---|---|
| Host | `127.0.0.1` |
| Port | `2223` |
| User | `portcloak` |
| Authentication | password |
| Folder | `/config/snapshots` |
| Credential | `portcloak-test-secret` |

`/config` is the home directory of the container's `portcloak` user, and the
only path backed by a named volume — `/tmp` is writable too, and empties on
restart. Remote folders belong under `/config` for that reason: what lands
there survives a restart, and goes away with `down -v`. Anywhere above it is
root-owned and `SUDO_ACCESS` is off, so a folder outside it fails to create.

## Targets

### Docker

```bash
docker compose -f playground/target/docker/compose.yml up -d
playground/seed/seed.sh docker
```

| | URL | Realm | Directory |
|---|---|---|---|
| kc-a | http://127.0.0.1:8080 | `corp-a` | ldap-a, `dc=corp-a,dc=example,dc=com` |
| kc-b | http://127.0.0.1:8081 | `corp-b` | ldap-b, `dc=corp-b,dc=example,dc=com` |
| kc-merged | http://127.0.0.1:8082 | *(empty)* | none |

All three: `admin` / `admin`. Three servers rather than one, because the
interesting operations need more than one — capture from kc-a, restore into
kc-merged, and compare against kc-b, which was seeded the same way and never
touched. One instance can prove an export runs; it cannot prove a realm arrived
somewhere else intact.

kc-merged has no directory on purpose. It is the destination, and a restore into
a Keycloak that has never seen these users is the case worth rehearsing.

### OpenShift, on CRC

```bash
playground/target/openshift/crc.sh setup     # 8 CPU, 32 GiB, 100 GiB disk
eval $(crc oc-env) && oc login -u kubeadmin ...
playground/target/openshift/apply.sh
playground/seed/seed.sh openshift
```

Same three servers, same two directories, one namespace: `kc`. `apply.sh` also
creates the Role PortCloak's Kubernetes adapter needs — get, list, create and
delete on pods, create on `pods/exec` — which is the whole list, and worth
reading beside what the tool actually does.

The VM is sized in `crc.sh` and it is not a guess. Three Keycloaks each want a
JVM heap, and during a capture a fourth appears: the ephemeral clone, with the
same image and the same database connection. Eight cores and 32 GiB is what
leaves headroom for the clone rather than making the clone the thing that gets
evicted. Cluster monitoring is turned off, which is roughly 4 GiB back.

## Seeding

```bash
playground/seed/seed.sh docker --ldap-users 20000 --users 500 --clients 20
```

The generator is Go, in its own module with no dependencies — the Keycloak admin
API is HTTP and JSON, LDAP entries are text, and both are in the standard
library. Being a nested module, it is invisible to the application's
`go build ./...`, so nothing here can break the build or grow its dependencies.

It writes both halves of a federated realm from one seed, so they agree:

```bash
cd playground/seed
go run . realm -realm corp-a -users 500 -out corp-a-realm.json
go run . ldif  -realm corp-a -ldap-users 20000 -out corp-a.ldif
go run . all   -realm corp-a -apply http://127.0.0.1:8080
```

### What it generates, and why

The realms PortCloak has trouble with are not big; they are **various**. A
hundred thousand identical users exercise one code path a hundred thousand
times. So the generator's first axis is variety and the counts are only the
second:

- **Credentials** — roughly a third of users carry OTP, a fifth a passkey, some
  both. The OTP secret is real base32 and a phone will accept it. The passkey is
  structurally valid and **will not authenticate**: a WebAuthn credential is
  bound to an authenticator that exists, and no generator can produce a private
  key held in somebody's laptop. It imports, exports and is carried, which is
  what a fidelity fixture needs it to do.
- **Clients** — four shapes that behave differently on restore: confidential
  with a secret, public SPA with PKCE and no secret, bearer-only, and a service
  account. Each with client roles and protocol mappers.
- **Groups** — a forest, not a list. Nesting is where group-path handling goes
  wrong, and eight flat groups would never show it.
- **Roles** — some plain, every fourth composite over roles that already exist,
  because a composite is a role that points at other roles and the pointing is
  what a move can lose.
- **Awkward data** — one user in forty carries something somebody assumed would
  never appear: a quote in a name, a plus in an address, a backslash, a space, a
  string long enough to hit a column limit, characters outside ASCII. They are a
  fixed fraction rather than an option, because the realm nobody opts into is the
  realm nobody tests. This is also why the LDIF writer escapes DNs per RFC 4514
  and base64-encodes values that need it — unescaped, one of those usernames
  produces a file `ldapadd` rejects at entry 40,000.
- **An identity provider with a client secret**, and a **federation provider**
  with connection and read timeouts set — the two settings a default install
  omits, and whose absence is what lets a stalled directory block until the
  server's transaction reaper gives up.

Everything is deterministic from `-seed`. A fidelity test that cannot be re-run
against the same realm is a test whose failure cannot be investigated, and "it
worked when I generated it again" is not an answer.

Generated documents land in `playground/.seed/`, which is gitignored: the realm
JSON is what `kc.sh import` would read, and the LDIF is what the directory
holds.

## Tearing down

```bash
docker compose -f playground/storage/compose.yml down -v
docker compose -f playground/target/docker/compose.yml down -v
playground/target/openshift/apply.sh delete
playground/target/openshift/crc.sh delete
```
