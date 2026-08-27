// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * How PortCloak asks for a snapshot's decryption key.
 *
 * Two screens need it — reading inside a snapshot, and restoring one — and they
 * ask in different containers: the inspector in a modal on its way in, the
 * restore wizard as a field on the step that says decryption happens first. The
 * container differs; the question must not. A snapshot is opened by a
 * passphrase or by an age identity, and an operator who learns the shape of
 * that question on one screen should recognise it on the other.
 */
import { Label, Input, Textarea } from "../design-system";

/** A key as the engine takes it: one passphrase, or one or more identities. */
export interface SnapshotKey {
  passphrase: string;
  identities: string[];
}

export function noKey(): SnapshotKey {
  return { passphrase: "", identities: [] };
}

export function hasKey(key: SnapshotKey): boolean {
  return key.passphrase !== "" || key.identities.length > 0;
}

export function SnapshotKeyFields({
  value,
  onChange,
}: {
  value: SnapshotKey;
  onChange: (key: SnapshotKey) => void;
}) {
  return (
    <div>
      <Label>Passphrase</Label>
      <Input
        type="password"
        placeholder="The passphrase this snapshot was sealed with"
        value={value.passphrase}
        onChange={(e) => onChange({ ...value, passphrase: e.target.value })}
      />

      <Label style={{ marginTop: 12 }}>…or an age private key</Label>
      <Textarea
        rows={3}
        placeholder="AGE-SECRET-KEY-1…"
        value={value.identities[0] ?? ""}
        onChange={(e) => {
          const identity = e.target.value.trim();
          onChange({ ...value, identities: identity ? [identity] : [] });
        }}
      />
    </div>
  );
}
