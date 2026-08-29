// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * One job: what it is, how far it got, what it said, and what can still be done
 * to it.
 */
import { memo, useEffect, useRef, type ReactNode } from "react";
import styled from "styled-components";

import { JobsAPI, type JobView, type LogLine } from "../../api";
import { useNavigate } from "../../app/ShellContext";
import { Icon } from "../../components/Icon";
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardHead,
  CardTitle,
  Log,
  LogCommand,
  Muted,
  Notice,
  Numeric,
  NumericHeader,
  ProgressBar,
  ProgressTrack,
  Row,
  Small,
  Step,
  StepLabel,
  StepMarker,
  Stepper,
  Table,
  TableScroll,
  useModal,
  type StepState,
  type Tone,
} from "../../design-system";
import { titleCase, when } from "../../utils/format";
import { ResumePassphrase } from "./ResumePassphrase";
import { logTail, type Live } from "./live";

type Modal = ReturnType<typeof useModal>;

export function JobCard({ job, live, reload }: { job: JobView; live?: Live; reload: () => void }) {
  const navigate = useNavigate();
  const modal = useModal();

  const running = job.state === "running" || job.state === "queued";
  const interrupted = job.state === "interrupted";
  const tail = logTail(job.id);
  const note = live?.note || job.message || "";

  const logRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const element = logRef.current;
    if (element) element.scrollTop = element.scrollHeight;
  }, [tail.length]);

  return (
    <Card $tone={interrupted ? "warning" : undefined}>
      <CardHead>
        <Row $gap={12}>
          <KindMark $kind={job.kind}>
            <Icon name={job.kind === "restore" ? "restore" : "capture"} />
          </KindMark>
          <Identity job={job} />
          <StatusSlot>
            <Badge $tone={stateTone(job.state)}>{stateLabel(job.state)}</Badge>
          </StatusSlot>
        </Row>
        <Row $gap={14}>
          <HeadTimes>
            {startedAt(job) ? (
              <Fact icon="clock" title="When this run began">
                <FactLabel>Started</FactLabel>
                {startedAt(job)}
              </Fact>
            ) : null}
            {elapsedLine(job) ? (
              <Muted>
                <Small>{elapsedLine(job)}</Small>
              </Muted>
            ) : null}
          </HeadTimes>
          <Actions job={job} modal={modal} reload={reload} navigate={navigate} />
        </Row>
      </CardHead>

      <CardBody>
        <Body>
          <Phases job={job} live={live} />

          <Detail>
            {running ? (
              <ProgressTrack>
                <ProgressBar $percent={live?.percent ?? 0} $warn={live?.warn} />
              </ProgressTrack>
            ) : null}

            {job.provenance?.cloneRef ? (
              <Notice
                tone={running ? "ok" : "info"}
                title={
                  running
                    ? `Ephemeral clone ${job.provenance.cloneRef} is running — the serving instance is untouched.`
                    : `Ephemeral clone ${job.provenance.cloneRef} was created and destroyed.`
                }
                body={
                  running ? "The clone is destroyed on completion, on failure, and on cancel." : ""
                }
              />
            ) : null}

            <Muted>
              <Small>{note}</Small>
            </Muted>

            {job.checkpointNote ? (
              <div>
                <Muted>
                  <Small>{job.checkpointNote}</Small>
                </Muted>
              </div>
            ) : null}

            {running || tail.length > 0 ? (
              <Log ref={logRef}>
                <LogLines lines={tail} />
              </Log>
            ) : null}

            {job.ledger && job.ledger.length > 0 ? <LedgerTable job={job} /> : null}

            {job.hint ? (
              <div style={{ marginTop: 8 }}>
                <Muted>
                  <Small>{job.hint}</Small>
                </Muted>
              </div>
            ) : null}
          </Detail>
        </Body>
      </CardBody>
    </Card>
  );
}

