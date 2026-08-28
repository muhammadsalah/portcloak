// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The two things the inspector's header can do to a snapshot: prove it, and
 * close it again.
 *
 * They are functions rather than components because each is a modal opened from
 * a button, and none of them owns any of the screen.
 */
import { InspectAPI, type VerifyReport } from "../../api";
import {
  Badge,
  Mono,
  Muted,
  Notice,
  Small,
  Table,
  TableScroll,
  useModal,
} from "../../design-system";

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
