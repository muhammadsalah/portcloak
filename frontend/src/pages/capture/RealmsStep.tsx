// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Step two: which realms.
 *
 * Discovered ones are offered as a list; where discovery was not possible the
 * name is typed, and it has to match exactly because the export names its
 * output after it.
 */
import {
  Card,
  CardBody,
  Checkbox,
  Field,
  Input,
  Muted,
  Notice,
  SectionTitle,
  Small,
} from "../../design-system";
import type { CaptureDraft, UpdateDraft } from "./draft";

export function RealmsStep({
  draft,
  update,
}: {
  draft: CaptureDraft;
  update: UpdateDraft;
}) {
  return (
    <div>
      <SectionTitle style={{ marginBottom: 6 }}>Realms</SectionTitle>
      <p>
        <Muted>
          <Small>{draft.realmsNote}</Small>
        </Muted>
      </p>

      {draft.realmsDiscovered && draft.discoveredRealms.length > 0 ? (
        <Card>
          <CardBody>
            {draft.discoveredRealms.map((realm) => (
              <Checkbox
                key={realm}
                checked={draft.realms.includes(realm)}
                label={realm}
                onChange={(on) =>
                  update({
                    realms: on
                      ? [...draft.realms, realm]
                      : draft.realms.filter((r) => r !== realm),
                  })
                }
              />
            ))}
          </CardBody>
        </Card>
      ) : (
        <Field
          label="Realm name"
          hint="The export names its output after the realm, so this has to match exactly."
        >
          <Input
            type="text"
            value={draft.manualRealm}
            placeholder="acme"
            onChange={(e) => {
              const realm = e.target.value.trim();
              update({ manualRealm: realm, realms: realm ? [realm] : [] });
            }}
          />
        </Field>
      )}

      {draft.realms.length > 1 ? (
        <Notice
          tone="info"
          title={`${draft.realms.length} realms selected — that is ${draft.realms.length} snapshots`}
          body="One snapshot holds exactly one realm, so each is sealed, uploaded and reported independently. They share one execution context, and on Docker or Kubernetes one ephemeral clone. If one realm fails, the others still complete."
        />
      ) : null}
    </div>
  );
}
