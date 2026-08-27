/**
 * Where PortCloak keeps its files, and how to move it.
 *
 * The panel is mostly a statement of fact; the two buttons are the only things
 * on this screen that relocate anything, and both explain what travels and what
 * deliberately does not before they do it.
 */
import { useEffect, useState } from "react";

import { SettingsAPI, type LocationView } from "../../api";
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardHead,
  CardTitle,
  FailureNotice,
  Field,
  Input,
  KeyValue,
  Mono,
  Muted,
  Notice,
  PathBox,
  Row,
  Small,
  useModal,
  useModalControls,
  type Tone,
} from "../../design-system";

type Modal = ReturnType<typeof useModal>;

export function LocationPanel({
  location,
  reload,
}: {
  location: LocationView;
  reload: () => void;
}) {
  const modal = useModal();
  const missing = location.credentials.filter((c) => !c.present);

  return (
    <Card>
      <CardHead>
        <CardTitle>Where PortCloak keeps its files</CardTitle>
        <Badge $tone={sourceTone(location)}>{sourceLabel(location)}</Badge>
      </CardHead>
      <CardBody>
        <PathBox>{location.root}</PathBox>

        <KeyValue style={{ marginTop: 12 }}>
          <dt>Configuration</dt>
          <dd>
            <Mono>{location.configFile}</Mono>
          </dd>
          <dt>Chosen how</dt>
          <dd>{location.sourceNote}</dd>
          {location.source === "chosen" ? (
            <>
              <dt>Recorded in</dt>
              <dd>
                <Mono>{location.pointer}</Mono>
              </dd>
            </>
          ) : null}
        </KeyValue>

        <p>
          <Muted>
            <Small>{location.note}</Small>
          </Muted>
        </p>

        {location.failure ? <FailureNotice failure={location.failure} /> : null}

        {/*
          A reason the move would be refused is worth showing before the button
          is pressed, not after — an open snapshot is something the operator can
          go and close.
        */}
        {location.blocked ? (
          <Notice tone="info" title="Not movable right now" body={location.blocked} />
        ) : null}

        <Row style={{ marginTop: 12 }}>
          <Button
            $variant="primary"
            disabled={!location.movable || Boolean(location.blocked)}
            onClick={() => confirmMove(location, modal, reload)}
          >
            Move to another folder…
          </Button>
          {!location.atDefault ? (
            <Button
              disabled={!location.movable || Boolean(location.blocked)}
              onClick={() => confirmDefault(location, modal, reload)}
            >
              Use the default folder
            </Button>
          ) : null}
          <Button $variant="plain" onClick={reload}>
            Refresh
          </Button>
        </Row>

        {missing.length > 0 ? (
          <Notice
            tone="warn"
            title={`${missing.length} credential${missing.length === 1 ? "" : "s"} not on this machine`}
            body={
              "Configuration is portable between machines; the secrets deliberately are not. " +
              missing.map((m) => m.name).join(", ")
            }
          />
        ) : (
          <AllPresent>
            {`✓ Every credential referenced by this config is in this machine's keychain (${location.credentials.length}).`}
          </AllPresent>
        )}
      </CardBody>
    </Card>
  );
}

function sourceLabel(location: LocationView): string {
  switch (location.source) {
    case "environment":
      return "PORTCLOAK_HOME";
    case "chosen":
      return "Chosen folder";
    default:
      return "Default folder";
  }
}

function sourceTone(location: LocationView): Tone {
  switch (location.source) {
    case "environment":
      return "warn";
    case "chosen":
      return "info";
    default:
      return "neutral";
  }
}

