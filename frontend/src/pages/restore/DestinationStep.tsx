// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/** Step two: where it goes, and the key that opens it on the way. */
import { useState } from "react";

import type { EnvironmentView, LibraryEntry } from "../../api";
import { SnapshotKeyFields, type SnapshotKey } from "../../components/SnapshotKeyFields";
import {
  Badge,
  Card,
  CardBody,
  CardHead,
  CardTitle,
  FieldBox,
  FieldHint,
  Label,
  Link,
  Notice,
  Row,
  Select,
  Spinner,
  Strong,
} from "../../design-system";

export function DestinationStep({
  snapshot,
  destinations,
  environment,
  onEnvironment,
  planning,
  storedKeys,
  keyNote,
  unlockedWith,
  keyValue,
  onKey,
}: {
  snapshot?: LibraryEntry;
  destinations: EnvironmentView[];
  environment: string;
  onEnvironment: (name: string) => void;
  planning: boolean;
  storedKeys: number;
  keyNote: string;
  unlockedWith: string;
  keyValue: SnapshotKey;
  onKey: (key: SnapshotKey) => void;
}) {
  if (planning) return <Spinner>Downloading, decrypting and verifying the snapshot…</Spinner>;

  return (
    <Card>
      <CardHead>
        <CardTitle>Destination environment</CardTitle>
      </CardHead>
      <CardBody>
        <FieldBox>
          <Label>Environment</Label>
          <Select value={environment} onChange={(e) => onEnvironment(e.target.value)}>
            {destinations.map((destination) => (
              <option key={destination.name} value={destination.name}>
                {`${destination.name} — ${destination.kind} · ${destination.target}`}
              </option>
            ))}
          </Select>
          <FieldHint>
            Keep capture and restore environments separate where you can: a restore target carries
            the higher privilege, and defining it as its own entry means that credential is only
            present where a restore is actually intended.
          </FieldHint>
        </FieldBox>

        {snapshot?.encrypted ? (
          <KeySection
            encryptionMode={snapshot.encryptionMode}
            storedKeys={storedKeys}
            keyNote={keyNote}
            unlockedWith={unlockedWith}
            keyValue={keyValue}
            onKey={onKey}
          />
        ) : null}

        <Notice
          tone="info"
          title="PortCloak verifies the snapshot before contacting this environment"
          body="Integrity and decryption are checked first. A snapshot that cannot be proven intact is never written to a target."
        />
      </CardBody>
    </Card>
  );
}

/**
 * The key.
 *
 * It is asked for here, beside the notice that promises decryption runs before
 * the destination is contacted — because that promise is what needs it. It sits
 * on the step rather than in a modal so that a key which turns out to be wrong
 * can be corrected next to the message saying so; the failure from pressing
 * Next lands on this same screen.
 *
 * Where this machine already holds keys, the field stops being a gate.
 * PortCloak tries what it holds and says which one worked — silent would be the
 * one thing worse than the prompt it replaces — and the field stays as an
 * override for the snapshot sealed with something else.
 */
function KeySection({
  encryptionMode,
  storedKeys,
  keyNote,
  unlockedWith,
  keyValue,
  onKey,
}: {
  encryptionMode?: string;
  storedKeys: number;
  keyNote: string;
  unlockedWith: string;
  keyValue: SnapshotKey;
  onKey: (key: SnapshotKey) => void;
}) {
  const [showOverride, setShowOverride] = useState(false);

  const fields = (
    <div>
      <SnapshotKeyFields value={keyValue} onChange={onKey} />
      <FieldHint>
        {encryptionMode === "recipients"
          ? "This snapshot was sealed to age recipients, so it takes the private key of one of them."
          : "PortCloak did not store this key when the snapshot was written unless it was told to. Without it the bundle stays sealed."}
      </FieldHint>
    </div>
  );

  const head = (
    <Row style={{ marginBottom: 6 }}>
      <Strong>Decryption key</Strong>
      <Badge $tone="neutral">Encrypted</Badge>
    </Row>
  );

  if (storedKeys === 0) {
    return (
      <FieldBox>
        {head}
        {fields}
      </FieldBox>
    );
  }

  // With keys on this machine, the field is folded away behind the statement of
  // what will be tried.
  return (
    <FieldBox>
      {head}
      {unlockedWith ? (
        <Notice
          tone="ok"
          title={`Opened with the stored key “${unlockedWith}”`}
          body="PortCloak held this key already, so it was not asked for. The audit log records that it was used."
        />
      ) : (
        <Notice
          tone="info"
          title="No key is needed here if one PortCloak already holds opens this snapshot"
          body={`${keyNote} Whichever one does is named before anything is written to a destination.`}
        />
      )}
      <Link onClick={() => setShowOverride((shown) => !shown)}>Use a different key</Link>
      {showOverride ? fields : null}
    </FieldBox>
  );
}
