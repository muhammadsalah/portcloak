// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The audit log, and nothing else.
 *
 * It used to share a screen with the maintenance panels — the configuration
 * file, the orphan sweep, the purge — which put four buttons that change things
 * next to a record of what has already happened. The panels are on Settings
 * now. What is here is read-only by design: filtered, never edited, never
 * cleared from the app.
 */
import { useState } from "react";
import styled from "styled-components";

import { AuditAPI, type AuditEntry } from "../../api";
import {
  Card,
  CardBody,
  CardFoot,
  CardHead,
  CardTitle,
  Dot,
  FailureNotice,
  Grow,
  Mono,
  Muted,
  PageSubtitle,
  PageTitle,
  Row,
  Select,
  Small,
  Spinner,
  type Tone,
} from "../../design-system";
import { useAsync } from "../../hooks/useAsync";
import { stamp } from "../../utils/format";

const actions = [
  { value: "", label: "All actions" },
  { value: "capture", label: "Captures" },
  { value: "restore", label: "Restores" },
  { value: "secretReveal", label: "Secret reveals" },
  { value: "exportView", label: "Exports" },
  { value: "snapshotDelete", label: "Deletions" },
  { value: "encryptionDeclined", label: "Encryption declined" },
  { value: "orphanRemoved", label: "Orphans removed" },
  { value: "purgeLocalData", label: "Purges" },
  { value: "homeMoved", label: "Folder moves" },
  { value: "keyCreated", label: "Keys created" },
  { value: "keyDeleted", label: "Keys deleted" },
];

const ranges = [
  { value: "0", label: "All time" },
  { value: "1", label: "Last 24 hours" },
  { value: "7", label: "Last 7 days" },
  { value: "30", label: "Last 30 days" },
  { value: "90", label: "Last 90 days" },
];

export function AuditPage() {
  const [action, setAction] = useState("");
  const [days, setDays] = useState(0);

  const { state } = useAsync(() => AuditAPI.entries(action, days), [action, days]);

  if (state.status === "failed") throw state.error;

  return (
    <div>
      <PageTitle>Audit log</PageTitle>
      <PageSubtitle>
        Everything PortCloak has done, in the order it did it. Append-only, and never cleared from
        here.
      </PageSubtitle>

      {state.status === "loading" ? (
        <Spinner>Reading the audit log…</Spinner>
      ) : state.value.failure ? (
        <FailureNotice failure={state.value.failure} />
      ) : (
        <Card>
          <CardHead>
            <CardTitle>
              {`${state.value.entries.length} entr${state.value.entries.length === 1 ? "y" : "ies"}`}
            </CardTitle>
            <Row>
              <Select
                aria-label="Filter by action"
                value={action}
                onChange={setAction}
                options={actions}
              />
              <Select
                aria-label="Filter by period"
                value={String(days)}
                onChange={(range) => setDays(Number(range))}
                options={ranges.map((r) => ({ value: String(r.value), label: r.label }))}
              />
              <Muted>
                <Mono>{state.value.path}</Mono>
              </Muted>
            </Row>
          </CardHead>

          <CardBody>
            {state.value.entries.length === 0 ? (
              <Muted>
                {action || days ? "Nothing matches that filter." : "Nothing has been recorded yet."}
              </Muted>
            ) : (
              state.value.entries.map((entry, i) => <AuditRow key={i} entry={entry} />)
            )}
          </CardBody>

          <CardFoot $muted>{state.value.note}</CardFoot>
        </Card>
      )}
    </div>
  );
}

function AuditRow({ entry }: { entry: AuditEntry }) {
  return (
    <RowBox>
      {/*
        The full stamp leads the row: an audit entry is read to answer "when
        exactly, and in which zone", and that answer should not be split across
        two columns at opposite ends of the line.
      */}
      <Time>{stamp(entry.at)}</Time>
      <TopDot $tone={toneFor(entry.action, entry.outcome)} />
      <Grow>
        <div>{describe(entry)}</div>
        <Muted>
          <Small>
            {[entry.snapshotId, entry.environment, entry.storage, entry.detail]
              .filter(Boolean)
              .join(" · ")}
          </Small>
        </Muted>
        {entry.reason ? (
          <div>
            <Muted>
              <Small>{`Reason: ${entry.reason}`}</Small>
            </Muted>
          </div>
        ) : null}
      </Grow>
    </RowBox>
  );
}

function describe(e: AuditEntry): string {
  switch (e.action) {
    case "capture":
      return `Captured ${e.realm ?? "a realm"} from ${e.environment ?? "an environment"}`;
    case "restore":
      return e.outcome === "restored"
        ? `Restored ${e.realm ?? "a realm"} into ${e.environment ?? "an environment"}`
        : `Restore of ${e.realm ?? "a realm"} into ${e.environment ?? "an environment"} did not finish`;
    case "secretReveal":
      return "Revealed a secret";
    case "exportView":
      return "Exported an inspection view";
    case "snapshotDelete":
      return "Deleted a snapshot";
    case "encryptionDeclined":
      return `Wrote ${e.realm ?? "a snapshot"} UNENCRYPTED`;
    case "orphanRemoved":
      return "Removed an orphaned ephemeral clone";
    case "purgeLocalData":
      return "Purged local working data";
    case "homeMoved":
      return "Moved the folder PortCloak keeps its files in";
    case "verifySnapshot":
      return e.outcome === "verified" ? "Verified a snapshot" : "A snapshot failed verification";
    case "jobDiscarded":
      return "Discarded an interrupted job";
    case "keyCreated":
      return "Created an encryption key";
    case "keyImported":
      return "Imported an encryption key";
    case "keyRevealed":
      return "Revealed the secret half of an encryption key";
    case "keyDeleted":
      return "DELETED an encryption key";
    default:
      return `${e.action} — ${e.outcome}`;
  }
}

function toneFor(action: string, outcome: string): Tone {
  if (action === "encryptionDeclined") return "danger";
  if (outcome.includes("not finish") || outcome.includes("failed")) return "danger";
  if (action === "keyDeleted") return "danger";
  if (action === "secretReveal" || action === "exportView" || action === "keyRevealed") {
    return "warn";
  }
  if (action === "snapshotDelete" || action === "jobDiscarded") return "neutral";
  return "ok";
}

/** One line of the audit log: time, tone, what happened. */
const RowBox = styled.div`
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 8px 0;
  border-bottom: 1px solid ${(p) => p.theme.color.borderSubtle};

  &:last-child {
    border-bottom: none;
  }
`;

const Time = styled.div`
  /* Wide enough for the longest form Intl produces for a full stamp — a
     four-digit year, seconds, and a named zone like "GMT+3". Fixed rather
     than content-sized so every row's action text starts on the same line. */
  width: 210px;
  flex: none;
  /* Digits of equal width, so the column reads as a column and the eye can
     scan down it for a time without the figures shifting under it. */
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  font-size: 12px;
  color: ${(p) => p.theme.color.textSecondary};
`;

const TopDot = styled(Dot)`
  margin-top: 6px;
`;
