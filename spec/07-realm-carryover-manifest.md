<!--
  Copyright 2026 Muhammad Salah
  SPDX-License-Identifier: Apache-2.0
-->

# 07 — Realm Carry-Over Manifest

This is the heart of PortCloak's promise. The **manifest** is the contract that says, for a
given snapshot, *exactly what was carried over* — down to the individual secret — so that an
imported realm is a faithful clone and nothing important vanishes silently.

Two audiences:
- **Machines:** `manifest.json` (per realm) drives restore, diffing, and validation.
- **Humans:** the UI renders it as a checklist with counts and a completeness verdict.

> Design principle: **secrets are carried in the encrypted realm JSON; the manifest references
> them by type and location, never by value.** The manifest is safe to preview; the payload is
> not (see [08](./08-security.md)).

## 7.1 Coverage map — everything a realm carries

Legend for **Source**: `KC` = comes from offline `kc.sh export` realm JSON; `Hybrid` = KC with
Admin API verification that the value is unmasked; `API` = detected via Admin API only.

### A. Realm settings & policies
| Item | Carried | Secret? | Source | Notes |
|------|:------:|:------:|:------:|-------|
| Realm core (name, enabled, display, SSL required) | ✅ | — | KC | |
| Token lifespans (access, refresh, id, offline) | ✅ | — | KC | Token continuity relies on these + keys. |
| SSO session idle/max, offline session settings | ✅ | — | KC | |
| Password policy (hashing algo, iterations, rules) | ✅ | — | KC | Must match so imported hashes stay valid. |
| Brute-force detection settings | ✅ | — | KC | |
| OTP policy (type, algo, digits, period) | ✅ | — | KC | Must match so existing OTP secrets verify. |
| WebAuthn policy (passwordless + 2FA) | ✅ | — | KC | Must match for passkeys to keep working. |
| Realm attributes | ✅ | maybe | KC | Attributes can hold config secrets → scanned. |
| Internationalization / supported locales | ✅ | — | KC | |
| Localization texts (custom messages) | ✅ | — | KC/API | |
| Themes (login/account/email/admin) — the *selection* | ✅ | — | KC | The theme **files** are not in realm JSON — detected and reported, see section M. |
| Default roles / default groups | ✅ | — | KC | |
| Events config (listeners, storage, expiration) | ✅ | — | KC | Event *data* not exported (config only). |

### B. Cryptographic keys (token signing) — **FR-F4**
| Item | Carried | Secret? | Source | Notes |
|------|:------:|:------:|:------:|-------|
| `rsa-generated` key providers | ✅ | 🔑 private key | Hybrid | Stored as components → in export. |
| `rsa` (imported) key providers | ✅ | 🔑 private key | Hybrid | |
| `rsa-enc` (encryption keys) | ✅ | 🔑 | Hybrid | |
| `ecdsa` key providers | ✅ | 🔑 | Hybrid | |
| `hmac-generated` (HS signatures) | ✅ | 🔑 secret | Hybrid | |
| `aes-generated` (cookie/secret enc) | ✅ | 🔑 | Hybrid | |
| `java-keystore` providers | ✅ | 🔑 | Hybrid | Keystore *file* path noted; embedded material carried if in component. |
| Active KID / priority / algorithm | ✅ | — | Hybrid | Carrying keys ⇒ **tokens signed before the move stay verifiable**. |
> The optional Admin API verification step confirms the exported components actually contain
> private material (guarding against version-specific masking quirks); any masked key is flagged
> `partial`. **Carrying the active signing key is what replaces session portability** — tokens
> issued before the move remain verifiable afterwards (see section L).

