// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The three things the inspector's header can do to a snapshot: prove it,
 * export a view of it, and close it again.
 *
 * They are functions rather than components because each is a modal opened from
 * a button, and none of them owns any of the screen.
 */
import { useEffect, useState } from "react";

import { InspectAPI, type UsersQuery, type VerifyReport } from "../../api";
import {
  Badge,
  Input,
  Label,
  Mono,
  Muted,
  Notice,
  Small,
  Table,
  TableScroll,
  useModal,
  useModalControls,
} from "../../design-system";
import { bytes, count } from "../../utils/format";

type Modal = ReturnType<typeof useModal>;

/* ── Verify ────────────────────────────────────────────────────────────── */

export async function verify(snapshotId: string, modal: Modal): Promise<void> {
  const [report, failure] = await InspectAPI.verify(snapshotId);
  if (failure) {
    modal.open({ title: "Verification could not run", body: <div>{failure.message}</div> });
    return;
  }
  modal.open({
    title: "Verification",
    body: <VerifyBody report={report} />,
    cancelLabel: "Close",
  });
}

function VerifyBody({ report }: { report: VerifyReport }) {
  return (
    <div>
      <Notice tone={report.ok ? "ok" : "danger"} title={report.message} body={report.note} />
      <TableScroll>
        <Table>
          <thead>
            <tr>
              <th>Artifact</th>
              <th />
              <th />
            </tr>
          </thead>
          <tbody>
            {report.artifacts.map((artifact) => (
              <tr key={artifact.name}>
                <td>
                  <Mono>{artifact.name}</Mono>
                </td>
                <td>
                  {artifact.ok ? (
                    <Badge $tone="ok">OK</Badge>
                  ) : (
                    <Badge $tone="danger">Failed</Badge>
                  )}
                </td>
                <td>
                  <Muted>
                    <Small>{artifact.note ?? ""}</Small>
                  </Muted>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      </TableScroll>
    </div>
  );
}

/* ── Export ────────────────────────────────────────────────────────────── */

export function exportView(
  snapshotId: string,
  view: string,
  query: UsersQuery,
  modal: Modal,
): void {
  modal.open({
    title: "Export this view",
    body: (
      <ExportForm
        view={view}
        onExport={async (path, format) => {
          const [result, failure] = await InspectAPI.exportView(
            snapshotId,
            { view, format, path },
            query,
          );
          if (failure) {
            modal.open({ title: "Not exported", body: <div>{failure.message}</div> });
            return;
          }
          modal.open({
            title: "Exported",
            body: (
              <div>
                <p>
                  <Mono>{result.path}</Mono>
                </p>
                <p>{`${count(result.rows)} rows · ${bytes(result.bytes)}`}</p>
                <p>
                  <Muted>
                    <Small>{result.note}</Small>
                  </Muted>
                </p>
              </div>
            ),
            cancelLabel: "Close",
          });
        }}
      />
    ),
    confirmLabel: "Export",
  });
}

/** The destination path. Its extension is what chooses the format. */
function ExportForm({
  view,
  onExport,
}: {
  view: string;
  onExport: (path: string, format: string) => void | Promise<void>;
}) {
  const [path, setPath] = useState(
    `${view}.${view === "users" || view === "secretLedger" ? "csv" : "json"}`,
  );
  const { setConfirm } = useModalControls();

  useEffect(() => {
    setConfirm(() => onExport(path, path.endsWith(".json") ? "json" : "csv"));
  }, [path, onExport, setConfirm]);

  return (
    <div>
      <p>
        <Muted>
          <Small>
            The export carries the rows currently shown, redacted by the same rules as the screen —
            presence, never values. Exporting is itself recorded in the audit log.
          </Small>
        </Muted>
      </p>
      <Label>Destination path</Label>
      <Input type="text" value={path} onChange={(e) => setPath(e.target.value)} />
    </div>
  );
}

/* ── Close ─────────────────────────────────────────────────────────────── */

export function closeSnapshot(
  snapshotId: string,
  modal: Modal,
  onClosed: (confirmed: string) => void,
): void {
  modal.open({
    title: "Close this snapshot?",
    body: (
      <div>
        <p>
          PortCloak will drop the inspection index and shred every decrypted working file for this
          snapshot.
        </p>
        <p>
          <Muted>
            <Small>
              Re-opening pays the index build again. That cost is deliberate: an index is a
              searchable copy of your user directory, and leaving one on this workstation between
              sessions is the worse liability.
            </Small>
          </Muted>
        </p>
      </div>
    ),
    confirmLabel: "Close snapshot",
    onConfirm: async () => {
      const result = await InspectAPI.close(snapshotId);
      if (result.failure) {
        modal.open({ title: "Not closed", body: <div>{result.failure.message}</div> });
        return;
      }
      onClosed(result.confirmed);
    },
  });
}
