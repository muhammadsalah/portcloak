/**
 * The storage form: the kind along the top, the fields that kind needs, the two
 * switches that change what may be written here, and the reachability probe.
 */
import { useState } from "react";

import {
  ConfigAPI,
  type ConfigSnapshot,
  type Storage,
  type StorageProbeResult,
} from "../../api";
import { useNavigate } from "../../app/ShellContext";
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardFoot,
  CardHead,
  FailureNotice,
  Field,
  FieldBox,
  FieldHint,
  FieldRow,
  Input,
  KeyValue,
  Link,
  Muted,
  Notice,
  NoticeBox,
  NoticeTitle,
  Right,
  Row,
  Select,
  Small,
  Spinner,
  Strong,
  Tabs,
  Toggle,
  useModal,
} from "../../design-system";
import { bytes } from "../../utils/format";
import { credentialLabel, kindLabel, kinds } from "./kinds";

export function StorageEditor({
  snapshot,
  initialDraft,
  originalName,
  onSaved,
  onDeleted,
}: {
  snapshot: ConfigSnapshot;
  initialDraft: Storage;
  originalName: string;
  onSaved: (name: string) => void;
  onDeleted: () => void;
}) {
  const navigate = useNavigate();
  const modal = useModal();

  const [draft, setDraft] = useState<Storage>(initialDraft);
  const [secret, setSecret] = useState("");
  const [probe, setProbe] = useState<StorageProbeResult | undefined>();
  const [probing, setProbing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | undefined>();

  const set = <K extends keyof Storage>(key: K, value: Storage[K]) =>
    setDraft((previous) => ({ ...previous, [key]: value }));

  const selected = snapshot.storage.find((s) => s.name === originalName);

  const save = async () => {
    setSaving(true);
    setError(undefined);
    const failure = await ConfigAPI.saveStorage(originalName, draft, secret);
    setSaving(false);
    if (failure) {
      setError(failure.message);
      return;
    }
    onSaved(draft.name);
  };

  const test = async () => {
    setProbing(true);
    setProbe(await ConfigAPI.testStorage(originalName));
    setProbing(false);
  };

  return (
    <Card>
      <CardHead>
        <Row>
          <Strong>{draft.name || "New storage"}</Strong>
          <Badge $tone="info">{kindLabel(draft.kind)}</Badge>
        </Row>
      </CardHead>

      <CardBody>
        <Tabs
          items={kinds.map((kind) => ({ key: kind.value, label: kind.label }))}
          active={draft.kind}
          onSelect={(kind) => {
            set("kind", kind);
            setProbe(undefined);
          }}
        />

        {error ? <Notice tone="danger" title="Not saved" body={error} /> : null}

        {selected && !selected.readiness.ready ? (
          <Notice tone="warn" title="Not usable yet" body={selected.readiness.reason ?? ""} />
        ) : null}

        {selected && !selected.credentialPresent ? (
          <Notice
            tone="warn"
            title="This storage's credential is not in this machine's keychain"
            body="Configuration is portable between machines; the secrets deliberately are not."
          />
        ) : null}

        <Field label="Name">
          <Input type="text" value={draft.name} onChange={(e) => set("name", e.target.value)} />
        </Field>

        <KindFields draft={draft} set={set} />

        {draft.kind !== "disk" && !(draft.kind === "ssh" && draft.auth === "agent") ? (
          <Field
            label={credentialLabel(draft.kind)}
            hint={
              draft.credentialRef
                ? `Stored in this machine's keychain as ${draft.credentialRef}. config.yaml holds only the handle.`
                : "Stored in this machine's keychain; config.yaml will hold only a handle."
            }
          >
            <Input
              type="password"
              value={secret}
              placeholder={
                draft.credentialRef ? "•••••••• (stored)" : credentialLabel(draft.kind)
              }
              onChange={(e) => setSecret(e.target.value)}
            />
          </Field>
        ) : null}

        <SwitchField
          on={Boolean(draft.default)}
          onChange={(value) => set("default", value)}
          label="Default for new captures"
          hint="Exactly one storage can be the default. Setting this clears the others."
        />
        <SwitchField
          on={Boolean(draft.encryptionRequired)}
          onChange={(value) => set("encryptionRequired", value)}
          label="Encryption required"
          hint="Removes the opt-out for anything written here. A snapshot cannot be written to this storage unencrypted, and the engine enforces it — a hand-edited config cannot bypass it."
        />
      </CardBody>

      <CardBody $divided>
        <Row>
          <Strong>Test storage</Strong>
          <Right>
            <Button disabled={!originalName || probing} onClick={() => void test()}>
              {probing ? "Testing…" : "Test storage"}
            </Button>
          </Right>
        </Row>
        {!originalName ? (
          <Muted>
            <Small>Save the storage first, then test it.</Small>
          </Muted>
        ) : null}
        {probing ? (
          <Spinner>Listing, writing a probe object, verifying, removing it…</Spinner>
        ) : null}
        {probe?.failure ? <FailureNotice failure={probe.failure} /> : null}
        {probe && !probe.failure ? (
          <ReachPanel
            probe={probe}
            draft={draft}
            onCreateFolder={async () => {
              await ConfigAPI.createStorageFolder(originalName);
              setProbe(await ConfigAPI.testStorage(originalName));
            }}
          />
        ) : null}
      </CardBody>

      <CardFoot>
        <Row>
          {originalName ? (
            <Link onClick={() => navigate({ name: "browse", storage: originalName })}>Browse</Link>
          ) : null}
          {originalName ? (
            <Link
              onClick={async () => {
                await ConfigAPI.duplicateStorage(originalName);
                navigate({ name: "storage" });
              }}
            >
              Duplicate
            </Link>
          ) : null}
          {originalName ? (
            <Link $tone="danger" onClick={() => confirmDelete(originalName, modal, onDeleted)}>
              Delete
            </Link>
          ) : null}
        </Row>
        <Row>
          <Button onClick={() => navigate({ name: "storage" })}>Cancel</Button>
          <Button $variant="primary" disabled={saving || !draft.name} onClick={() => void save()}>
            {saving ? "Saving…" : "Save"}
          </Button>
        </Row>
      </CardFoot>
    </Card>
  );
}

type Setter = <K extends keyof Storage>(key: K, value: Storage[K]) => void;

function KindFields({ draft, set }: { draft: Storage; set: Setter }) {
  switch (draft.kind) {
    case "disk":
      return (
        <Field
          label="Root folder"
          hint="The tree under here is browsable: an operator who lost the app can still find and identify a snapshot with ls."
        >
          <Input
            type="text"
            value={draft.folder ?? ""}
            placeholder="~/PortCloak/snapshots"
            onChange={(e) => set("folder", e.target.value)}
          />
        </Field>
      );

    case "ssh":
      return (
        <>
          <FieldRow>
            <Field label="Host">
              <Input
                type="text"
                value={draft.host ?? ""}
                onChange={(e) => set("host", e.target.value)}
              />
            </Field>
            <Field label="Port">
              <Input
                type="number"
                value={String(draft.port ?? 22)}
                onChange={(e) => set("port", Number(e.target.value) || 22)}
              />
            </Field>
          </FieldRow>
          <FieldRow>
            <Field label="User">
              <Input
                type="text"
                value={draft.user ?? ""}
                onChange={(e) => set("user", e.target.value)}
              />
            </Field>
            <Field label="Authentication">
              <Select
                value={draft.auth ?? "key"}
                onChange={(e) => set("auth", e.target.value as Storage["auth"])}
              >
                <option value="key">Private key</option>
                <option value="agent">SSH agent</option>
                <option value="password">Password</option>
              </Select>
            </Field>
          </FieldRow>
          <Field label="Remote folder">
            <Input
              type="text"
              value={draft.folder ?? ""}
              onChange={(e) => set("folder", e.target.value)}
            />
          </Field>
        </>
      );

    case "s3":
      return (
        <>
          <FieldRow>
            <Field label="Endpoint" hint="Point this at MinIO to use the same code path.">
              <Input
                type="text"
                value={draft.endpoint ?? ""}
                placeholder="s3.eu-west-1.amazonaws.com"
                onChange={(e) => set("endpoint", e.target.value)}
              />
            </Field>
            <Field label="Region">
              <Input
                type="text"
                value={draft.region ?? ""}
                placeholder="eu-west-1"
                onChange={(e) => set("region", e.target.value)}
              />
            </Field>
          </FieldRow>
          <FieldRow>
            <Field label="Bucket">
              <Input
                type="text"
                value={draft.bucket ?? ""}
                onChange={(e) => set("bucket", e.target.value)}
              />
            </Field>
            <Field
              label="Prefix (folder)"
              hint="One bucket can hold several independent snapshot trees."
            >
              <Input
                type="text"
                value={draft.prefix ?? ""}
                placeholder="portcloak/"
                onChange={(e) => set("prefix", e.target.value)}
              />
            </Field>
          </FieldRow>
          <FieldRow>
            <Field label="Part size (MB)" hint="Smaller parts resume faster on a flaky link.">
              <Input
                type="number"
                value={String(draft.partSizeMb ?? 8)}
                onChange={(e) => set("partSizeMb", Number(e.target.value) || 8)}
              />
            </Field>
            <SwitchField
              on={Boolean(draft.pathStyle)}
              onChange={(value) => set("pathStyle", value)}
              label="Path-style addressing"
              inlineHint="Required by MinIO and most S3-compatible stores."
            />
          </FieldRow>
        </>
      );

    case "azure":
      return (
        <>
          <FieldRow>
            <Field label="Account">
              <Input
                type="text"
                value={draft.account ?? ""}
                onChange={(e) => set("account", e.target.value)}
              />
            </Field>
            <Field
              label="Endpoint (optional)"
              hint="Point this at Azurite's dev endpoint to use the emulator."
            >
              <Input
                type="text"
                value={draft.endpoint ?? ""}
                placeholder="http://127.0.0.1:10000/devstoreaccount1"
                onChange={(e) => set("endpoint", e.target.value)}
              />
            </Field>
          </FieldRow>
          <FieldRow>
            <Field label="Container">
              <Input
                type="text"
                value={draft.container ?? ""}
                onChange={(e) => set("container", e.target.value)}
              />
            </Field>
            <Field label="Prefix (folder)">
              <Input
                type="text"
                value={draft.prefix ?? ""}
                onChange={(e) => set("prefix", e.target.value)}
              />
            </Field>
          </FieldRow>
        </>
      );
  }
}

/** A switch with its name beside it, and what turning it on means underneath. */
function SwitchField({
  on,
  onChange,
  label,
  hint,
  inlineHint,
}: {
  on: boolean;
  onChange: (value: boolean) => void;
  label: string;
  hint?: string;
  inlineHint?: string;
}) {
  return (
    <FieldBox>
      {inlineHint ? (
        <>
          <label>{label}</label>
          <Row>
            <Toggle on={on} onChange={onChange} />
            <Muted>
              <Small>{inlineHint}</Small>
            </Muted>
          </Row>
        </>
      ) : (
        <Row>
          <Toggle on={on} onChange={onChange} />
          <div>
            <div>{label}</div>
            <FieldHint>{hint}</FieldHint>
          </div>
        </Row>
      )}
    </FieldBox>
  );
}

/** The three-way result is described, never collapsed into pass or fail. */
function ReachPanel({
  probe,
  draft,
  onCreateFolder,
}: {
  probe: StorageProbeResult;
  draft: Storage;
  onCreateFolder: () => Promise<void>;
}) {
  const reach = probe.reach;
  const tone =
    reach.access === "writable" ? "ok" : reach.access === "read-only" ? "warn" : "danger";

  const rows: [string, string][] = [
    ["Root", reach.root],
    ["Access", reach.access],
    ["Integrity", reach.integrity || "—"],
    ["Resumable upload", reach.resumable ? "yes" : "no"],
  ];
  if (reach.latency) rows.push(["Round trip", `${Math.round(reach.latency / 1e6)} ms`]);
  if (reach.freeBytes) rows.push(["Free space", bytes(reach.freeBytes)]);
  if (reach.failedStep) rows.push(["Failed at", reach.failedStep]);

  // A disk folder that does not exist yet is offered rather than rejected.
  const offerFolder =
    reach.access === "unreachable" &&
    draft.kind === "disk" &&
    Boolean(reach.detail?.includes("does not exist"));

  return (
    <NoticeBox $tone={tone}>
      <NoticeTitle>{probe.note}</NoticeTitle>
      <KeyValue style={{ marginTop: 10 }}>
        {rows.map(([label, value]) => (
          <div key={label} style={{ display: "contents" }}>
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </KeyValue>
      {offerFolder ? (
        <Button style={{ marginTop: 10 }} onClick={() => void onCreateFolder()}>
          Create the folder
        </Button>
      ) : null}
    </NoticeBox>
  );
}

function confirmDelete(
  name: string,
  modal: ReturnType<typeof useModal>,
  onDeleted: () => void,
): void {
  modal.open({
    title: `Delete the storage “${name}”?`,
    body: (
      <div>
        <p>The definition and its keychain secret are removed from this machine.</p>
        <p>
          <Muted>
            <Small>
              Stored snapshot files are not deleted. Removing a storage definition forgets how to
              reach the data; it does not destroy it.
            </Small>
          </Muted>
        </p>
      </div>
    ),
    confirmLabel: "Delete storage",
    confirmTone: "danger-solid",
    onConfirm: async () => {
      const failure = await ConfigAPI.deleteStorage(name);
      if (failure) {
        modal.open({ title: "Not deleted", body: <div>{failure.message}</div> });
        return;
      }
      onDeleted();
    },
  });
}
