// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * What PortCloak is holding on this disk, and the one button that clears it.
 *
 * Purging is housekeeping, not job control: discarding an interrupted job's
 * checkpoint is a separate action on the Activity screen, and this panel says
 * so rather than quietly doing both.
 */
import { SettingsAPI, type WorkingData } from "../../api";
import {
  Badge,
  BulletList,
  Button,
  Card,
  CardBody,
  CardHead,
  CardTitle,
  FailureNotice,
  KeyValue,
  Muted,
  NoticeBox,
  Small,
  useModal,
} from "../../design-system";
import { bytes } from "../../utils/format";

type Modal = ReturnType<typeof useModal>;

export function WorkingDataPanel({
  working,
  reload,
}: {
  working: WorkingData;
  reload: () => void;
}) {
  const modal = useModal();

  return (
    <Card>
      <CardHead>
        <CardTitle>Local working data</CardTitle>
      </CardHead>
      <CardBody>
        <KeyValue>
          <dt>Inspection indexes</dt>
          <dd>{working.indexNote}</dd>
          <dt>Finished job records</dt>
          <dd>{`${working.finishedJobs} · ${bytes(working.finishedBytes)}`}</dd>
          <dt>Interrupted jobs (resumable)</dt>
          <dd>
            {working.interruptedJobs > 0 ? (
              <Badge $tone="warn">{`${working.interruptedJobs} · kept`}</Badge>
            ) : (
              "none"
            )}
          </dd>
          <dt>Decrypted working files</dt>
          <dd>{bytes(working.workBytes)}</dd>
          <dt>Rotated logs</dt>
          <dd>{bytes(working.logBytes)}</dd>
        </KeyValue>

        <NoticeBox $tone="info" style={{ marginTop: 12, fontSize: 12 }}>
          <div>{working.note}</div>
          <div style={{ marginTop: 6, fontWeight: 600 }}>It never touches:</div>
          <Keeps keeps={working.keeps} />
        </NoticeBox>

        <Button
          $variant="danger"
          style={{ marginTop: 12 }}
          onClick={() => confirmPurge(working, modal, reload)}
        >
          Purge local data
        </Button>
      </CardBody>
    </Card>
  );
}

function Keeps({ keeps }: { keeps: string[] }) {
  return (
    <BulletList>
      {keeps.map((keep) => (
        <li key={keep}>{keep}</li>
      ))}
    </BulletList>
  );
}

function confirmPurge(working: WorkingData, modal: Modal, reload: () => void): void {
  modal.open({
    title: "Purge local working data?",
    body: (
      <div>
        <p>{working.note}</p>
        <p style={{ fontWeight: 600, marginBottom: 2 }}>It will not touch:</p>
        <Keeps keeps={working.keeps} />
        <p style={{ marginTop: 12 }}>
          <Muted>
            <Small>
              Discarding an interrupted job&apos;s checkpoint is a separate action on the Activity
              screen. This is housekeeping, not job control.
            </Small>
          </Muted>
        </p>
      </div>
    ),
    confirmLabel: "Purge",
    confirmTone: "danger-solid",
    onConfirm: async () => {
      const result = await SettingsAPI.purge();
      if (result.failure) {
        modal.open({
          title: "Not purged",
          body: <FailureNotice failure={result.failure} />,
          cancelLabel: "Close",
        });
        return;
      }
      modal.open({
        title: "Purged",
        body: (
          <div>
            <p>{result.note}</p>
            <p>
              <Muted>
                <Small>{`${bytes(result.bytes)} freed.`}</Small>
              </Muted>
            </p>
          </div>
        ),
        cancelLabel: "Close",
      });
      reload();
    },
  });
}