/**
 * What this job is, in the terms of the thing it moves.
 *
 * A capture and a restore run the same way and mean opposite things, so the
 * card says which in two ways at once — the glyph and the word — rather than
 * relying on the reader noticing one of them.
 *
 * Underneath, the facts are named by their own icons rather than strung
 * together with an arrow. An arrow only works if the reader knows which side is
 * the source, and on this card that flips with the job kind: the storage is
 * where a capture ends and where a restore begins. A row of labelled facts says
 * what each one is without asking anyone to work out the direction, and it is
 * the same row for both kinds, so the eye finds the storage in the same place
 * whichever card it landed on.
 */
function Identity({ job }: { job: JobView }) {
  const restore = job.kind === "restore";

  return (
    <IdentityBox>
      <Row $gap={8}>
        <Kind>{titleCase(job.kind)}</Kind>
        <CardTitle>{job.realm || "—"}</CardTitle>
      </Row>
      <Facts>
        {restore ? (
          <Fact icon="library" title="The snapshot being restored">
            <FactLabel>Snapshot</FactLabel>
            {job.origin?.capturedAt ? when(job.origin.capturedAt) : "—"}
          </Fact>
        ) : null}
        <Fact icon="environments" title={restore ? "Destination environment" : "Captured from"}>
          {job.environment || "—"}
        </Fact>
        <Fact icon="storage" title={restore ? "Read from" : "Written to"}>
          {job.storage || "—"}
        </Fact>
      </Facts>
    </IdentityBox>
  );
}

/**
 * One labelled fact: the glyph says which kind of thing it is, the text says
 * which one.
 */
function Fact({ icon, title, children }: { icon: string; title: string; children: ReactNode }) {
  return (
    <FactBox title={title}>
      <Icon name={icon} />
      <span>{children}</span>
    </FactBox>
  );
}

/**
 * When this run began, written out in full.
 *
 * It sits beside the elapsed time because the two answer one question between
 * them — started then, running this long — and because on a restore card it
 * would otherwise be a second full timestamp in the same row as the snapshot's,
 * inches apart and told apart only by an icon.
 *
 * The start rather than the finish: it is the fixed point. A job that is still
 * running has no finish, and one that failed finished at a time that says less
 * than when it set off.
 */
function startedAt(job: JobView): string {
  const at = job.startedAt || job.createdAt;
  return at ? when(at) : "";
}

/**
 * The run's phases, numbered, with where it has got to.
 *
 * The count is stated as well as drawn: "4 of 9" is what an operator says on a
 * call, and a column of ticks is not.
 */
function Phases({ job, live }: { job: JobView; live?: Live }) {
  const states = job.phases.map(
    (phase) =>
      live?.steps.get(phase.phase) ??
      (phase.skipped ? "skipped" : phase.done ? "done" : phase.live ? "live" : "pending"),
  );
  // Skipped counts toward the tally: the phase was reached, and the pipeline
  // got to the end. What it did not do is reach a verdict, which is what the
  // marker says rather than the count.
  const done = states.filter((state) => state === "done" || state === "skipped").length;

  return (
    <PhaseColumn>
      <PhaseCount>{`${done} of ${job.phases.length} phases`}</PhaseCount>
      <Stepper>
        {job.phases.map((phase, i) => (
          <Step key={phase.phase} $state={states[i]} $last={i === job.phases.length - 1}>
            <StepMarker $state={states[i]}>{marker(states[i], i + 1)}</StepMarker>
            <StepLabel $state={states[i]}>{phase.label}</StepLabel>
          </Step>
        ))}
      </Stepper>
    </PhaseColumn>
  );
}

/** A step shows its number until it has an answer, then it shows the answer. */
function marker(state: StepState, n: number): string {
  if (state === "done") return "✓";
  if (state === "failed") return "✕";
  // A dash, not a tick and not a cross: nothing was concluded here.
  if (state === "skipped") return "–";
  return String(n);
}

