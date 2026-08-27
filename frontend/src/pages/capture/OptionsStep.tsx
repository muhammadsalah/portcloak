/**
 * Step three: how the export runs, and how the result is sealed.
 *
 * The encryption half is the important one. Declining is a deliberate action
 * with its own confirmation, not a toggle nobody noticed.
 */
import { useState } from "react";

import type { KeyRecipient, ProbeResult, TargetFacts, WizardDefaults } from "../../api";
import {
  Button,
  Checkbox,
  Chip,
  Divider,
  Field,
  FieldHint,
  Grow,
  IconButton,
  Input,
  Notice,
  NoticeBox,
  NoticeTitle,
  Right,
  Row,
  SectionTitle,
  Select,
  Small,
  Strong,
  Toggle,
  useModal,
} from "../../design-system";
import { bytes } from "../../utils/format";
import type { CaptureDraft, UpdateDraft } from "./draft";

export function OptionsStep({
  defaults,
  draft,
  update,
  probe,
  storedKeys,
}: {
  defaults: WizardDefaults;
  draft: CaptureDraft;
  update: UpdateDraft;
  probe: ProbeResult | undefined;
  storedKeys: KeyRecipient[];
}) {
  return (
    <div>
      <SectionTitle>Options</SectionTitle>

      {probe?.ok ? <ProbeSummary facts={probe.facts} /> : null}

      <Field
        label="Users export mode"
        hint="Bounded file sizes let PortCloak checkpoint per file — better behaviour on flaky links, and what makes a very large realm survivable."
      >
        <Select value={draft.usersMode} onChange={(e) => update({ usersMode: e.target.value })}>
          <option value="different_files">
            {`different_files — ${draft.usersPerFile} users per file`}
          </option>
          <option value="realm_file">realm_file — users inside the realm document</option>
        </Select>
      </Field>

      <Checkbox
        checked={draft.verify}
        label="Verify secrets are unmasked (Admin API)"
        hint="Catches version-specific masking so a dud client secret is flagged, not shipped. Skipped without complaint if the Admin API is not reachable."
        onChange={(value) => update({ verify: value })}
      />
      <Checkbox
        checked={draft.detectDependencies}
        label="Detect external dependencies (themes, provider JARs)"
        hint="Reported as restore preconditions. PortCloak never migrates these files."
        onChange={(value) => update({ detectDependencies: value })}
      />

      <Divider />

      <EncryptionSection
        defaults={defaults}
        draft={draft}
        update={update}
        storedKeys={storedKeys}
      />
    </div>
  );
}

function ProbeSummary({ facts }: { facts: TargetFacts }) {
  const parts: string[] = [];
  if (facts.keycloakVersion) parts.push(`Keycloak ${facts.keycloakVersion}`);
  if (facts.kcPath) parts.push(`kc.sh at ${facts.kcPath}`);
  if (facts.cloneCapable) parts.push("ephemeral clone permitted");
  if (facts.ports?.http) {
    parts.push(
      `ports ${facts.ports.http} / ${facts.ports.https} / ${facts.ports.management} allocated`,
    );
  }
  if (facts.freeBytes) parts.push(`${bytes(facts.freeBytes)} free`);
  parts.push(facts.adminReachable ? "Admin API reachable" : "Admin API not reachable");

  return (
    <NoticeBox $tone="ok">
      <NoticeTitle>Probe passed — capture will not touch the serving instance</NoticeTitle>
      <Small>{parts.join(" · ")}</Small>
    </NoticeBox>
  );
}

function EncryptionSection({
  defaults,
  draft,
  update,
  storedKeys,
}: {
  defaults: WizardDefaults;
  draft: CaptureDraft;
  update: UpdateDraft;
  storedKeys: KeyRecipient[];
}) {
  const modal = useModal();

  const setEncrypt = (on: boolean) => {
    if (on) {
      update({ encrypt: true, acknowledgedUnencrypted: false });
      return;
    }
    // Declining is one deliberate action: the notice is shown in full and
    // confirmed, rather than being a default nobody noticed.
    update({ encrypt: false });
    modal.open({
      title: "Write this snapshot unencrypted?",
      body: (
        <div>
          <p>{defaults.declineNotice}</p>
          <p>
            <Small>
              This is a supported choice and PortCloak will not nag about it again for this
              capture. It will label the snapshot in the library, the manifest and the completeness
              report, and record the decision in the audit log.
            </Small>
          </p>
        </div>
      ),
      confirmLabel: "Write it unencrypted",
      confirmTone: "danger-solid",
      cancelLabel: "Keep encryption on",
      onConfirm: () => update({ acknowledgedUnencrypted: true }),
      // "Keep encryption on" is an answer, not a way out of the question. It
      // turns the toggle back on rather than leaving the wizard in the state
      // neither button offered — encryption off, unconfirmed, and blocked on
      // "Confirm that this snapshot may be written unencrypted."
      onCancel: () => update({ encrypt: true, acknowledgedUnencrypted: false }),
    });
  };

  return (
    <div>
      <Row>
        <Strong>🔒 Encrypt this snapshot</Strong>
        <Right>
          <Toggle on={draft.encrypt} onChange={setEncrypt} />
        </Right>
      </Row>
      <p>
        <Small>{defaults.encryptionNotice}</Small>
      </p>

      {draft.encrypt ? (
        <>
          <Row $align="flex-start" $gap={12}>
            <Select
              style={{ maxWidth: 200 }}
              value={draft.encryptionMode}
              onChange={(e) =>
                update({ encryptionMode: e.target.value as CaptureDraft["encryptionMode"] })
              }
            >
              <option value="passphrase">Passphrase</option>
              <option value="recipients">Recipients (age)</option>
            </Select>
            <Grow>
              {draft.encryptionMode === "passphrase" ? (
                <Input
                  type="password"
                  placeholder="Passphrase"
                  value={draft.passphrase}
                  onChange={(e) => update({ passphrase: e.target.value })}
                />
              ) : (
                <RecipientsEditor draft={draft} update={update} storedKeys={storedKeys} />
              )}
            </Grow>
          </Row>
          {draft.encryptionMode === "passphrase" ? (
            <RememberPassphrase draft={draft} update={update} />
          ) : null}
        </>
      ) : draft.acknowledgedUnencrypted ? (
        <Notice
          tone="danger"
          title="This snapshot will be written unencrypted"
          body={defaults.declineNotice}
        />
      ) : null}
    </div>
  );
}

