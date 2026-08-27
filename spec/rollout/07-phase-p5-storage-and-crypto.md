<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# P5 — Storage & Encryption

**Goal.** Snapshots reach the two storage backends operators actually keep long-term backups in
— S3-compatible object storage and Azure Blob — with resumable multipart transfers that inherit
P4's resilience. Encryption becomes real: opt-in, prominently offered, and impossible to get
wrong by accident. When it is declined, the tool says so in a way that cannot be missed, because
an unencrypted bundle holds unmasked client secrets and private signing keys in the clear.

**Covers.** UC-S3, UC-S4, UC-S8, UC-C10 · FR-S3, FR-S4, FR-S5 · NFR-3.

**Depends on.** P0…P4.

**Packages.** `engine/store/s3`, `engine/store/azure`, `engine/crypto`, `engine/snapshot`
(envelope), `frontend/`.

---

## Tasks

### T-P5.1 — S3 store with resumable multipart
`ResumableStore` over `aws-sdk-go-v2`, with an overridable endpoint so MinIO is the same code
path rather than a special case. Multipart upload IDs and part ETags are recorded in the P4
checkpoint, so an interrupted upload resumes from the last completed part rather than restarting.
Abort on discard, so a cancelled job does not leave billable incomplete uploads behind.

*Done when:* the `BlobStore` contract suite passes unchanged against MinIO and against real S3;
the resume and abort paths are proven by fault injection.

### T-P5.2 — Azure Blob store with resumable block upload
`ResumableStore` over `azblob` using staged blocks. The block list is the checkpoint. Validated
against Azurite in CI and against a real account manually, because Azurite's fidelity to the real
service is good but not total.

*Done when:* the contract suite passes against Azurite; the staged-block resume path is proven;
uncommitted blocks are cleaned up on discard.

### T-P5.3 — List, retrieve, delete across every backend
FR-S5 for all four backends, with the sidecar files ([06 §6.1](../06-snapshot-and-manifest.md))
driving listing so the library can show realm, capture time and size **without downloading or
decrypting a bundle**. This is what makes Tier 0 inspection possible in P6, and it is why the
sidecars sit next to the bundle rather than inside it.

Delete removes the bundle and both sidecars as one operation, and reports what it actually
removed rather than assuming.

*Done when:* list/get/delete are green across all four backends; listing a bucket of 500
snapshots is fast and pages properly; a bundle whose sidecar is missing still lists, marked as
needing a deeper read.

### T-P5.4 — Crypto vault
`age` encryption over the sealed bundle, with passphrase and recipient-key modes. The envelope
records that the bundle is encrypted, by what scheme, and for whom — enough to know whether you
can open it before you try. Streaming, so encryption does not break NFR-6.

Decryption is verified at capture time by reading the first block back: a bundle that cannot be
decrypted is discovered at capture, not eighteen months later during an incident.

*Done when:* round-trip works in both modes; a wrong passphrase fails clearly; the streaming
memory ceiling holds; the capture-time decryptability check catches a deliberately corrupted key.

### T-P5.5 — The opt-in that is honestly presented
The capture wizard's encryption step becomes real ([08 §8.2](../08-security.md),
[D8](../12-decisions.md)). Encryption is **offered prominently and recommended**, and declining
is one deliberate action, not a default. A storage marked **encryption required** (P1) removes
the opt-out entirely for bundles written there.

An unencrypted bundle is labelled as such in the library, in the manifest and in the completeness
report. This is the one place where a slightly uncomfortable UI is the correct design: an
operator should never be able to say afterwards that they did not realise the file contained
unmasked secrets.

*Done when:* declining requires a deliberate action; an encryption-required storage cannot receive
an unencrypted bundle; the unencrypted label appears in all three places.

### T-P5.6 — Browse a storage backend
UC-S8: browse what a storage actually contains, including objects PortCloak did not write.
Foreign objects are shown as unrecognised rather than hidden — an operator debugging a
misconfigured prefix needs to see what is really there, and silently filtering is how a prefix
typo goes unnoticed for a month.

*Done when:* browsing works across all four backends; foreign objects are visible and marked;
a read-only credential browses successfully without misreporting an error.

---

## Testing

**Unit.** Envelope encoding across schemes. Crypto round-trip, wrong-key, and truncated-input
cases. Multipart checkpoint serialisation. Sidecar parsing, including a malformed sidecar.