### C. Clients — **FR-F3, FR-F7**
| Item | Carried | Secret? | Source | Notes |
|------|:------:|:------:|:------:|-------|
| Client definitions (clientId, protocol, flags) | ✅ | — | KC | |
| **Client secret (confidential)** | ✅ | 🔑 **unmasked** | Hybrid | Present in realm JSON; **API-verified** it is real, not `**********`. |
| Redirect URIs, web origins, base/admin URLs | ✅ | — | KC | |
| Protocol mappers (per client) | ✅ | — | KC | |
| Default & optional client scopes (assignment) | ✅ | — | KC | |
| Service account enablement + its roles | ✅ | — | KC | |
| Client authenticator (jwt, secret, x509) config | ✅ | 🔑 maybe | KC | Signed-JWT client keys carried if stored. |
| **Authorization services** (resources, scopes, policies, permissions) | ✅ | maybe | KC | Full authz model for clients with it enabled. |
| Registration access tokens | ⚠️ partial | 🔑 | KC | Rarely needed; noted if absent. |

### D. Client scopes (realm-level) — **FR-F7**
| Realm client scopes + their protocol mappers | ✅ | — | KC | Default/optional assignment preserved. |

### E. Roles — **FR-F7**
| Realm roles | ✅ | — | KC |
| Client roles | ✅ | — | KC |
| Composite role relationships | ✅ | — | KC |

### F. Groups — **FR-F7**
| Group hierarchy + attributes | ✅ | — | KC |
| Group role mappings (realm + client) | ✅ | — | KC |
| Default groups | ✅ | — | KC |

### G. Users & credentials — **FR-F1, FR-F2**
| Item | Carried | Secret? | Source | Notes |
|------|:------:|:------:|:------:|-------|
| User core (username, email, names, enabled, emailVerified) | ✅ | — | KC | `--users different_files` for scale. |
| User attributes | ✅ | maybe | KC | Scanned for secret-shaped attributes. |
| **Password credential** | ✅ | 🔑 **hash only** | KC | Stores algorithm + iterations + salt + hash (e.g. pbkdf2-sha512/argon2). No plaintext exists to carry. |
| **OTP/TOTP credentials** | ✅ | 🔑 seed | KC | Carried as user credential → 2FA keeps working. |
| **WebAuthn / passkey (FIDO2, soft token)** credentials | ✅ | 🔑 | KC | Public key + credential id + counter; usable post-restore. |
| Recovery/backup codes | ✅ | 🔑 | KC | If present as a credential type. |
| Required actions (per user) | ✅ | — | KC | |
| Realm + client role mappings | ✅ | — | KC | |
| Group memberships | ✅ | — | KC | |
| Federated identities (social account links) | ✅ | — | KC | Link to IdP preserved. |
| Service-account users | ✅ | — | KC | Tied to their client. |
| Disableable credential types | ✅ | — | KC | |
> **Federated (LDAP) users:** users that live in LDAP are *not* duplicated into the realm JSON
> unless imported/linked; the manifest states user origin (local vs federated) so operators
> know LDAP must be reachable at the destination (see H).

### H. User federation (LDAP / Kerberos) — **FR-F5**
| Item | Carried | Secret? | Source | Notes |
|------|:------:|:------:|:------:|-------|
| LDAP provider component (URL, users DN, search scope, sync policy) | ✅ | — | KC | Stored as a component. |
| **LDAP bind DN + bind credential** | ✅ | 🔑 | KC | Bind password carried in component config. |
| Kerberos config | ✅ | 🔑 maybe | KC | Keytab reference noted. |
| All LDAP mappers (attribute/group/role/full-name…) | ✅ | — | KC | Stored as child components. |
| Federation sync settings (full/changed sync periods) | ✅ | — | KC | |

### I. Identity providers (federations) — **FR-F6**
| Item | Carried | Secret? | Source | Notes |
|------|:------:|:------:|:------:|-------|
| IdP definitions (OIDC/SAML/social) | ✅ | — | KC | |
| **IdP client secret** (OIDC) | ✅ | 🔑 | KC | In realm JSON config. |
| SAML signing/encryption certs & keys | ✅ | 🔑 maybe | KC | Carried where stored in config. |
| IdP mappers | ✅ | — | KC | |

