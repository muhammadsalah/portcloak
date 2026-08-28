<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# Code signing policy

PortCloak's releases are signed. This page says who can sign, what has to be
true before a signature is applied, and how to check one yourself — because a
signature is only worth as much as the process behind it, and that process
should be readable rather than assumed.

## Who does what

PortCloak has one maintainer. Stating the roles separately is not bureaucracy
for its own sake: they are different decisions, and writing them down is what
makes it visible if one of them is ever delegated.

| Role | Who | What it means here |
|---|---|---|
| **Author** | Muhammad Salah | Trusted to commit to `main` without further review. |
| **Reviewer** | Muhammad Salah | Every change proposed by anyone who is not a committer is reviewed before it is merged. No contribution reaches a release unreviewed. |
| **Approver** | Muhammad Salah | Approves each individual signing request. Approval is per release, never standing. |

If that list grows, this table changes with it, in a commit.

## What has to be true before anything is signed

1. The commit is on `main` and the full test suite is green on it. CI runs the
   unit, contract, redaction and integration suites; see
   [`spec/rollout/01-test-strategy.md`](./spec/rollout/01-test-strategy.md).
2. A `v*` tag is cut on that commit, and the tag is signed.
3. The release gate in
   [`spec/rollout/11-release-0.0.1.md`](./spec/rollout/11-release-0.0.1.md) is
   reviewed, every box with its evidence.
4. Artifacts are built **only** by
   [`.github/workflows/release.yml`](./.github/workflows/release.yml), from
   that tag, in public CI. Nothing built on a laptop is ever signed.
5. Each signing request is approved by hand. There is no automatic approval and
   no signing policy that would permit one.

Signing credentials live in the release workflow's secrets and nowhere else.
They are deliberately absent from `build/package.sh`, so the script anyone can
run — including from a pull request — cannot reach them.
[`build/README.md`](./build/README.md#signing-and-notarisation) explains that
split.

## What is signed, and how to verify it

| Platform | Signature |
|---|---|
| macOS | Apple Developer ID, hardened runtime, notarised by Apple and stapled |
| Windows | Authenticode, certificate by SignPath Foundation |
| all platforms | `SHA256SUMS` signed with [Sigstore](https://www.sigstore.dev/) cosign in keyless mode, plus a build-provenance attestation per artifact |

The last row is the one to check, and it is the one that answers the question
worth asking. A Developer ID or Authenticode signature tells you a certificate
authority knows who paid for a certificate. It does not tell you the bytes came
from this source tree. The Sigstore signature and the provenance attestation
do — they bind each artifact to this repository, this workflow and this commit,
and there is no private key involved for anyone to lose:

```bash
sha256sum -c SHA256SUMS

cosign verify-blob --bundle SHA256SUMS.cosign.bundle \
  --certificate-identity 'https://github.com/muhammadsalah/portcloak/.github/workflows/release.yml@refs/tags/v0.0.1' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  SHA256SUMS

gh attestation verify PortCloak-0.0.1-macos-universal.zip --repo muhammadsalah/portcloak
```

Every release publishes those commands in its own notes, with its own tag
substituted.

## Privacy

PortCloak collects nothing. It has no telemetry, no analytics, no crash
reporting and no update check, and it opens no network connection of its own —
the only systems it contacts are the Keycloak instances and storage backends an
operator configures. There is no account and no sign-in, because there is
nobody to sign in to. Configuration is plain YAML under `~/.portcloak/`, every
credential is a handle into the OS keychain, and the audit log is a local file.

Nothing is transmitted to the maintainer or to any third party as a consequence
of running the application, so there is no personal data to describe the
handling of. If that ever changes, it changes here first.

## Reporting a problem with a signature

If an artifact carrying a PortCloak signature does not verify, or you believe
one was issued for something not built from this repository, open an issue —
or, if you would rather not do that publicly, use GitHub's private
vulnerability reporting on this repository.

---

Free code signing provided by [SignPath.io](https://signpath.io), certificate
by [SignPath Foundation](https://signpath.org).
