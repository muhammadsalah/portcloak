// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/** Step five: every decision the other four made, in one list, before it runs. */
import type { KeyRecipient, WizardDefaults } from "../../api";
import {
  Card,
  CardBody,
  KeyValue,
  Notice,
  SectionTitle,
} from "../../design-system";
import { kindLabel, type CaptureDraft } from "./draft";

export function ReviewStep({
  defaults,
  draft,
  storedKeys,
}: {
  defaults: WizardDefaults;
  draft: CaptureDraft;
  storedKeys: KeyRecipient[];
}) {
  const environment = defaults.environments.find((e) => e.name === draft.environment);
  const storage = defaults.storages.find((s) => s.name === draft.storage);

  const rows: [string, string][] = [
    [
      "Source",
      environment
        ? `${environment.name} · ${kindLabel(environment.kind)} · ${environment.target}`
        : "—",
    ],
    ["Realms", draft.realms.join(", ") || "—"],
    ["Snapshots produced", String(draft.realms.length)],
    ["Users mode", draft.usersMode],
    ["Verify secrets", draft.verify ? "yes" : "no"],
    ["Detect dependencies", draft.detectDependencies ? "yes" : "no"],
    [
      "Encryption",
      draft.encrypt
        ? describeEncryption(draft, storedKeys)
        : "NONE — unmasked secrets in the clear",
    ],
    ["Storage", storage ? `${storage.name} · ${storage.root}` : "—"],
  ];

  return (
    <div>
      <SectionTitle>Review &amp; run</SectionTitle>

      <Card>
        <CardBody>
          <KeyValue>
            {rows.map(([label, value]) => (
              <div key={label} style={{ display: "contents" }}>
                <dt>{label}</dt>
                <dd>{value}</dd>
              </div>
            ))}
          </KeyValue>
        </CardBody>
      </Card>

      {!draft.encrypt ? (
        <Notice tone="danger" title="Unencrypted" body={defaults.declineNotice} />
      ) : null}

      <Notice
        tone="info"
        title="What this capture will not carry"
        body="Sessions are out of scope by design — users re-authenticate after a restore, and token continuity comes from the realm's signing keys travelling with the snapshot. Custom theme files and provider JARs are detected and reported, never migrated."
      />
    </div>
  );
}

/**
 * What the review step says about encryption.
 *
 * Naming the keys matters here more than counting them: "2 recipient(s)" tells
 * an operator nothing about whether they will be able to open this snapshot
 * afterwards, and that is the only question the review step is for.
 */
function describeEncryption(draft: CaptureDraft, storedKeys: KeyRecipient[]): string {
  if (draft.encryptionMode === "passphrase") {
    return draft.rememberPassphraseAs
      ? `passphrase, remembered as “${draft.rememberPassphraseAs}”`
      : "passphrase, not stored anywhere";
  }
  const named = storedKeys
    .filter((key) => draft.recipients.includes(key.publicKey))
    .map((key) => key.name);
  const pasted = draft.recipients.length - named.length;
  const parts = [...named];
  if (pasted > 0) parts.push(`${pasted} pasted key(s)`);
  return parts.length > 0 ? `sealed to ${parts.join(", ")}` : "no recipients chosen";
}