/** What moves and what stays, stated before the folder is picked. */
function MovedAndKept() {
  return (
    <div>
      <div style={{ fontWeight: 600, marginBottom: 2 }}>What moves:</div>
      <ul style={{ fontSize: 12, margin: "4px 0 0", paddingLeft: 18 }}>
        <li>config.yaml — your environments, storage definitions and preferences</li>
        <li>the audit log</li>
        <li>job checkpoints, including any interrupted job waiting to be resumed</li>
        <li>logs, inspection indexes and decrypted working files</li>
      </ul>
      <div style={{ fontWeight: 600, margin: "10px 0 2px" }}>What does not:</div>
      <ul style={{ fontSize: 12, margin: "4px 0 0", paddingLeft: 18 }}>
        <li>this machine&apos;s keychain — every credential stays exactly where it is</li>
        <li>every snapshot in storage; moving this folder moves no backup</li>
      </ul>
    </div>
  );
}

/** The folder field, which arms the confirm button once it holds a path. */
function MoveForm({
  from,
  onMove,
}: {
  from: string;
  onMove: (folder: string) => void | Promise<void>;
}) {
  const [folder, setFolder] = useState("");
  const { setConfirmDisabled, setConfirm } = useModalControls();

  useEffect(() => {
    const trimmed = folder.trim();
    setConfirmDisabled(trimmed === "");
    setConfirm(() => onMove(trimmed));
  }, [folder, onMove, setConfirmDisabled, setConfirm]);

  return (
    <div>
      <Field
        label="New folder"
        hint="The full path. It has to be empty or not exist yet, so nothing already there can be overwritten."
      >
        <Input
          type="text"
          value={folder}
          placeholder="/Volumes/work/portcloak"
          onChange={(e) => setFolder(e.target.value)}
        />
      </Field>
      <MovedAndKept />
      <p style={{ marginTop: 12 }}>
        <Muted>
          <Small>
            {`Moving from ${from}. The running application follows the folder — nothing has to be restarted.`}
          </Small>
        </Muted>
      </p>
    </div>
  );
}

function confirmMove(location: LocationView, modal: Modal, reload: () => void): void {
  modal.open({
    title: "Move the PortCloak folder?",
    body: (
      <MoveForm
        from={location.root}
        onMove={async (folder) => {
          const result = await SettingsAPI.move(folder);
          reportMove(result, folder, modal);
          reload();
        }}
      />
    ),
    confirmLabel: "Move",
    confirmDisabled: true,
  });
}

function confirmDefault(location: LocationView, modal: Modal, reload: () => void): void {
  modal.open({
    title: "Go back to the default folder?",
    body: (
      <div>
        <p>
          {`Everything moves back to ${location.default}, and PortCloak stops recording a chosen folder.`}
        </p>
        <MovedAndKept />
      </div>
    ),
    confirmLabel: "Move it back",
    onConfirm: async () => {
      const result = await SettingsAPI.useDefault();
      reportMove(result, location.default, modal);
      reload();
    },
  });
}

/**
 * A move that failed has to say so out loud. The panel behind the modal
 * re-reads the location either way, so a silent failure would look exactly like
 * a success that did nothing.
 */
function reportMove(result: LocationView, wanted: string, modal: Modal): void {
  if (result.failure) {
    modal.open({
      title: "Not moved",
      body: (
        <div>
          <FailureNotice failure={result.failure} />
          <p>
            <Muted>
              <Small>{`PortCloak is still using ${result.root}.`}</Small>
            </Muted>
          </p>
        </div>
      ),
      cancelLabel: "Close",
    });
    return;
  }
  modal.open({
    title: "Moved",
    body: (
      <div>
        <p>{`PortCloak is now reading and writing under ${result.root}.`}</p>
        {result.root !== wanted ? (
          <p>
            <Muted>
              <Small>{`You asked for ${wanted}.`}</Small>
            </Muted>
          </p>
        ) : null}
        <p>
          <Muted>
            <Small>
              Nothing was left behind at the old location, and the next launch will find it here.
            </Small>
          </Muted>
        </p>
      </div>
    ),
    cancelLabel: "Close",
  });
}

const AllPresent = ({ children }: { children: string }) => (
  <div style={{ fontSize: 12, color: "var(--success)", marginTop: 12 }}>{children}</div>
);
