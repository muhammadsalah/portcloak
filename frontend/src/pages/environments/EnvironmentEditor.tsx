// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The environment form.
 *
 * One card: the kind along the top, the fields that kind needs, the optional
 * Admin API block, then the probe and the footer. Everything typed lives in
 * this component, which is what lets a rejected save leave the draft untouched
 * — see the note on `save`.
 */
import { useState } from "react";

import {
  ConfigAPI,
  type ConfigSnapshot,
  type Environment,
  type ProbeResult,
} from "../../api";
import { useNavigate } from "../../app/ShellContext";
import { ProbePanel } from "../../components/ProbePanel";
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardFoot,
  CardHead,
  Checkbox,
  FailureNotice,
  Field,
  FieldHint,
  FieldRow,
  Input,
  Link,
  Muted,
  Notice,
  Right,
  Row,
  Select,
  Small,
  Spinner,
  Strong,
  Tabs,
  useModal,
} from "../../design-system";
import { kindLabel, kinds } from "./kinds";

export function EnvironmentEditor({
  snapshot,
  initialDraft,
  originalName,
  onSaved,
  onDeleted,
}: {
  snapshot: ConfigSnapshot;
  initialDraft: Environment;
  originalName: string;
  onSaved: (name: string) => void;
  onDeleted: () => void;
}) {
  const navigate = useNavigate();
  const modal = useModal();

  const [draft, setDraft] = useState<Environment>(initialDraft);
  const [secret, setSecret] = useState("");
  const [adminSecret, setAdminSecret] = useState("");
  const [probe, setProbe] = useState<ProbeResult | undefined>();
  const [probing, setProbing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | undefined>();

  const set = <K extends keyof Environment>(key: K, value: Environment[K]) =>
    setDraft((previous) => ({ ...previous, [key]: value }));

  const selected = snapshot.environments.find((e) => e.name === originalName);

  /**
   * A save that the engine refuses has to leave the form exactly as it was,
   * with the reason on top of it.
   *
   * The failing path used to re-enter the whole screen, which builds a fresh
   * state from the configuration on disk. That threw away two things at once:
   * the message, because it had just been written to the state object being
   * replaced, and the operator's draft, because the file is the version without
   * their edits. A rejected workload reference therefore looked like a Save
   * button that did nothing and then blanked the form — with the sentence
   * naming the field and the fix already computed, and discarded.
   *
   * Only a save that succeeded re-reads the configuration. A failure leaves
   * this component mounted with everything still in it.
   */
  const save = async () => {
    setSaving(true);
    setError(undefined);

    const failure = await ConfigAPI.saveEnvironment(originalName, draft, secret);
    setSaving(false);
    if (failure) {
      setError(failure.message);
      return;
    }
    if (adminSecret) {
      await ConfigAPI.saveAdminCredential(draft.name, adminSecret);
    }
    onSaved(draft.name);
  };

  return (
    <Card>
      <CardHead>
        <Row>
          <Strong>{draft.name || "New environment"}</Strong>
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
            title="This environment's credential is not in this machine's keychain"
            body="Configuration is portable between machines; the secrets deliberately are not. Enter it again below."
          />
        ) : null}

        <Field label="Name" hint="How PortCloak refers to this environment everywhere.">
          <Input type="text" value={draft.name} onChange={(e) => set("name", e.target.value)} />
        </Field>

        <KindFields draft={draft} set={set} secret={secret} setSecret={setSecret} />

        <AdminSection
          draft={draft}
          set={set}
          adminSecret={adminSecret}
          setAdminSecret={setAdminSecret}
        />
      </CardBody>

      <CardBody $divided>
        <Row>
          <Strong>Test connection</Strong>
          <Right>
            <Button
              disabled={!originalName || probing}
              onClick={async () => {
                setProbing(true);
                setProbe(await ConfigAPI.testEnvironment(originalName));
                setProbing(false);
              }}
            >
              {probing ? "Testing…" : "Test connection"}
            </Button>
          </Right>
        </Row>
        {!originalName ? (
          <Muted>
            <Small>Save the environment first, then test it.</Small>
          </Muted>
        ) : null}
        {probing ? <Spinner>Probing…</Spinner> : null}
        {probe?.failure ? <FailureNotice failure={probe.failure} /> : null}
        {probe && !probe.failure ? <ProbePanel facts={probe.facts} ok={probe.ok} /> : null}
      </CardBody>

      <CardFoot>
        <Row>
          {originalName ? (
            <Link
              onClick={async () => {
                await ConfigAPI.duplicateEnvironment(originalName);
                navigate({ name: "environments" });
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
          <Button onClick={() => navigate({ name: "environments" })}>Cancel</Button>
          <Button $variant="primary" disabled={saving || !draft.name} onClick={() => void save()}>
            {saving ? "Saving…" : "Save"}
          </Button>
        </Row>
      </CardFoot>
    </Card>
  );
}

type Setter = <K extends keyof Environment>(key: K, value: Environment[K]) => void;

/** The fields that only one kind of environment has. */
function KindFields({
  draft,
  set,
  secret,
  setSecret,
}: {
  draft: Environment;
  set: Setter;
  secret: string;
  setSecret: (value: string) => void;
}) {
  switch (draft.kind) {
    case "local":
      return (
        <>
          <Field
            label="Keycloak server folder"
            hint="The install root — the folder containing bin/kc.sh, not the bin folder itself."
          >
            <Input
              type="text"
              value={draft.serverFolder ?? ""}
              placeholder="/opt/keycloak"
              onChange={(e) => set("serverFolder", e.target.value)}
            />
          </Field>
          <Field label="Java home (optional)">
            <Input
              type="text"
              value={draft.javaHome ?? ""}
              onChange={(e) => set("javaHome", e.target.value)}
            />
          </Field>
        </>
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
                onChange={(e) => set("auth", e.target.value as Environment["auth"])}
              >
                <option value="key">Private key</option>
                <option value="agent">SSH agent</option>
                <option value="password">Password</option>
              </Select>
            </Field>
          </FieldRow>
          <Field label="Keycloak server folder on the host">
            <Input
              type="text"
              value={draft.serverFolder ?? ""}
              placeholder="/opt/keycloak"
              onChange={(e) => set("serverFolder", e.target.value)}
            />
          </Field>
          {draft.auth === "agent" ? (
            <FieldHint style={{ marginBottom: 16 }}>
              Agent authentication stores no secret at all — PortCloak asks the running agent when
              it connects.
            </FieldHint>
          ) : (
            <CredentialField
              label={draft.auth === "password" ? "Password" : "Private key"}
              credentialRef={draft.credentialRef}
              value={secret}
              onChange={setSecret}
            />
          )}
        </>
      );

    case "docker":
      return (
        <>
          <Field
            label="Docker endpoint"
            hint="Leave it empty for DOCKER_HOST or the local socket. Fill it in for a specific socket, a TLS endpoint, or Docker over SSH."
          >
            <Input
              type="text"
              value={draft.dockerEndpoint ?? ""}
              placeholder="unix:///var/run/docker.sock"
              onChange={(e) => set("dockerEndpoint", e.target.value)}
            />
          </Field>
          <Field
            label="Service or container running Keycloak"
            hint="PortCloak inspects this container read-only and runs the export inside a throwaway clone of it."
          >
            <Input
              type="text"
              value={draft.container ?? ""}
              placeholder="keycloak"
              onChange={(e) => set("container", e.target.value)}
            />
          </Field>
          <KcPathField draft={draft} set={set} />
        </>
      );

    case "kubernetes":
      return (
        <>
          <FieldRow>
            <Field label="Kubeconfig context">
              <Input
                type="text"
                value={draft.context ?? ""}
                placeholder="prod-cluster"
                onChange={(e) => set("context", e.target.value)}
              />
            </Field>
            <Field
              label="Kubeconfig file (optional)"
              hint="Only needed when the cluster is not in the default kubeconfig."
            >
              <Input
                type="text"
                value={draft.kubeconfig ?? ""}
                placeholder="~/.kube/config"
                onChange={(e) => set("kubeconfig", e.target.value)}
              />
            </Field>
          </FieldRow>
          <FieldRow>
            <Field label="Namespace">
              <Input
                type="text"
                value={draft.namespace ?? ""}
                placeholder="iam-prod"
                onChange={(e) => set("namespace", e.target.value)}
              />
            </Field>
            <Field label="Deployment or StatefulSet">
              <Input
                type="text"
                value={draft.workload ?? ""}
                placeholder="statefulset/keycloak"
                onChange={(e) => set("workload", e.target.value)}
              />
            </Field>
          </FieldRow>
          <Field
            label="Container name (optional)"
            hint="Only needed when the pod runs more than one container."
          >
            <Input
              type="text"
              value={draft.containerName ?? ""}
              onChange={(e) => set("containerName", e.target.value)}
            />
          </Field>
          <KcPathField draft={draft} set={set} />
        </>
      );
  }
}

/**
 * Where kc.sh lives inside the container or pod.
 *
 * PortCloak reads KEYCLOAK_HOME off the image and otherwise assumes the path
 * the official images use. A custom build that installs Keycloak somewhere else
 * is not an exotic case, and left to guess PortCloak fails deep inside the
 * export against an executable that is not there. This is where the operator
 * says so instead.
 */
function KcPathField({ draft, set }: { draft: Environment; set: Setter }) {
  return (
    <Field
      label="Path to kc.sh inside the container (optional)"
      hint="Leave this empty for the official images. Set it for a custom build that installs Keycloak elsewhere — the probe reports which path it will use either way."
    >
      <Input
        type="text"
        value={draft.kcPath ?? ""}
        placeholder="/opt/keycloak/bin/kc.sh"
        onChange={(e) => set("kcPath", e.target.value)}
      />
    </Field>
  );
}

function CredentialField({
  label,
  credentialRef,
  value,
  onChange,
}: {
  label: string;
  credentialRef?: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <Field
      label={label}
      hint={
        credentialRef
          ? `Stored in this machine's keychain as ${credentialRef}. config.yaml holds only the handle above.`
          : "Stored in this machine's keychain; config.yaml will hold only a handle."
      }
    >
      <Input
        type="password"
        value={value}
        placeholder={credentialRef ? "•••••••• (stored)" : label}
        onChange={(e) => onChange(e.target.value)}
      />
    </Field>
  );
}

/**
 * The optional Admin API block.
 *
 * The user and credential fields used to appear only once a base URL had been
 * typed, and nothing on this form redrew while you typed — so the two fields
 * that complete the block were invisible at exactly the moment they were being
 * filled in, and only turned up after leaving the screen and coming back. They
 * are shown from the start now. An empty base URL is still what decides whether
 * the Admin API is used at all; the form simply stops hiding half a question.
 */
function AdminSection({
  draft,
  set,
  adminSecret,
  setAdminSecret,
}: {
  draft: Environment;
  set: Setter;
  adminSecret: string;
  setAdminSecret: (value: string) => void;
}) {
  return (
    <div>
      <Field
        label="Admin API base URL · optional, used only to verify secrets and detect themes"
        hint="The export reads the realm from the database and does not need this. Without it, verification and dependency detection are reported as skipped."
      >
        <Input
          type="text"
          value={draft.adminBaseUrl ?? ""}
          placeholder="https://sso.example"
          onChange={(e) => set("adminBaseUrl", e.target.value)}
        />
      </Field>

      <FieldRow>
        <Field
          label="Admin user"
          hint="The account the verification pass signs in as. It needs to read the realm, nothing more."
        >
          <Input
            type="text"
            value={draft.adminUser ?? ""}
            placeholder="admin"
            onChange={(e) => set("adminUser", e.target.value)}
          />
        </Field>
        <Field
          label="Admin credential"
          hint={
            draft.adminCredentialRef
              ? `Stored in this machine's keychain as ${draft.adminCredentialRef}. config.yaml holds only the handle.`
              : "Stored in this machine's keychain; config.yaml will hold only a handle."
          }
        >
          <Input
            type="password"
            value={adminSecret}
            placeholder={draft.adminCredentialRef ? "•••••••• (stored)" : "password"}
            onChange={(e) => setAdminSecret(e.target.value)}
          />
        </Field>
      </FieldRow>

      {/*
        Off by default, per environment, and never inferred from a failed
        handshake — an internal Keycloak behind a private CA is an ordinary
        deployment, and quietly trusting whatever answers is not.
      */}
      <Checkbox
        checked={Boolean(draft.adminInsecureTls)}
        label="Accept a self-signed certificate"
        hint="For an internal server whose certificate is self-signed or signed by a private CA this machine does not carry. It applies to this environment's Admin API alone."
        onChange={(value) => set("adminInsecureTls", value)}
      />

      {draft.adminInsecureTls ? (
        <Notice
          tone="warn"
          title="TLS verification is off for this Admin API"
          body={
            "PortCloak will accept whatever certificate answers at that URL, so it can no longer tell that server from anything able to occupy its address. " +
            "The admin credential above is sent over that connection. Nothing else changes: a snapshot's integrity is checked by its own digests and its encryption is unaffected."
          }
        />
      ) : null}
    </div>
  );
}

function confirmDelete(
  name: string,
  modal: ReturnType<typeof useModal>,
  onDeleted: () => void,
): void {
  modal.open({
    title: `Delete the environment “${name}”?`,
    body: (
      <div>
        <p>The definition and its keychain secrets are removed from this machine.</p>
        <p>
          <Muted>
            <Small>
              Snapshots already captured from it keep its name in their provenance — deleting an
              environment does not rewrite history, and nothing on the Keycloak itself is touched.
            </Small>
          </Muted>
        </p>
      </div>
    ),
    confirmLabel: "Delete environment",
    confirmTone: "danger-solid",
    onConfirm: async () => {
      const failure = await ConfigAPI.deleteEnvironment(name);
      if (failure) {
        modal.open({ title: "Not deleted", body: <div>{failure.message}</div> });
        return;
      }
      onDeleted();
    },
  });
}
