// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The forms that live inside the Keys screen's modals.
 *
 * They are components rather than trees built in a handler because each holds
 * state while it is open — which tab, what has been typed — and a component is
 * the only thing in React that can. The confirm button belongs to the modal
 * frame, so each form registers what pressing it should do through
 * `useModalControls`, and re-registers as what it would do changes.
 */
import { useEffect, useState } from "react";

import { KeysAPI } from "@/api";
import {
  Field,
  FieldHint,
  Input,
  Label,
  Muted,
  Notice,
  RevealValue,
  Small,
  Tab,
  TabBar,
  Textarea,
  useModalControls,
  type useModal,
} from "@/design-system";

type Modal = ReturnType<typeof useModal>;

/** The sentence a newly stored key is handed back with. */
function BackupNotice({ warning }: { warning: string }) {
  return <Notice tone="warn" title="Keep a copy somewhere this machine is not" body={warning} />;
}

export function GenerateKeyForm({ modal, reload }: { modal: Modal; reload: () => void }) {
  const [kind, setKind] = useState<"identity" | "passphrase">("identity");
  const [name, setName] = useState("");
  const [note, setNote] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const { setConfirm } = useModalControls();

  useEffect(() => {
    setConfirm(async () => {
      if (kind === "passphrase") {
        const failure = await KeysAPI.savePassphrase(name, passphrase, note);
        if (failure) {
          modal.open({ title: "Not created", body: <div>{failure.message}</div> });
          return;
        }
        reload();
        return;
      }
      const generated = await KeysAPI.generate(name, note);
      if (generated.failure) {
        modal.open({ title: "Not created", body: <div>{generated.failure.message}</div> });
        return;
      }
      reload();
      // Shown once, and only as a copy to keep elsewhere: PortCloak already
      // holds it, so this is not the operator's only chance to save it — it is
      // their only chance to save it somewhere the machine is not.
      modal.open({
        title: `The key “${generated.name}”`,
        body: (
          <div>
            <BackupNotice warning={generated.warning} />
            <Label style={{ marginTop: 12 }}>Private key</Label>
            <RevealValue>{generated.privateKey}</RevealValue>
            <Label style={{ marginTop: 12 }}>Public key (the recipient)</Label>
            <RevealValue>{generated.publicKey}</RevealValue>
          </div>
        ),
        cancelLabel: "Done",
      });
    });
  }, [kind, name, note, passphrase, setConfirm, modal, reload]);

  return (
    <div>
      <TabBar>
        <Tab $active={kind === "identity"} onClick={() => setKind("identity")}>
          Age keypair
        </Tab>
        <Tab $active={kind === "passphrase"} onClick={() => setKind("passphrase")}>
          Passphrase
        </Tab>
      </TabBar>

      <Field label="Name" hint="How every other screen refers to this key.">
        <Input
          type="text"
          value={name}
          placeholder="ops-team"
          onChange={(e) => setName(e.target.value)}
        />
      </Field>

      {kind === "passphrase" ? (
        <Field
          label="Passphrase"
          hint="PortCloak remembers this in the keychain and tries it when a snapshot needs opening."
        >
          <Input
            type="password"
            value={passphrase}
            onChange={(e) => setPassphrase(e.target.value)}
          />
        </Field>
      ) : (
        <p>
          <Muted>
            <Small>
              PortCloak generates the keypair. The private half goes to this machine&apos;s keychain
              and is shown once so you can keep a copy; the public half is what a capture seals to.
            </Small>
          </Muted>
        </p>
      )}

      <Field label="Note (optional)">
        <Input type="text" value={note} onChange={(e) => setNote(e.target.value)} />
      </Field>
    </div>
  );
}

export function ImportKeyForm({ modal, reload }: { modal: Modal; reload: () => void }) {
  const [name, setName] = useState("");
  const [note, setNote] = useState("");
  const [secret, setSecret] = useState("");
  const { setConfirm } = useModalControls();

  useEffect(() => {
    setConfirm(async () => {
      const failure = await KeysAPI.importIdentity(name, secret, note);
      if (failure) {
        modal.open({ title: "Not imported", body: <div>{failure.message}</div> });
        return;
      }
      reload();
    });
  }, [name, note, secret, setConfirm, modal, reload]);

  return (
    <div>
      <Field label="Name" hint="How every other screen refers to this key.">
        <Input
          type="text"
          value={name}
          placeholder="ops-team"
          onChange={(e) => setName(e.target.value)}
        />
      </Field>

      <Label>Age private key</Label>
      <Textarea
        rows={3}
        placeholder="AGE-SECRET-KEY-1…"
        value={secret}
        onChange={(e) => setSecret(e.target.value.trim())}
      />
      <FieldHint>
        Only the private half is needed. PortCloak derives the public half from it, so there is no
        way to store a pair whose halves do not match.
      </FieldHint>

      <Field label="Note (optional)">
        <Input type="text" value={note} onChange={(e) => setNote(e.target.value)} />
      </Field>
    </div>
  );
}

/**
 * "Type the name to confirm."
 *
 * The confirm button stays disabled until the field matches exactly, and what
 * it then does is registered here, so the caller only has to say what deleting
 * means.
 */
export function ConfirmByName({
  expected,
  onConfirmed,
}: {
  expected: string;
  onConfirmed: () => void | Promise<void>;
}) {
  const [typed, setTyped] = useState("");
  const { setConfirmDisabled, setConfirm } = useModalControls();

  useEffect(() => {
    setConfirmDisabled(typed !== expected);
    setConfirm(async () => {
      if (typed !== expected) return;
      await onConfirmed();
    });
  }, [typed, expected, onConfirmed, setConfirmDisabled, setConfirm]);

  return (
    <>
      <Label style={{ marginTop: 12 }}>{`Type ${expected} to confirm`}</Label>
      <Input type="text" value={typed} onChange={(e) => setTyped(e.target.value)} />
    </>
  );
}
