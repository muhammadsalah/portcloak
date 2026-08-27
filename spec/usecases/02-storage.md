# 02 — Storage

> A **Storage** definition is a destination where snapshot bundles are written and read.
> Every kind is rooted at a **folder / prefix**, so one disk, host, bucket or container can hold
> several independent snapshot trees. Several storages may be defined; one is the **default**.

![Configuration use cases](./diagrams/png/uc-02-configuration.png)

*Source: [`uc-02-configuration.puml`](./diagrams/uc-02-configuration.puml) · [SVG](./diagrams/svg/uc-02-configuration.svg)*

## Storage kinds and their fields

| Kind | Required fields | Optional |
|------|-----------------|----------|
| **Disk** | Name, root **folder** | Encryption-required |
| **SSH** | Name, host, port, user, auth, remote **folder** | Jump host, encryption-required |
| **S3** | Name, endpoint, region, bucket, **prefix (folder)**, credentials | Path-style addressing, part size, storage class, SSE |
| **Azure Blob / Azurite** | Name, account or endpoint, container, **prefix (folder)**, credentials | Block size, access tier |

Layout written under the configured folder/prefix:

```
<folder|prefix>/<realm>/<timestamp>-<snapshot-id>.pck
                       /<timestamp>-<snapshot-id>.manifest.json   # non-secret sidecar
                       /<timestamp>-<snapshot-id>.sha256
```

---

## UC-S1 — Add Disk storage

**Goal.** Use a folder on this machine to hold snapshots.
**Trigger.** *Storage → Add → Disk*.

**Main success scenario**
1. Operator enters a name and picks a **root folder**.
2. PortCloak verifies the folder exists and is writable, and reports free space.
3. Operator triggers **Test**: a temp file is written, fsynced, read back and removed.
4. Operator saves.

**Alternate flows**
- **A1 — Folder does not exist.** PortCloak offers to create it.
- **A2 — Set as default** for new captures (UC-S7).

**Exceptions**
- **E1 — Not writable.** The exact path and permission error are shown; save allowed as *Untested*.
- **E2 — Insufficient free space.** Reported as a warning with the number, not a hard block.

**Postconditions.** A Disk storage exists.
**Covers.** FR-N2, FR-N3, FR-S1.

---

## UC-S2 — Add SSH storage

**Goal.** Push snapshots to a folder on a remote host over SFTP.
**Trigger.** *Storage → Add → SSH*.

**Main success scenario**
1. Operator enters name, host, port, user and auth method; the secret goes to the **OS keychain**.
2. Operator enters the remote **folder**.
3. Operator triggers **Test**: PortCloak connects, verifies the folder is writable, uploads and
   removes a probe file, and confirms it can compute a remote checksum.
4. Operator saves.

**Alternate flows**
- **A1 — Jump host** in the chain, tested end to end.
- **A2 — Shares an SSH identity with an Environment.** The same keychain entry may be reused;
  PortCloak references it rather than duplicating the secret.

**Exceptions**
- **E1 — Unreachable / auth rejected.** Auth failure is non-retryable and reported as such.
- **E2 — Remote folder not writable.** Path and error surfaced.
- **E3 — No remote shell for `sha256sum`.** Integrity falls back to re-read-and-hash; the Test
  reports which method will be used rather than hiding it.

**Postconditions.** An SSH storage exists.
**Covers.** FR-N2, FR-N3, FR-S2.

---

## UC-S3 — Add S3-compatible storage

**Goal.** Store snapshots in an S3 bucket under a prefix.
**Trigger.** *Storage → Add → S3*.

**Main success scenario**
1. Operator enters name, endpoint, region, bucket and **prefix**.
2. Operator supplies access key and secret; both go to the **OS keychain**.
3. Operator triggers **Test**: PortCloak lists the prefix, performs a small multipart upload,
   verifies the checksum, then aborts/removes the probe object.
4. Operator saves.

**Alternate flows**
- **A1 — MinIO or other S3-compatible store.** Custom endpoint plus **path-style addressing**.
- **A2 — Tune part size** for the link quality; smaller parts resume faster on flaky networks.
- **A3 — Server-side encryption** requested in addition to PortCloak's own (belt and braces).