### J. Authentication — **FR-F8**
| Item | Carried | Secret? | Source | Notes |
|------|:------:|:------:|:------:|-------|
| Authentication flows + executions + subflows | ✅ | — | KC | |
| Flow bindings (browser, direct grant, registration, reset) | ✅ | — | KC | |
| Authenticator configs | ✅ | 🔑 maybe | KC | e.g. reCAPTCHA/X.509/webhook secrets carried & flagged. |
| Required actions (realm-level) | ✅ | — | KC | |
| Client policies & profiles | ✅ | — | KC | |

### K. Communication & misc
| SMTP server config | ✅ | 🔑 password | KC | SMTP password carried & flagged. |
| Client registration policies (components) | ✅ | — | KC | |

### L. Sessions — **explicitly out of scope**

| Item | Carried | Notes |
|------|:------:|-------|
| Online user sessions | ❌ | Out of scope (N5). |
| Offline sessions / offline refresh tokens | ❌ | Out of scope (N5). |
| Client sessions | ❌ | Out of scope (N5). |

Sessions live in Infinispan caches rather than in the realm representation, are cluster-topology
dependent, and do not survive being recreated on a different instance in any dependable way.
Rather than ship a best-effort feature that quietly fails in exactly the situations people rely
on, PortCloak **declares sessions out of scope**: after a restore, **users re-authenticate**.

What operators usually actually need from "session portability" is **token continuity** — that
access and refresh tokens minted before the move are still accepted afterwards. PortCloak
delivers that properly, through section **B**: the realm's **signing keys travel with the
snapshot**, so tokens signed by the old instance still verify against the new one. That is a
stronger and far more reliable guarantee than replaying session objects.

The completeness report lists sessions under `outOfScope`, never under `missing` — the
distinction matters: nothing failed, this was a design decision.

### M. External dependencies — **detected and reported, never migrated** (FR-D1)

| Item | Carried | Detected | Notes |
|------|:------:|:--------:|-------|
| Custom login/account/email/admin **themes** | ❌ | ✅ | Theme *selection* is in the realm JSON (section A); the theme **files/JAR** are not. |
| Deployed **provider / SPI JARs** | ❌ | ✅ | Custom authenticators, mappers, event listeners, storage providers. |
| Keystore / truststore **files** referenced by config | ❌ | ✅ | `java-keystore` key providers may point at a file on disk. |
| Anything else living in `providers/` or `themes/` | ❌ | ✅ | Reported by path/name. |

These are outside the realm representation by construction — they are deployment artifacts, not
realm data. Attempting to migrate them would mean shipping binaries between environments, which
PortCloak deliberately does not do (N7).