const Body = styled.div`
  display: grid;
  grid-template-columns: minmax(190px, 230px) minmax(0, 1fr);
  gap: 20px;
  align-items: start;

  /* Under about a laptop half-width there is no room for two columns, and a
     stepper squeezed to 120px wraps every label. The phases go back on top. */
  @media (max-width: 860px) {
    grid-template-columns: minmax(0, 1fr);
  }
`;

const Detail = styled.div`
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
`;

const PhaseColumn = styled.div`
  border-right: 1px solid ${(p) => p.theme.color.borderSubtle};
  padding-right: 16px;

  @media (max-width: 860px) {
    border-right: none;
    padding-right: 0;
    border-bottom: 1px solid ${(p) => p.theme.color.borderSubtle};
    padding-bottom: 12px;
  }
`;

const PhaseCount = styled.div`
  font-size: 11px;
  letter-spacing: 0.5px;
  text-transform: uppercase;
  font-weight: 600;
  color: ${(p) => p.theme.color.textSecondary};
  margin-bottom: 10px;
`;

/** The glyph tile: capture and restore are the same drawing, mirrored. */
const KindMark = styled.span<{ $kind: string }>`
  flex: none;
  width: 30px;
  height: 30px;
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: ${(p) => (p.$kind === "restore" ? p.theme.color.warningText : p.theme.color.infoText)};
  background: ${(p) => (p.$kind === "restore" ? p.theme.color.warningBg : p.theme.color.infoBg)};
  border: 1px solid
    ${(p) => (p.$kind === "restore" ? p.theme.color.warningBorder : p.theme.color.infoBorder)};
`;

const Kind = styled.span`
  font-size: 11px;
  letter-spacing: 0.6px;
  text-transform: uppercase;
  font-weight: 700;
  color: ${(p) => p.theme.color.textSecondary};
`;

const IdentityBox = styled.div`
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
`;

const Facts = styled.div`
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 14px;
`;

const FactBox = styled.span`
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  color: ${(p) => p.theme.color.textSecondary};
  white-space: nowrap;

  /* The glyphs are drawn on a 16px grid for the navigation rail; beside
     12px text they sit better a size down. */
  svg {
    width: 13px;
    height: 13px;
    flex: none;
    color: ${(p) => p.theme.color.textMuted};
  }
`;

/**
 * The word in front of a fact whose value is a bare timestamp.
 *
 * A restore card carries two of those — when the snapshot was captured and when
 * the run started — and an icon is not enough to tell them apart at a glance
 * when both were the same afternoon.
 */
const FactLabel = styled.span`
  /* The gap is here rather than in the markup: JSX drops the newline between
     this element and the timestamp after it, so without a margin the two run
     together as "SNAPSHOT28 August". */
  margin-right: 6px;
  font-size: 10px;
  letter-spacing: 0.6px;
  text-transform: uppercase;
  font-weight: 700;
  color: ${(p) => p.theme.color.textMuted};
`;

/** The two times, stacked at the right of the head: when, and how long. */
const HeadTimes = styled.div`
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 2px;
  text-align: right;
`;

/*
 * The state is the answer to a different question from the identity beside it,
 * so it is spaced as its own thing rather than reading as another word in the
 * title.
 */
const StatusSlot = styled.span`
  margin-left: 10px;
`;