**Contract.** `BlobStore` across all four implementations — disk, sftp, s3, azure — from the same
table. Any divergence is a bug in the newest implementation, not a reason to fork the table.

**Fault injection.** Interrupt an S3 multipart at part 7 of 20; resume; assert the final object's
ETag matches an uninterrupted upload. The same against Azure staged blocks. Kill the process
mid-upload, restart, resume, and assert no duplicate or orphaned upload remains.

**Integration.** Capture to MinIO, to Azurite, and to real S3 (manually, once per release).
Encrypted and unencrypted variants of each. Listing performance against 500 snapshots.

**Security.** `TestUnencryptedBundleIsLabelled` — the label appears in the library, the manifest
and the completeness report. `TestEncryptionRequiredStorageRejectsPlaintext`. The redaction suite
extended to cover the sidecar files, which are the one artifact deliberately written in the clear
and therefore the one most likely to leak by accident.

**Manual.** Capture unencrypted and look at every screen where that bundle appears. If you can
lose track of the fact that it holds plaintext client secrets, the design is wrong regardless of
what the tests say.

## Verification

| Requirement | Evidence |
|---|---|
| FR-S3 | Contract suite green against MinIO and real S3; `TestS3_MultipartResume`. |
| FR-S4 | Contract suite green against Azurite; `TestAzure_StagedBlockResume`. |
| FR-S5 | `TestListGetDelete_AllBackends`, plus the 500-snapshot listing benchmark. |
| NFR-3 | `TestCrypto_RoundTrip`, `TestCrypto_WrongKey`, `TestUnencryptedBundleIsLabelled`, `TestEncryptionRequiredStorageRejectsPlaintext`, and redaction coverage of the sidecars. |
| UC-C10 | Manual walkthrough: an encrypted capture and a declined one, screenshots of both. |
| UC-S3 · UC-S4 | Storage configured and captured to, end to end, screenshots attached. |
| UC-S8 | Storage browser screenshot showing both recognised snapshots and a foreign object. |
| NFR-6 | `TestCrypto_BoundedMemory` — encryption of a 2 GB bundle stays under the ceiling. |

## Demo

Capture a realm straight to MinIO with encryption on. Interrupt the network at part 7 of the
multipart upload; watch it retry, then resume and complete. Show that the object's digest matches
one uploaded without interruption. Then capture the same realm unencrypted and walk through the
library, the manifest and the completeness report — each one says, unmissably, that this bundle
holds unmasked secrets in the clear.

## Exit criteria

- [ ] All four storage backends pass the same contract suite.
- [ ] S3 and Azure uploads resume after an app restart and converge.
- [ ] Encryption round-trips in both modes and is verified at capture time.
- [ ] Declining encryption is deliberate; encryption-required storage cannot be bypassed.
- [ ] An unencrypted bundle is labelled in the library, the manifest and the report.
- [ ] A storage backend can be browsed, foreign objects included.

## Commits

```
feat(store/s3): resumable multipart uploads with endpoint override for MinIO
feat(store/azure): staged-block uploads against azurite and azure blob
feat(store): list, retrieve and delete driven by sidecar metadata
feat(crypto): streaming age encryption with passphrase and recipient modes
feat(crypto): verify decryptability at capture time
feat(ui): encryption opt-in and the unencrypted-bundle labelling
feat(ui): storage browser including unrecognised objects
test(store): fault injection across multipart and staged-block resume
```

## Risks

**Azurite is not Azure.** Some behaviours differ, particularly around conditional headers.
*Mitigation:* Azurite in CI for coverage, one manual run against a real account before release,
and the divergences recorded in the phase record rather than in someone's memory.

**S3-compatible is not S3.** MinIO, Ceph and Wasabi differ around multipart edge cases and
listing consistency. *Mitigation:* the contract suite is the compatibility statement — anything
that passes it is supported, and 0.0.1 claims support only for what has been run.

**Encryption becoming de facto mandatory by UI pressure.** [D8](../12-decisions.md) says opt-in.
A warning so loud it is effectively a block violates the decision as much as a silent default
would. *Mitigation:* the manual pass explicitly checks that declining is a normal, respected
choice, clearly labelled rather than punished.
