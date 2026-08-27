// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The Keys screen.
 *
 * Encryption used to be something an operator supplied from outside PortCloak
 * every single time: a passphrase typed at capture and typed again at every
 * restore, or an age keypair generated elsewhere and pasted in. PortCloak could
 * generate a keypair, but showed it once and stored nothing — so the operator
 * was still the key management system. That works exactly once, and then it
 * becomes the reason encryption gets turned off.
 *
 * A key here has a name, a kind and a home. The secret half goes to this
 * machine's OS keychain like every other secret PortCloak holds; config.yaml
 * carries the name, the kind, the public half where there is one, and a handle.
 * Restore and Inspect then try these keys without asking, and say which one
 * worked.
 */
import { KeysAPI, type KeyAvailability, type KeysView, type StoredKey } from "../../api";
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardHead,
  CardTitle,
  Label,
  Muted,
  Notice,
  PageHead,
  PageSubtitle,
  PageTitle,
  RevealValue,
  Row,
  Small,
  Spinner,
  useModal,
} from "../../design-system";
import { useAsync } from "../../hooks/useAsync";
import { ConfirmByName, GenerateKeyForm, ImportKeyForm } from "./KeyDialogs";

export function KeysPage() {
  const { state, reload } = useAsync(
    () =>
      Promise.all([KeysAPI.list(), KeysAPI.availability().catch(() => null)])
        .then(([view, availability]) => ({ view, availability }))
        .catch((error: unknown) => ({ error })),
    [],
  );

  if (state.status === "failed") throw state.error;
  if (state.status === "loading") return <Spinner>Reading keys…</Spinner>;
  if ("error" in state.value) {
    return (
      <Notice tone="danger" title="The keys could not be read." body={String(state.value.error)} />
    );
  }

  return (
    <Keys
      view={state.value.view}
      availability={state.value.availability}
      reload={() => void reload()}
    />
  );
}

function Keys({
  view,
  availability,
  reload,
}: {
  view: KeysView;
  availability: KeyAvailability | null;
  reload: () => void;
}) {
  const modal = useModal();

  const generate = () =>
    modal.open({
      title: "Create a key",
      body: <GenerateKeyForm modal={modal} reload={reload} />,
      confirmLabel: "Create",
    });

  const importKey = () =>
    modal.open({
      title: "Import a key",
      body: <ImportKeyForm modal={modal} reload={reload} />,
      confirmLabel: "Import",
    });

  return (
    <div>
      <PageHead>
        <div>
          <PageTitle>Encryption keys</PageTitle>
          <PageSubtitle>{view.note}</PageSubtitle>
        </div>
        <Row>
          <Button onClick={importKey}>Import a key</Button>
          <Button $variant="primary" onClick={generate}>
            Create a key
          </Button>
        </Row>
      </PageHead>

      {view.failure ? <Notice tone="danger" title="Not read" body={view.failure.message} /> : null}

      {availability && availability.fromSession > 0 ? (
        <SessionKeys availability={availability} reload={reload} />
      ) : null}

      {view.keys.length === 0 ? (
        <EmptyState onGenerate={generate} onImport={importKey} />
      ) : (
        <>
          {view.keys.map((key) => (
            <KeyCard
              key={key.name}
              storedKey={key}
              deleteWarning={view.deleteWarning}
              reload={reload}
            />
          ))}
          <Muted>
            <Small>
              A key lives in this machine&apos;s keychain. config.yaml holds only its name, its
              kind, its public half where it has one, and a handle — so the file stays portable
              between machines and the secrets deliberately do not.
            </Small>
          </Muted>
        </>
      )}
    </div>
  );
}

/**
 * Keys typed on a screen during this run.
 *
 * They are held in memory so that unlocking a snapshot in the library and then
 * restoring it does not ask for the same key twice. They are not stored, and
 * saying so here is the point of the card: quitting forgets them, and the way
 * to keep one is to create it above.
 */
function SessionKeys({
  availability,
  reload,
}: {
  availability: KeyAvailability;
  reload: () => void;
}) {
  return (
    <Card>
      <CardHead>
        <Row>
          <CardTitle>Used in this session</CardTitle>
          <Badge $tone="info">{availability.fromSession}</Badge>
        </Row>
        <Button
          onClick={async () => {
            await KeysAPI.forgetSessionKeys();
            reload();
          }}
        >
          Forget them
        </Button>
      </CardHead>
      <CardBody $muted>
        <Small>
          {`${availability.fromSession} key(s) you entered while opening or restoring a snapshot are held in memory for the rest of this run, so you are not asked for them again. They are not stored anywhere: quitting PortCloak forgets them.`}
        </Small>
      </CardBody>
    </Card>
  );
}