Instead, PortCloak **detects** them (via the Admin API's provider/theme information and by
inspecting the source's `providers`/`themes` directories during capture) and records them in
`dependencies.json` as **restore preconditions**:

```json
"externalDependencies": [
  { "type": "theme", "name": "acme-login",
    "detectedAt": "/opt/keycloak/themes/acme-login",
    "action": "provision manually at destination before import" },
  { "type": "provider-jar", "name": "acme-authenticator-2.1.jar",
    "detectedAt": "/opt/keycloak/providers/acme-authenticator-2.1.jar",
    "action": "provision manually at destination before import" }
]
```

The restore wizard surfaces this list **before** import (FR-D2), because a realm referencing a
missing theme or a missing authenticator SPI will import "successfully" and then fail at login —
the worst possible failure mode, and precisely the one this reporting is there to pre-empt. The
list is informative: it does not block the import.

## 7.2 Secret ledger

Every secret PortCloak carries is enumerated in a **secret ledger** so operators can audit
blast radius. Entries reference *type + location*, never value:

```json
"secrets": [
  { "type": "client-secret", "location": "clients[app-web].secret", "carried": true, "masked": false },
  { "type": "ldap-bind",     "location": "components[ldap/corp].config.bindCredential", "carried": true, "masked": false },
  { "type": "idp-secret",    "location": "identityProviders[google].config.clientSecret", "carried": true, "masked": false },
  { "type": "key-private",   "location": "components[keys/rsa-generated].config.privateKey", "carried": true, "masked": false },
  { "type": "smtp",          "location": "smtpServer.password", "carried": true, "masked": false },
  { "type": "authcfg",       "location": "authenticatorConfig[recaptcha].config.secret", "carried": true, "masked": false }
]
```

The brief's requirement — *clients with secrets not masked, because Keycloak accepts them if
supplied/enriched in the realm import* — is exactly this: PortCloak keeps secrets **unmasked**
in the encrypted payload and **records** them in the ledger, so a re-import reconstitutes a
working realm without manual secret re-entry.

## 7.3 Example manifest (abridged)

```json
{
  "schemaVersion": "1.0",
  "realm": "acme",
  "source": {
    "kind": "kubernetes",
    "keycloakVersion": "25.0.2",
    "captureMode": "offline-export",
    "executionMode": "ephemeral-clone",
    "cloneRef": "job/portcloak-01HZY3-acme",
    "secretVerification": "passed"
  },
  "counts": {
    "users": 48213, "clients": 37, "clientScopes": 12, "realmRoles": 25,
    "groups": 14, "identityProviders": 3, "ldapProviders": 1, "keyProviders": 5,
    "authFlows": 8
  },
  "keys": [
    { "kid": "abc123", "type": "RSA", "alg": "RS256", "use": "sig",
      "active": true, "privateCarried": true }
  ],
  "federation": {
    "ldap": [ { "name": "corp", "bindCarried": true, "mappers": 9 } ]
  },
  "identityProviders": [
    { "alias": "google", "protocol": "oidc", "secretCarried": true, "mappers": 2 }
  ],
  "credentials": {
    "passwordHashes": 48210, "otp": 15122, "webauthn": 8873, "recoveryCodes": 402
  },
  "secrets": [ "... see secret ledger ..." ],
  "externalDependencies": [
    { "type": "theme", "name": "acme-login",
      "action": "provision manually at destination before import" },
    { "type": "provider-jar", "name": "acme-authenticator-2.1.jar",
      "action": "provision manually at destination before import" }
  ],
  "encryption": { "enabled": true, "mode": "recipients", "recipients": 2 },
  "completeness": {
    "captured": ["settings","keys","clients","clientScopes","roles","groups",
                 "users","credentials","ldap","identityProviders","authFlows","smtp"],
    "partial":  [],
    "missing":  [],
    "outOfScope": ["onlineSessions","offlineSessions","themeFiles","providerJars"],
    "warnings": [
      "Sessions are out of scope by design; users will re-authenticate after restore. Token continuity is preserved because the active RSA signing key (kid abc123) travels with this snapshot.",
      "This realm depends on 1 custom theme and 1 provider JAR. Deploy them to the destination before importing, or logins will fail after a successful-looking import."
    ]
  }
}
```

Note the distinction the report draws: `missing` means *something we intended to carry did not
make it* (a real problem to investigate), while `outOfScope` means *we deliberately do not carry
this* (a design decision, already accounted for). Collapsing the two would make every snapshot
look partially broken.

## 7.4 How the manifest is used

- **Preview (FR-R2):** shown before restore; the operator sees counts, the secret ledger, and
  the completeness verdict, and confirms strategy (overwrite/skip/merge).
- **Dry run:** diff the snapshot's manifest against the destination realm to preview
  adds/changes before importing (FR-R2).
- **Validation:** after restore, PortCloak re-reads the target (Admin API) and checks counts and
  key KIDs against the manifest, reporting drift.
- **Audit:** the secret ledger + counts land in the audit log (NFR-5) — redacted, value-free.

## 7.5 Requirement coverage

This document is the direct fulfillment of **FR-F1..F9**, **FR-D1..D2** and **FR-M1..M3**. The
completeness report and secret ledger are what make the fidelity claims *verifiable* rather than
aspirational, and the `outOfScope` category records — without pretending otherwise — the two
things PortCloak deliberately does not move: **sessions** (N5) and **on-disk deployment assets**
such as themes and provider JARs (N7), the latter detected and reported so they never become a
post-import surprise.
