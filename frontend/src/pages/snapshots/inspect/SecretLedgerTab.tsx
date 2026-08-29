// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Every secret the snapshot carries, by location, and whether it is there at
 * all.
 *
 * Values are masked. Revealing one is a deliberate action with a reason field,
 * and the audit log records that it happened — never what was shown.
 */
import { useEffect, useState } from "react";
import styled from "styled-components";

import { InspectAPI, type LedgerEntry, type LedgerView } from "@/api";
import {
  Card,
  CardFoot,
  FailureNotice,
  Input,
  Label,
  Link,
  Mono,
  Muted,
  Notice,
  Small,
  Spinner,
  Table,
  TableScroll,
  useModal,
  useModalControls,
  Pagination,
  pageSizes,
} from "@/design-system";
import { useAsync } from "@/hooks/useAsync";
import { count } from "@/utils/format";

type Modal = ReturnType<typeof useModal>;

export function SecretLedgerTab({ snapshotId }: { snapshotId: string }) {
  const { state } = useAsync(() => InspectAPI.ledger(snapshotId), [snapshotId]);

  if (state.status === "failed") throw state.error;
  if (state.status === "loading") return <Spinner>Reading the ledger…</Spinner>;

  const ledger = state.value;
  if (ledger.failure) return <FailureNotice failure={ledger.failure} />;

  return <Ledger ledger={ledger} snapshotId={snapshotId} />;
}

/**
 * The ledger, paged.
 *
 * A realm that issues a client per integration has a secret per client, so this
 * is the same length as the client list and for the same reason.
 */
function Ledger({ ledger, snapshotId }: { ledger: LedgerView; snapshotId: string }) {
  const [page, setPage] = useState<{ offset: number; limit: number }>({
    offset: 0,
    limit: pageSizes[0],
  });
  const entries = ledger.entries;
  const offset = page.offset >= entries.length ? 0 : page.offset;
  const shown = entries.slice(offset, offset + page.limit);
  const from = entries.length === 0 ? 0 : offset + 1;
  const to = Math.min(offset + page.limit, entries.length);

  return (
    <div>
      <Notice tone="warn" title={ledger.note} />

      <Card>
        <TableScroll>
          <Table>
            <thead>
              <tr>
                <th>Location</th>
                <th>Kind</th>
                <th>Status</th>
                <th>Value</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {shown.map((entry) => (
                <LedgerRow
                  key={entry.location}
                  entry={entry}
                  snapshotId={snapshotId}
                  revealAllowed={ledger.revealAllowed}
                />
              ))}
            </tbody>
          </Table>
        </TableScroll>

        <CardFoot>
          <div>
            <Small>{`${from}–${to} of ${count(entries.length)}`}</Small>
            <div>
              <Muted>
                <Small>{ledger.summary}</Small>
              </Muted>
            </div>
          </div>
          {entries.length > pageSizes[0] ? (
            <Pagination
              total={entries.length}
              offset={offset}
              limit={page.limit}
              onChange={setPage}
            />
          ) : null}
        </CardFoot>
      </Card>
    </div>
  );
}

function LedgerRow({
  entry,
  snapshotId,
  revealAllowed,
}: {
  entry: LedgerEntry;
  snapshotId: string;
  revealAllowed: boolean;
}) {
  const modal = useModal();
  const [revealed, setRevealed] = useState<{ value: string; note: string } | null>(null);

  return (
    <>
      <tr>
        <td>
          <Mono>{entry.location}</Mono>
        </td>
        <td>{entry.kindLabel}</td>
        <Status $masked={entry.masked}>{entry.status}</Status>
        <td>
          {!entry.revealable ? (
            <NotCarried>{entry.note ?? "not carried"}</NotCarried>
          ) : revealed ? (
            <Mono>{revealed.value}</Mono>
          ) : (
            <Muted>
              <Mono>••••••••••••</Mono>
            </Muted>
          )}
        </td>
        <td style={{ textAlign: "right" }}>
          {entry.revealable && revealAllowed ? (
            revealed ? (
              <Link onClick={() => setRevealed(null)}>Hide</Link>
            ) : (
              <Link
                onClick={() =>
                  confirmReveal(snapshotId, entry.location, modal, (value, note) =>
                    setRevealed({ value, note }),
                  )
                }
              >
                Reveal
              </Link>
            )
          ) : entry.revealable ? (
            <Muted>
              <Small>Reveal is off</Small>
            </Muted>
          ) : null}
        </td>
      </tr>
      {revealed ? (
        <tr>
          <td colSpan={5}>
            <Muted>
              <Small>{revealed.note}</Small>
            </Muted>
          </td>
        </tr>
      ) : null}
    </>
  );
}

function confirmReveal(
  snapshotId: string,
  location: string,
  modal: Modal,
  onRevealed: (value: string, note: string) => void,
): void {
  modal.open({
    title: "Reveal this secret?",
    body: (
      <RevealForm
        location={location}
        onReveal={async (reason) => {
          const result = await InspectAPI.reveal(snapshotId, location, reason);
          if (result.failure) {
            modal.open({ title: "Not revealed", body: <div>{result.failure.message}</div> });
            return;
          }
          onRevealed(result.value, result.note);
        }}
      />
    ),
    confirmLabel: "Reveal",
  });
}

/** The reason field. Optional, but recorded when it is given. */
function RevealForm({
  location,
  onReveal,
}: {
  location: string;
  onReveal: (reason: string) => void | Promise<void>;
}) {
  const [reason, setReason] = useState("");
  const { setConfirm } = useModalControls();

  useEffect(() => {
    setConfirm(() => onReveal(reason));
  }, [reason, onReveal, setConfirm]);

  return (
    <div>
      <p>
        <Mono>{location}</Mono>
      </p>
      <p>
        <Muted>
          <Small>
            The value is shown once and an entry is written to the audit log naming what was
            revealed and when. The value itself is never written anywhere.
          </Small>
        </Muted>
      </p>
      <Label>Reason (optional)</Label>
      <Input
        type="text"
        placeholder="ticket OPS-12"
        value={reason}
        onChange={(e) => setReason(e.target.value)}
      />
    </div>
  );
}

const Status = styled.td<{ $masked: boolean }>`
  color: ${(p) => (p.$masked ? p.theme.color.danger : p.theme.color.success)};
`;

const NotCarried = styled.span`
  font-size: 12px;
  color: ${(p) => p.theme.color.danger};
`;
