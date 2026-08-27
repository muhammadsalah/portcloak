/**
 * Storage: where snapshots live.
 *
 * The same list-beside-editor shape as Environments, plus one extra mode — the
 * browser, which shows what a backend really holds rather than what PortCloak
 * expects to find there.
 */
import { useState } from "react";
import styled from "styled-components";

import { ConfigAPI, type ConfigSnapshot, type Storage, type StorageView } from "../../api";
import { useNavigate } from "../../app/ShellContext";
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardHead,
  CardTitle,
  Muted,
  PageHead,
  PageSubtitle,
  PageTitle,
  Row,
  Small,
  Spinner,
  Split,
  Truncate,
} from "../../design-system";
import { useAsync } from "../../hooks/useAsync";
import { StorageBrowser } from "./StorageBrowser";
import { StorageEditor } from "./StorageEditor";
import { kindLabel } from "./kinds";

export function StoragePage({ select, browsing }: { select?: string; browsing?: boolean }) {
  const { state } = useAsync(() => ConfigAPI.load(), []);

  if (state.status === "failed") throw state.error;
  if (state.status === "loading") return <Spinner>Loading configuration…</Spinner>;

  return <StorageScreen snapshot={state.value} select={select} browsing={Boolean(browsing)} />;
}

function StorageScreen({
  snapshot,
  select,
  browsing,
}: {
  snapshot: ConfigSnapshot;
  select?: string;
  browsing: boolean;
}) {
  const navigate = useNavigate();

  // A name that is no longer there — a deleted storage still in a route — is
  // nobody's selection rather than an editor full of undefined.
  const initial = snapshot.storage.find((s) => s.name === (select ?? snapshot.storage[0]?.name));
  const [selected, setSelected] = useState<string | undefined>(initial?.name);
  const [draft, setDraft] = useState<Storage | undefined>(initial ? { ...initial } : undefined);
  const [originalName, setOriginalName] = useState(initial?.name ?? "");
  // Bumped whenever a different storage is picked, so the editor remounts and
  // drops the probe result and typed secret belonging to the last one.
  const [editorKey, setEditorKey] = useState(0);

  const edit = (storage: StorageView | null) => {
    setSelected(storage?.name);
    setDraft(storage ? { ...storage } : { name: "", kind: "disk" });
    setOriginalName(storage?.name ?? "");
    setEditorKey((n) => n + 1);
  };

  const head = (
    <PageHead>
      <div>
        <PageTitle>{browsing ? "Storage browser" : "Storage"}</PageTitle>
        <PageSubtitle>
          {browsing
            ? "What this storage really holds, including objects PortCloak did not write."
            : "Where snapshots live. Every kind is rooted at a folder or prefix, so one backend can hold several independent trees."}
        </PageSubtitle>
      </div>
      {browsing ? (
        <Button onClick={() => navigate({ name: "storage", select: selected })}>
          Back to storage
        </Button>
      ) : (
        <Button $variant="primary" onClick={() => edit(null)}>
          Add storage
        </Button>
      )}
    </PageHead>
  );

  if (browsing) {
    return (
      <div>
        {head}
        {selected ? <StorageBrowser storage={selected} /> : null}
      </div>
    );
  }

  if (snapshot.storage.length === 0 && !draft) {
    return (
      <div>
        {head}
        <NothingYet snapshot={snapshot} />
      </div>
    );
  }

  return (
    <div>
      {head}
      <Split>
        <StorageList storages={snapshot.storage} selected={selected} onSelect={edit} />
        {draft ? (
          <StorageEditor
            key={editorKey}
            snapshot={snapshot}
            initialDraft={draft}
            originalName={originalName}
            onSaved={(name) => navigate({ name: "storage", select: name })}
            onDeleted={() => navigate({ name: "storage" })}
          />
        ) : (
          <Placeholder />
        )}
      </Split>
    </div>
  );
}

/**
 * The first launch. What a storage is, and the one property of it that decides
 * everything else: PortCloak roots every kind at a folder or prefix, so a
 * bucket can hold several independent trees and deleting a definition here
 * never touches what is already stored in it.
 */
function NothingYet({ snapshot }: { snapshot: ConfigSnapshot }) {
  return (
    <Card>
      <CardBody>
        <CardTitle>No storage yet</CardTitle>
        <p>
          A storage is where snapshots are written — a folder on disk, a folder on a host over SSH,
          an S3 bucket, or an Azure Blob container. Mark one as requiring encryption and nothing
          plaintext will ever be written to it.
        </p>
        <p>
          <Muted>
            <Small>
              {`Add one with the button above, or write it into ${snapshot.configFile} by hand — the file is the same one this screen edits. Credentials never go in it: each secret goes to this machine's keychain and only a handle is written to the file.`}
            </Small>
          </Muted>
        </p>
      </CardBody>
    </Card>
  );
}

function Placeholder() {
  return (
    <Card>
      <CardBody $muted>Select a storage on the left, or add one.</CardBody>
    </Card>
  );
}

function StorageList({
  storages,
  selected,
  onSelect,
}: {
  storages: StorageView[];
  selected?: string;
  onSelect: (storage: StorageView) => void;
}) {
  return (
    <Card>
      <CardHead>
        <Muted>
          <Small>
            {`${storages.length} storage definition${storages.length === 1 ? "" : "s"}`}
          </Small>
        </Muted>
      </CardHead>
      {storages.map((storage) => (
        <ListRow
          key={storage.name}
          $selected={selected === storage.name}
          onClick={() => onSelect(storage)}
        >
          <Row>
            <span style={{ fontWeight: 500 }}>{storage.name}</span>
            {storage.default ? <Badge $tone="info">default</Badge> : null}
            {storage.encryptionRequired ? <Badge $tone="ok">encryption required</Badge> : null}
          </Row>
          <Truncate title={`${kindLabel(storage.kind)} · ${storage.root}`}>
            <Muted>
              <Small>{`${kindLabel(storage.kind)} · ${storage.root}`}</Small>
            </Muted>
          </Truncate>
          <ProbeLine storage={storage} />
        </ListRow>
      ))}
    </Card>
  );
}

function ProbeLine({ storage }: { storage: StorageView }) {
  if (!storage.lastProbe) return <NeverTested>Never tested</NeverTested>;

  const writable = storage.lastProbe.writable !== false;
  const bad = storage.stale || !storage.lastProbe.ok;
  const detail = storage.lastProbe.ok
    ? writable
      ? "writable"
      : "read-only"
    : "not reachable";

  return (
    <ProbeText $bad={bad}>
      {`Tested ${storage.probeAge} · ${detail}${storage.stale ? " — stale" : ""}`}
    </ProbeText>
  );
}

const ListRow = styled.div<{ $selected: boolean }>`
  padding: 12px 16px;
  cursor: pointer;
  border-left: 3px solid ${(p) => (p.$selected ? p.theme.color.primary : "transparent")};
  background: ${(p) => (p.$selected ? p.theme.color.primarySoft : "transparent")};
  border-bottom: 1px solid ${(p) => p.theme.color.borderSubtle};
`;

const NeverTested = styled.div`
  font-size: 12px;
  color: ${(p) => p.theme.color.textMuted};
`;

const ProbeText = styled.div<{ $bad: boolean }>`
  font-size: 12px;
  color: ${(p) => (p.$bad ? p.theme.color.danger : p.theme.color.success)};
`;