function EmptyState({ onGenerate, onImport }: { onGenerate: () => void; onImport: () => void }) {
  return (
    <Card>
      <CardBody>
        <CardTitle>No keys yet</CardTitle>
        <p>
          A key is how a snapshot gets sealed and opened again. Create one and PortCloak keeps it
          in this machine&apos;s keychain: captures can seal to it by name, and a restore opens
          the snapshot without asking you to remember anything.
        </p>
        <p>
          <Muted>
            <Small>
              An age keypair is the one to start with. Its public half is what a capture seals to,
              which means the machine that takes a snapshot does not have to be able to open it —
              capture and restore stop being the same privilege.
            </Small>
          </Muted>
        </p>
        <Row>
          <Button $variant="primary" onClick={onGenerate}>
            Create a key
          </Button>
          <Button onClick={onImport}>Import one I already have</Button>
        </Row>
      </CardBody>
    </Card>
  );
}

function KeyCard({
  storedKey,
  deleteWarning,
  reload,
}: {
  storedKey: StoredKey;
  deleteWarning: string;
  reload: () => void;
}) {
  const modal = useModal();
  const kindLabel = storedKey.kind === "identity" ? "Age keypair" : "Passphrase";

  return (
    <Card $tone={storedKey.present ? undefined : "warning"}>
      <CardHead>
        <Row>
          <CardTitle>{storedKey.name}</CardTitle>
          <Badge $tone="info">{kindLabel}</Badge>
          {storedKey.present ? (
            <Badge $tone="ok">In this keychain</Badge>
          ) : (
            <Badge $tone="warn">Not on this machine</Badge>
          )}
        </Row>
        <Row>
          {storedKey.age ? (
            <Muted>
              <Small>{`Created ${storedKey.age}`}</Small>
            </Muted>
          ) : null}
          {storedKey.present ? (
            <Button onClick={() => confirmReveal(storedKey, modal)}>Show secret</Button>
          ) : null}
          <Button
            $variant="danger"
            onClick={() => confirmDelete(storedKey, deleteWarning, modal, reload)}
          >
            Delete
          </Button>
        </Row>
      </CardHead>
      <CardBody>
        {storedKey.note ? <p style={{ marginTop: 0 }}>{storedKey.note}</p> : null}
        <Muted>
          <Small>{storedKey.summary}</Small>
        </Muted>
        {storedKey.publicKey ? (
          <div style={{ marginTop: 10 }}>
            <Label>Public key — captures seal to this</Label>
            <RevealValue>{storedKey.publicKey}</RevealValue>
          </div>
        ) : null}
      </CardBody>
    </Card>
  );
}

function confirmReveal(storedKey: StoredKey, modal: ReturnType<typeof useModal>): void {
  modal.open({
    title: `Show the secret half of “${storedKey.name}”?`,
    body: (
      <div>
        <p>
          {storedKey.kind === "identity"
            ? "This is the private key. Anyone holding it can open every snapshot sealed to this key."
            : "This is the passphrase. Anyone holding it can open every snapshot sealed with it."}
        </p>
        <p>
          <Muted>
            <Small>PortCloak records that it was shown, and never what was shown.</Small>
          </Muted>
        </p>
      </div>
    ),
    confirmLabel: "Show it",
    confirmTone: "danger-solid",
    onConfirm: async () => {
      const result = await KeysAPI.reveal(storedKey.name);
      if (result.failure) {
        modal.open({ title: "Not shown", body: <div>{result.failure.message}</div> });
        return;
      }
      modal.open({
        title: `The key “${result.name}”`,
        body: (
          <div>
            <Notice
              tone="warn"
              title="Keep a copy somewhere this machine is not"
              body={result.warning}
            />
            <Label style={{ marginTop: 12 }}>
              {storedKey.kind === "identity" ? "Private key" : "Passphrase"}
            </Label>
            <RevealValue>{result.secret}</RevealValue>
          </div>
        ),
        cancelLabel: "Done",
      });
    },
  });
}

/**
 * Deleting a key is the one action on this screen PortCloak cannot soften.
 *
 * A key is not "in use" by anything the tool can see: it is in use by every
 * snapshot ever sealed with it, and those live in storage backends that may not
 * even be configured here. So the confirmation asks for the name to be typed,
 * the way an overwrite does — the consequence is comparable.
 */
function confirmDelete(
  storedKey: StoredKey,
  warning: string,
  modal: ReturnType<typeof useModal>,
  reload: () => void,
): void {
  modal.open({
    title: `Delete the key “${storedKey.name}”?`,
    body: (
      <div>
        <Notice tone="danger" title="This cannot be undone" body={warning} />
        <ConfirmByName
          expected={storedKey.name}
          onConfirmed={async () => {
            const failure = await KeysAPI.remove(storedKey.name);
            if (failure) {
              modal.open({ title: "Not deleted", body: <div>{failure.message}</div> });
              return;
            }
            reload();
          }}
        />
      </div>
    ),
    confirmLabel: "Delete this key",
    confirmTone: "danger-solid",
    confirmDisabled: true,
  });
}
