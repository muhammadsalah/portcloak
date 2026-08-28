<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# Release — 0.0.2

**What 0.0.2 is.** A maintenance release with a pass over the interface. Nothing changes about what
PortCloak carries or how a snapshot is sealed, so a 0.0.1 bundle opens and restores under 0.0.2
unchanged. What changes is what happens when a capture goes wrong on a large realm, and what the
screens tell you while it is happening.

It exists because of one failure, found by running 0.0.1 against a realm federated to a slow LDAP
directory: the capture died after exactly five minutes and reported `kc.sh export exited with code
1`. Everything else here came out of understanding that sentence — see
[`spec/notes/02-the-engine.md`](../notes/02-the-engine.md#14-a-timestamp-in-front-of-the-level-hid-every-line-that-said-why),
entries 14 and 15.

The complete list is in [`CHANGELOG.md`](../../CHANGELOG.md#002--2026-08-28). This page is the part
worth reading before upgrading.

---

## The failure this release is about

`kc.sh export` writes one page of users per transaction — `--users-per-file` of them — and on a
realm whose users come from a federation provider it re-reads every user through that provider,
one synchronous directory round trip each, **inside that transaction**. A page of a thousand
against a slow directory outruns the server's transaction limit, Narayana's reaper cancels it, and
the export dies part-way through a snapshot.

None of that reached the operator, because of a second fault stacked on top: PortCloak matched log
levels at the start of the line, which catches the launcher's bare `ERROR:` and nothing a *running*
Keycloak logs, since every one of those lines opens with a timestamp. So every warning and every
error the server produced was discarded, and the failure arrived as an exit code — which reads like
a disk problem and is not one.

Three things address it, in the order you would reach for them:

1. **The failure is named.** Keycloak's own output now reaches the job ledger and the failure
   message, and a rolled-back transaction is reported as what it is, with the setting that
   addresses it.
2. **Users per file is yours to set**, per capture, from 10 to 1,000 — the number that bounds the
   work inside one transaction. The range is enforced in the engine as well as the wizard, so a
   hand-edited config cannot put a page on the command line that no transaction finishes.
3. **The transaction limit can be lifted**, per capture and per restore, for a realm too large to
   move inside it. Read the trade below before using it.

## The screens

The second half of the release is a pass over the interface, and one change is worth stating as a
behaviour rather than as a look:

- **A restore starts from the snapshot being restored.** Restore has left the navigation rail. It
  was the one item that could not act on its own — every restore is *of a particular snapshot*, and
  the wizard it opened existed to ask which one — so it is now the button on each row of the
  Snapshots table and in the inspector's header, and the wizard opens on the destination step when
  it already knows. Capture left the rail for the same reason: it starts from the screen its result
  lands on.
- **The Activity card names which snapshot a restore is applying**, and when the run began. A realm
  and a destination do not distinguish two captures of the same realm a fortnight apart, and the
  difference between restoring one and the other is a fortnight of user changes.
- **Every date is written out in full, with the zone named.** A record of a moment on someone else's
  server does not get to be ambiguous.

## Upgrading

Nothing to do. Snapshots, manifests and configuration are unchanged, and a job queued by 0.0.1 that
0.0.2 resumes behaves the same way.

Two changes are visible immediately and neither needs a decision:

- **User files inside a snapshot are numbered `acme-users-000.json` rather than
  `acme-users-0.json`**, all the same width. This applies to snapshots captured by 0.0.2; existing
  bundles keep the names they were written with, and restore either way. Keycloak's `import` matches
  `-users-[0-9]+\.json`, verified against 24.0, 26.3 and 26.5.0, so padded names are found exactly
  as unpadded ones were.
- **Dropdowns are drawn by the app** rather than by the operating system, so they carry the same
  typography and spacing as the rest of the interface.

## Before you lift the transaction limit

The option is off unless asked for, and it should stay off unless a capture has actually failed on
the limit. What it does is not "turn transactions off" — Keycloak's export and import are each
written as a sequence of them and no option turns that off. It removes the *time limit* on one, by
setting `QUARKUS_TRANSACTION_MANAGER_DEFAULT_TRANSACTION_TIMEOUT=0` in the invocation's
environment.

That limit is also the only thing bounding a run that has stopped making progress. Without it, an
export whose directory has stopped answering holds a connection to the database open until the
clone is destroyed — and on Docker and Kubernetes that connection is the serving instance's. On a
restore the trade is sharper still: an export cancelled part-way leaves nothing behind, an import
leaves a half-applied realm.

Reach for a smaller users-per-file first. It fixes the same failure without the trade.

One caveat stated plainly: that variable is a Quarkus setting, not a published Keycloak option, so
a future Keycloak release may stop honouring it. PortCloak passes it and shows it in the logged
command line rather than claiming it worked.

## What 0.0.2 does not do

Everything in [0.0.1's list](./11-release-0.0.1.md#what-001-does-not-do) still stands: sessions are
out of scope, themes and provider JARs are reported rather than migrated, restore is whole-realm,
and one snapshot is one realm.

Added to it for this release:

- **A merge restore cannot have its transaction limit lifted.** Merge has no offline equivalent, so
  it runs through the Admin API's `partialImport` against a live server — a transaction PortCloak
  did not start and cannot configure. If a merge of a very large realm times out, the fix is on the
  destination's own configuration.
- **A hung directory is not survivable by any setting here.** If the LDAP connection stops
  answering rather than merely being slow, no page size finishes and no timeout is long enough. The
  federation provider's own connection and read timeouts are realm configuration on the source, not
  something PortCloak passes.
- **Platform signatures are still not in place.** No release carries an Apple Developer ID
  signature or Authenticode. The SHA256SUMS signature and the build provenance attestation are the
  verification, and each release's notes state what that particular build actually got. See
  [`CODE_SIGNING.md`](../../CODE_SIGNING.md).

## Trying it before releasing it

`playground/` is new in this release and exists for exactly this: three Keycloaks with Postgres and
two LDAP directories, in Docker or on CRC, plus the three storage backends that are not a folder,
and a generator that fills them with realms worth capturing — four client shapes, nested groups,
composite roles, OTP and passkey enrolments, and users whose names contain the characters somebody
assumed would never appear in one.

```bash
docker compose -f playground/storage/compose.yml up -d
docker compose -f playground/target/docker/compose.yml up -d
playground/seed/seed.sh docker --ldap-users 20000
```

Capture `corp-a` from kc-a, restore it into kc-merged, and compare against kc-b, which was seeded
identically and never touched. That is the check this release's fixes were found by, and the one
worth running before cutting it.

## Release checklist

The full gate is [11 — Release 0.0.1](./11-release-0.0.1.md#release-gate) and it does not get
re-run in full for a maintenance release. What does:

- [ ] `go test ./...`, `go vet ./...`, `gofmt -l .`, and the frontend's `check`, `format:check`
      and `test`.
- [ ] The fidelity round trip on `rich` and `federated`: capture → restore → re-export →
      identical inventory. Padded user filenames make this release's first re-export the one that
      proves the rename did not lose a page of users.
- [ ] A capture against a realm with a federation provider, at the default page size and at 100.
- [ ] A restore of a 0.0.1 bundle, unpadded filenames and all.
- [ ] `build/windows/winres.json` carries the version being released. It is the one version
      declaration `package.sh --version` does not substitute.
- [ ] The drafted release notes' signature section matches what the build actually got.
