// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/** Step four: where the bundle goes. */
import type { WizardDefaults } from "@/api";
import { Field, Notice, SectionTitle, Select } from "@/design-system";
import type { CaptureDraft, UpdateDraft } from "./draft";

export function StorageStep({
  defaults,
  draft,
  update,
}: {
  defaults: WizardDefaults;
  draft: CaptureDraft;
  update: UpdateDraft;
}) {
  const chosen = defaults.storages.find((storage) => storage.name === draft.storage);

  return (
    <div>
      <SectionTitle>Storage sink</SectionTitle>

      <Field
        label="Storage"
        hint="A capture writes to exactly one storage. The bundle is checksummed before it reaches any of them, so corruption is caught on retrieval whichever you choose."
      >
        <Select
          value={draft.storage}
          onChange={(storage) => update({ storage })}
          options={defaults.storages.map((storage) => ({
            value: storage.name,
            label: `${storage.name} · ${storage.kind} · ${storage.root}${storage.default ? " (default)" : ""}`,
          }))}
        />
      </Field>

      {chosen?.encryptionRequired && !draft.encrypt ? (
        <Notice
          tone="danger"
          title={`${chosen.name} requires encryption`}
          body="A snapshot cannot be written there unencrypted. Turn encryption back on, or choose a different storage."
        />
      ) : null}

      {chosen && !chosen.credentialPresent ? (
        <Notice
          tone="warn"
          title="This storage has no credential on this machine"
          body="Configuration is portable between machines; the secrets deliberately are not. Enter it again on the Storage screen."
        />
      ) : null}
    </div>
  );
}
