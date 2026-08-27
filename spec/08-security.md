# 08 — Security & Secret Handling

PortCloak snapshots are among the most sensitive artifacts an organization can produce: they
contain password hashes, OTP seeds, passkey material, client secrets, LDAP bind credentials,
IdP secrets, SMTP passwords, and **RSA private signing keys**. Security is therefore a core
design requirement (NFR-3), not a bolt-on.

## 8.1 Threat model (summary)

| Asset | Threat | Mitigation |
|-------|--------|-----------|
| Snapshot at rest (in any storage backend) | Storage backend compromise, misconfigured public bucket, stolen disk | **Encrypt before store** (opt-in) ⇒ storage backend untrusted. **If declined**, the file is plaintext-sensitive and storage backend hardening becomes the only control — flagged loudly. Sidecars carry no secrets either way. |
| Snapshot in transit | MITM on SSH/S3/Azure | TLS/SSH transport + the payload is already encrypted end-to-end. |
| PortCloak's own creds (SSH keys, cloud keys, admin creds) | Plaintext config theft | Stored in **OS keychain** and referenced by handle; `config.yaml` holds no secret values. |
| Secrets leaking to logs/UI | Accidental disclosure | **Redaction** everywhere; manifest references secrets by location, never value. |
| Malicious/tampered snapshot on restore | Supply-chain of a bad bundle | **Integrity tree verified** + optional signature before any import. |
| Over-broad restore | Wrong realm overwritten | Dry-run diff + explicit strategy + separate destination environment scope. One realm per snapshot bounds the blast radius. |
| Ephemeral clone receiving live traffic | Inherited selector labels attach the clone to a production Service | **Strip all inherited labels**; apply only PortCloak's own ([03 §3.3](./03-capture-targets.md)). |
| Orphaned clone left running | Crash mid-capture leaves a container with DB credentials parked | Deferred teardown + `ttlSecondsAfterFinished` + label-based orphan sweep on launch. |
| Inspection residue on disk | Local index retains usernames/emails after use | Index is **session-scoped and destroyed on close** (NFR-10, [10 §10.3](./10-snapshot-inspection.md)). |

## 8.2 Encryption at rest — opt-in

- **Opt-in, prominently promoted.** Encryption is not forced. The capture wizard presents the
  toggle **on** and requires a deliberate action to disable it.
- **Two modes:**
  - **Passphrase** — AES-256-GCM with a strong KDF (scrypt/Argon2id) over an operator passphrase.
  - **Recipients** — `age`/X25519 public keys; each listed recipient can decrypt with their own
    private key. Good for teams and for separating "who can capture" from "who can restore".
- **AEAD** (GCM) gives confidentiality **and** tamper-evidence on the payload; the separate
  integrity tree covers structure and enables partial verification.
- **What is not encrypted:** `envelope.json` and the sidecar `*.manifest.json` — deliberately
  secret-free (counts, categories, completeness) so listing works without keys.

### Where the keys live

Encryption material is held on exactly the same terms as every other secret PortCloak needs, and
for the same reason: a secret the tool refuses to keep is a feature the operator turns off. A
passphrase typed at capture and typed again at every restore, on every machine, is friction — and
friction is what decides whether encryption is on at all.

- **A key is named.** `config.yaml` carries a `keys:` list of entries with a name, a kind
  (`identity` or `passphrase`), the age public key where there is one, and a credential handle.
  The secret half is in the OS keychain, never in the file — so configuration stays portable
  between machines and the secrets deliberately do not, exactly as in §8.4.
- **PortCloak can create one.** An age keypair is generated in-app, its private half stored, its
  public half recorded and shown. It is also shown once as a copy to keep elsewhere, because a
  key that exists only in one machine's keychain is a key that a lost machine takes with it.
- **A capture seals to a key by name.** Recipient mode lists stored keys rather than asking for a
  public key to be pasted, which is what made it something operators read about and skipped. A
  pasted recipient is still accepted: a colleague's key is legitimate and PortCloak will never
  hold its private half.
- **An open tries what is held before asking.** Restore and inspection attempt the stored keys
  while reading the envelope — the first document in the archive, so a wrong key fails there
  rather than after a full extraction. Order is: nothing, then whatever the operator supplied,
  then each stored key with identities before passphrases (an identity attempt is free; scrypt
  deliberately is not).
- **Silent is not invisible.** The key that opened a snapshot is named on the screen, and every
  creation, import, reveal and deletion is in the audit log. What replaces a prompt has to leave
  more evidence than the prompt did, not less.
- **Deletion is irreversible and is presented as such.** A key is not in use by anything PortCloak
  can see; it is in use by every snapshot ever sealed with it, in backends that may not be
  configured here. Deleting one requires typing its name.

### When encryption is declined