**Exceptions**
- **E1 — Bucket missing or no permission.** Reported with the operation that failed
  (`ListObjects`, `CreateMultipartUpload`, …), not a generic error.
- **E2 — Clock skew / signature failure.** Called out explicitly, since it is a classic
  S3 misconfiguration.
- **E3 — Probe upload interrupted.** `AbortMultipartUpload` runs so no orphan parts accrue cost.

**Postconditions.** An S3 storage exists.
**Covers.** FR-N2, FR-N3, FR-S3.

---

## UC-S4 — Add Azure Blob / Azurite storage

**Goal.** Store snapshots in an Azure Blob container under a prefix.
**Trigger.** *Storage → Add → Azure Blob*.

**Main success scenario**
1. Operator enters name, account or endpoint, container and **prefix**.
2. Operator supplies a connection string / key / SAS; it goes to the **OS keychain**.
3. Operator triggers **Test**: PortCloak lists the prefix, stages and commits a small block blob,
   verifies it, and deletes it.
4. Operator saves.

**Alternate flows**
- **A1 — Azurite emulator.** Operator points the endpoint at Azurite's dev endpoint; the same
  code path serves both, which is exactly how the Azure path is developed and tested.

**Exceptions**
- **E1 — Container missing.** PortCloak offers to create it.
- **E2 — Credential rejected / SAS expired.** Non-retryable, reported plainly.
- **E3 — Uncommitted blocks left by an interrupted probe.** Cleaned up on the spot.

**Postconditions.** An Azure storage exists.
**Covers.** FR-N2, FR-N3, FR-S4.

---

## UC-S5 — Test a storage

**Goal.** Confirm a storage is reachable and writable before relying on it.

**Main success scenario**
1. PortCloak performs a **round trip**: list → write probe → verify checksum → delete probe.
2. It reports latency, whether resumable upload is available, and which integrity method applies.
3. The storage is stamped *Tested OK* with a timestamp.

**Exceptions**
- **E1 — Any step fails.** The failing step is named. Probe artifacts are cleaned up even when
  the test fails.

**Postconditions.** Test status recorded; no residue left behind.
**Covers.** FR-N3.

---

## UC-S6 — Edit or delete a storage

**Goal.** Change endpoint/folder/credentials, or remove a storage.

**Main success scenario (edit)**
1. Operator edits fields; changed credentials replace the keychain entry.
2. *Tested OK* is cleared; Operator may re-Test.

**Main success scenario (delete)**
1. Operator chooses *Delete* and confirms.
2. PortCloak removes the entry and its keychain secret. **Stored snapshot files are not deleted** —
   removing a storage definition forgets *how to reach* the data, it does not destroy it.

**Alternate flows**
- **A1 — Deleting the default storage.** PortCloak requires another default to be chosen first.

**Exceptions**
- **E1 — In use by a running job.** Deletion refused until the job completes or is discarded.
- **E2 — Snapshots exist there.** PortCloak states how many it knows about and confirms that the
  files will be left in place.

**Postconditions.** Definition updated or removed; remote data untouched.
**Covers.** FR-N4, FR-N5.

---

## UC-S7 — Set the default storage

**Goal.** Make one storage the pre-selected destination for new captures.

**Main success scenario**
1. Operator marks a storage as **default**; any previous default is unmarked.
2. The capture wizard pre-selects it, and the Operator can still override per capture.

**Postconditions.** Exactly one default storage exists.
**Covers.** FR-N5.

---

## UC-S8 — Browse the contents of a storage

**Goal.** See what snapshots a storage actually holds.

**Main success scenario**
1. Operator opens a storage and chooses *Browse*.
2. PortCloak lists objects under the folder/prefix, grouped by realm, reading only the
   **non-secret sidecar manifests** — **no decryption key is required** (Tier 0).
3. Each entry shows realm, timestamp, size, completeness and whether it is encrypted.

**Alternate flows**
- **A1 — Foreign objects present.** Files that are not PortCloak snapshots are listed separately
  as unrecognised rather than hidden or assumed.

**Exceptions**
- **E1 — Listing fails midway.** Partial results are shown, clearly marked incomplete, with the
  failure reason — never presented as a full listing.

**Postconditions.** Read-only.
**Covers.** FR-S5, FR-V1.