function Actions({
  job,
  modal,
  reload,
  navigate,
}: {
  job: JobView;
  modal: Modal;
  reload: () => void;
  navigate: ReturnType<typeof useNavigate>;
}) {
  // Resuming can mean two very different things — repeating an upload that is
  // already sealed, or running the whole export again — so the button says
  // which before it is pressed rather than after.
  const resumeNote = job.resumeNote ?? "";
  const rerunsExport =
    resumeNote.includes("runs it again") || resumeNote.includes("runs the export again");

  return (
    <Row>
      {job.cancellable ? (
        <Button
          $variant="danger"
          onClick={async () => {
            await JobsAPI.cancel(job.id);
            reload();
          }}
        >
          Cancel
        </Button>
      ) : null}

      {job.resumable ? (
        <>
          <Button
            $variant="primary"
            title={resumeNote}
            onClick={() => {
              if (job.needsPassphrase) {
                askPassphrase(job, resumeNote, modal, reload);
                return;
              }
              if (rerunsExport) {
                confirmResume(job, resumeNote, modal, reload);
                return;
              }
              void doResume(job, modal, reload);
            }}
          >
            {rerunsExport ? "Resume (re-exports)" : "Resume"}
          </Button>
          {resumeNote ? (
            <Muted>
              <Small>{resumeNote}</Small>
            </Muted>
          ) : null}
        </>
      ) : null}

      {job.discardable ? (
        <Button onClick={() => confirmDiscard(job, modal, reload)}>Discard</Button>
      ) : null}

      {job.state === "completed" && job.kind === "capture" && job.snapshotId ? (
        <Button
          onClick={() =>
            navigate({
              name: "inspect",
              storage: job.storage ?? "",
              bundleKey: job.storageKey ?? "",
              snapshotId: job.snapshotId ?? "",
            })
          }
        >
          Inspect
        </Button>
      ) : null}
    </Row>
  );
}

/**
 * The width of a ledger column is fixed rather than left to the content.
 *
 * Two of these columns hold sentences — a classified failure runs to a
 * paragraph, and an outcome can too — and letting the browser share the width
 * out gave the longest sentence the whole card and squeezed the error into a
 * ribbon one word wide. Naming the widths is what keeps the narrow columns
 * narrow and gives the prose the room it needs.
 */
const Ledger = styled(Table)`
  table-layout: fixed;

  tbody td {
    /* A classified failure and a Keycloak error are both prose, and prose in a
       fixed cell has to be allowed to wrap — including mid-word, for the
       identifiers and paths that have no spaces to break at. */
    white-space: normal;
    overflow-wrap: anywhere;
  }
`;

/**
 * Which outcomes are a pill and which are a sentence.
 *
 * A badge is a label: "failed", "warning", "left behind". The restore path also
 * writes a whole explanation into the same field — "the import ran and did not
 * finish; Keycloak's import is not transactional…" — and a pill cannot hold
 * that. It does not wrap, so it grew until it left the card.
 */
const maxBadgeLength = 24;

function LedgerTable({ job }: { job: JobView }) {
  return (
    <TableScroll style={{ marginTop: 12 }}>
      <Ledger>
        <colgroup>
          <col style={{ width: "9%" }} />
          <col style={{ width: "16%" }} />
          <col style={{ width: "8%" }} />
          <col style={{ width: "37%" }} />
          <col style={{ width: "30%" }} />
        </colgroup>
        <thead>
          <tr>
            <th>Phase</th>
            <th>Item</th>
            <NumericHeader>Attempts</NumericHeader>
            <th>Last error</th>
            <th>Outcome</th>
          </tr>
        </thead>
        <tbody>
          {(job.ledger ?? []).map((row, i) => (
            <tr key={i}>
              <td>{row.phase}</td>
              <td>{row.item ?? "—"}</td>
              <Numeric>{row.attempts}</Numeric>
              <td>
                <Muted>
                  <Small>{row.lastError ?? "—"}</Small>
                </Muted>
              </td>
              <td>
                <Outcome row={row} />
              </td>
            </tr>
          ))}
        </tbody>
      </Ledger>
    </TableScroll>
  );
}

function Outcome({ row }: { row: NonNullable<JobView["ledger"]>[number] }) {
  const tone = row.retryable ? "warn" : row.outcome.includes("destroy") ? "ok" : "neutral";
  if (row.outcome.length > maxBadgeLength) {
    return (
      <Muted>
        <Small>{row.outcome}</Small>
      </Muted>
    );
  }
  return <Badge $tone={tone}>{row.outcome}</Badge>;
}

/**
 * The log rows, drawn only when the log has actually changed.
 *
 * The screen repaints ten times a second while a job is talking, and a tail
 * runs to ten thousand lines. Without this the whole list is reconciled on
 * every one of those repaints, including the ones that only moved a progress
 * bar. `logTail` hands back the same array until the tail changes, which is
 * what makes this comparison work.
 */