/**
 * Who a snapshot is sealed to.
 *
 * The keys PortCloak already holds come first, by name. Pasting a public key
 * still works — a colleague's key is a legitimate recipient and PortCloak will
 * never hold its private half — but it is no longer the only way in, which is
 * what made recipient mode something operators read about and then skipped.
 */
function RecipientsEditor({
  draft,
  update,
  storedKeys,
}: {
  draft: CaptureDraft;
  update: UpdateDraft;
  storedKeys: KeyRecipient[];
}) {
  const [pending, setPending] = useState("");

  // Anything sealed to a key PortCloak does not hold, shown as a chip so it is
  // never silently part of the decision.
  const known = new Set(storedKeys.map((key) => key.publicKey));
  const pasted = draft.recipients.filter((recipient) => !known.has(recipient));

  return (
    <div>
      {storedKeys.length > 0 ? (
        <div style={{ marginBottom: 10 }}>
          {storedKeys.map((key) => (
            <Checkbox
              key={key.publicKey}
              checked={draft.recipients.includes(key.publicKey)}
              label={key.name}
              hint={
                key.openable
                  ? "This machine holds the private half, so a snapshot sealed to it can be opened here without being asked for a key."
                  : "Only the public half is here. A snapshot sealed to this key cannot be opened on this machine."
              }
              onChange={(on) =>
                update({
                  recipients: on
                    ? draft.recipients.includes(key.publicKey)
                      ? draft.recipients
                      : [...draft.recipients, key.publicKey]
                    : draft.recipients.filter((r) => r !== key.publicKey),
                })
              }
            />
          ))}
        </div>
      ) : null}

      {pasted.length > 0 ? (
        <Row $wrap $gap={6} style={{ marginBottom: 8 }}>
          {pasted.map((recipient) => (
            <Chip key={recipient}>
              {`${recipient.slice(0, 10)}…${recipient.slice(-4)}`}
              <IconButton
                onClick={() =>
                  update({ recipients: draft.recipients.filter((r) => r !== recipient) })
                }
              >
                ×
              </IconButton>
            </Chip>
          ))}
        </Row>
      ) : null}

      <Row>
        <Input
          type="text"
          placeholder="…or paste an age public key (age1…)"
          value={pending}
          onChange={(e) => setPending(e.target.value.trim())}
        />
        <Button
          onClick={() => {
            if (pending && !draft.recipients.includes(pending)) {
              update({ recipients: [...draft.recipients, pending] });
              setPending("");
            }
          }}
        >
          Add
        </Button>
      </Row>

      {storedKeys.length === 0 ? (
        <FieldHint>
          There are no keys on this machine yet. Create one under Keys and it appears here by name
          — and opens this snapshot again without being asked for.
        </FieldHint>
      ) : null}
    </div>
  );
}

/**
 * Remembering the passphrase.
 *
 * A passphrase typed at capture and typed again at every restore is the reason
 * encryption gets turned off. Naming it stores it in this machine's keychain,
 * where a restore finds it without asking. Leaving the name empty keeps the old
 * behaviour exactly: PortCloak holds nothing and cannot recover it.
 */
function RememberPassphrase({
  draft,
  update,
}: {
  draft: CaptureDraft;
  update: UpdateDraft;
}) {
  return (
    <div style={{ marginTop: 10 }}>
      <Field
        label="Remember this passphrase as (optional)"
        hint={
          draft.rememberPassphraseAs
            ? `Stored in this machine's keychain as the key “${draft.rememberPassphraseAs}”, and tried automatically whenever a snapshot needs opening.`
            : "Leave this empty and PortCloak stores nothing: the passphrase will be asked for every time this snapshot is opened, and cannot be recovered if it is lost."
        }
      >
        <Input
          type="text"
          value={draft.rememberPassphraseAs}
          placeholder="nightly-captures"
          onChange={(e) => update({ rememberPassphraseAs: e.target.value })}
        />
      </Field>
    </div>
  );
}