This is a supported choice, and it is worth being precise about what it means rather than
burying it. An unencrypted snapshot is a plaintext file containing unmasked client secrets, LDAP
bind credentials, IdP secrets, SMTP passwords and **RSA private signing keys**. Possession of it
is equivalent to possession of the realm.

PortCloak's obligations in that mode:

- **Say so, every time** — an explicit confirmation at capture, a persistent warning badge on the
  snapshot in the library, and a banner when it is opened or restored.
- **Record the choice** in the audit log (action and time — there is no user account to record).
- **Restrict what it can** — local files written `0600` into a restricted app directory;
  recommend (and where the SDK allows, request) server-side encryption on S3/Azure and strict
  file modes over SFTP.
- **Never treat it as equivalent** — the storage backend stops being untrusted
  ([04 §4.6](./04-storage-backends.md)) and the docs, UI and manifest all reflect that.

Organizations that want to remove the choice can set encryption as **mandatory by policy** on a
storage definition, so a snapshot written there can never be left unencrypted.

## 8.3 Secrets in the payload — carried, not masked

The brief requires client (and other) secrets to be **unmasked** so re-import yields a working
realm. PortCloak honors this by:

- Keeping secrets verbatim **inside the encrypted realm JSON** (never separately, never in the
  clear on disk).
- Recording each in the **secret ledger** ([07 §7.2](./07-realm-carryover-manifest.md)) by
  *type and location* only.
- Optionally **verifying via Admin API** that exported secrets are real (not `**********`), so a
  version-specific masking quirk is caught and flagged rather than shipped as a dud.

This is a security-relevant *feature*: the sensitivity is acknowledged, contained by encryption,
and auditable via the ledger.

## 8.4 Credential handling for PortCloak itself

- Environments store **non-secret** fields (host, port, namespace, bucket, endpoint).
- Secrets (SSH key passphrase, private keys, cloud access keys, admin password/token) live in
  the **OS keychain** via `go-keyring` (macOS Keychain / Windows Credential Manager / libsecret).
- Nothing sensitive is written to the config file or to logs.

## 8.5 Redaction & logging

- A `slog` redaction handler scrubs known-sensitive keys and pattern-matches secret-shaped values
  before anything is written.
- The **partial-failure ledger** and audit log store outcomes and locations, never values.
- UI progress/log panes render the same redacted stream.

## 8.6 Integrity & authenticity on restore

- **Integrity tree** (SHA-256 per artifact + root) recomputed on restore; any mismatch aborts.
- **Optional signing:** a snapshot may be signed (e.g. with an age/ed25519 identity) so a
  destination operator can verify provenance before importing.
- **No import without verification:** decrypt → verify integrity → preview → confirm → import.

## 8.7 Least privilege & scoping

- PortCloak has **no login and no in-app permissions** (N8). Authority comes entirely from the
  credentials each environment carries, so scoping is done by **how you define environments**,
  not by roles in the tool.
- Keep **capture** and **restore** environments separate: a capture-only environment needs just
  enough to run `kc.sh export`, while a restore target carries the higher privilege to import.
  Defining them as distinct entries means the destructive credential is only present where a
  restore is actually intended (NFR-7).
- **Ephemeral clone execution raises the source privilege floor** and this is stated plainly: on
  Kubernetes the source environment needs `create`/`delete` on `jobs` and `pods` plus `create` on
  `pods/exec` in the target namespace; on Docker it needs container create/exec/remove. That is
  more than "read-only", and it is the price of never touching the serving instance. PortCloak
  documents the exact verbs, checks them during `Probe`, and scopes them to one namespace.
- Admin-API verification uses a scoped service account where possible (realm-view as needed),
  not a superuser by default.

## 8.8 Data lifecycle & hygiene

- **Ephemeral clones are destroyed unconditionally** — they carry the same DB credentials as the
  serving instance, so a parked clone is a standing credential exposure. Teardown runs on
  success, failure and cancellation, backstopped by TTL and an orphan sweep.
- Temp export dirs inside targets are **cleaned up** after fetch (best-effort, logged).
- Local working files are written under a restricted-permission app dir and shredded after
  sealing.
- **The inspection index is session-scoped**: created when a snapshot is opened, dropped and
  securely deleted when it is closed, so browsing does not leave a searchable copy of the realm's
  user directory on the workstation (NFR-10). It lives in its own file under
  `~/.portcloak/index/`, isolated from configuration, so it can be purged wholesale at any time.
- PortCloak does not expire snapshots on its own — a secret-bearing bundle lives in its storage
  backend until someone deletes it. Where that matters, use the storage's own lifecycle controls
  (S3 lifecycle rules, Azure management policies, or housekeeping on disk/SSH).

## 8.9 Requirement coverage

Fulfills **NFR-3**, **NFR-7** and **NFR-10**, and underpins **FR-R4** (verify+decrypt before
restore), **FR-F3** (unmasked secrets, protected when encryption is enabled), **FR-V7** (audited
reveal) and the secret ledger in **FR-M1**.