const LogLines = memo(function LogLines({ lines }: { lines: LogLine[] }) {
  return (
    <>
      {lines.map((line, i) =>
        line.fromPortCloak ? (
          <LogCommand key={i}>{line.text}</LogCommand>
        ) : (
          <div key={i}>{line.text}</div>
        ),
      )}
    </>
  );
});

function elapsedLine(job: JobView): string {
  // The phase count moved to the stepper, which is where it is read from now.
  // Repeating it here would be the same number twice in one card.
  return job.elapsed ? `${job.elapsed} elapsed` : "";
}

function stateTone(state: string): Tone {
  switch (state) {
    case "running":
    case "queued":
      return "info";
    case "completed":
      return "ok";
    case "interrupted":
      return "warn";
    case "failed":
      return "danger";
    default:
      return "neutral";
  }
}

function stateLabel(state: string): string {
  return state === "completed"
    ? "Completed"
    : state === "interrupted"
      ? "Interrupted"
      : state === "failed"
        ? "Failed"
        : titleCase(state);
}

async function doResume(
  job: JobView,
  modal: Modal,
  reload: () => void,
  passphrase = "",
): Promise<void> {
  const result = await JobsAPI.resume(job.id, passphrase);
  if (result.failure) {
    modal.open({
      title: "This job was not resumed",
      body: <FailureBody message={result.failure.message} hint={result.failure.hint} />,
      cancelLabel: "Close",
    });
    return;
  }
  reload();
}

function FailureBody({ message, hint }: { message: string; hint?: string }) {
  return (
    <div>
      <div style={{ fontWeight: 600 }}>{message}</div>
      {hint ? <div>{hint}</div> : null}
    </div>
  );
}

/**
 * A capture sealed with a passphrase has to be given it again.
 *
 * The mode and the recipients are on the job and are rebuilt without asking;
 * the passphrase is not, because nothing sensitive is written to a job file.
 * Asking is the cost of that, and it is asked here rather than discovered from
 * a rejected resume.
 */
function askPassphrase(job: JobView, note: string, modal: Modal, reload: () => void): void {
  modal.open({
    title: "The passphrase this capture was sealed with",
    body: (
      <ResumePassphrase
        note={note}
        onResume={(passphrase) => doResume(job, modal, reload, passphrase)}
      />
    ),
    confirmLabel: "Resume",
    confirmDisabled: true,
  });
}

// Resuming a job whose export never finished re-reads the realm out of the
// database. That is the expensive half and it touches the source environment
// again, so it is confirmed rather than assumed — unlike repeating an upload
// from a bundle that is already sealed on this machine.
function confirmResume(job: JobView, note: string, modal: Modal, reload: () => void): void {
  modal.open({
    title: `Resume this ${job.kind}?`,
    body: (
      <div>
        <p>{note}</p>
        <p>
          <Muted>
            <Small>
              The export runs against the source environment again. Nothing already stored is
              changed.
            </Small>
          </Muted>
        </p>
      </div>
    ),
    confirmLabel: "Run the export again",
    onConfirm: () => doResume(job, modal, reload),
  });
}

function confirmDiscard(job: JobView, modal: Modal, reload: () => void): void {
  modal.open({
    title: `Discard this ${job.kind}?`,
    body: (
      <div>
        <p>
          PortCloak will abort any incomplete upload on the storage side, remove the local
          checkpoint and any partial bundle, and record the discard.
        </p>
        <p>
          <Muted>
            <Small>
              Nothing already stored is touched. This job will not be resumable afterwards.
            </Small>
          </Muted>
        </p>
      </div>
    ),
    confirmLabel: "Discard",
    confirmTone: "danger-solid",
    onConfirm: async () => {
      const result = await JobsAPI.discard(job.id);
      if (result.failure) {
        modal.open({ title: "Not discarded", body: <div>{result.failure.message}</div> });
        return;
      }
      reload();
    },
  });
}
