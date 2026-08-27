// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * One job: what it is, how far it got, what it said, and what can still be done
 * to it.
 */
import { useEffect, useRef } from "react";

import { JobsAPI, type JobView } from "../../api";
import { useNavigate } from "../../app/ShellContext";
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
  Pipeline,
  PipelineStep,
  ProgressBar,
  ProgressTrack,
  Row,
  Small,
  Table,
  TableScroll,
  useModal,
  type Tone,
} from "../../design-system";
import { titleCase } from "../../utils/format";
import { ResumePassphrase } from "./ResumePassphrase";
import { logTails, type Live } from "./live";

type Modal = ReturnType<typeof useModal>;

export function JobCard({
  job,
  live,
  reload,
}: {
  job: JobView;
  live?: Live;
  reload: () => void;
}) {
  const navigate = useNavigate();
  const modal = useModal();

  const running = job.state === "running" || job.state === "queued";
  const interrupted = job.state === "interrupted";
  const tail = logTails.get(job.id) ?? [];
  const note = live?.note || job.message || "";

  const logRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const element = logRef.current;
    if (element) element.scrollTop = element.scrollHeight;
  }, [tail.length]);

  return (
    <Card $tone={interrupted ? "warning" : undefined}>
      <CardHead>
        <Row>
          <CardTitle>{`${titleCase(job.kind)} · ${job.realm ?? ""}`}</CardTitle>
          {job.storage ? (
            <Muted>
              <Small>{`→ ${job.storage}`}</Small>
            </Muted>
          ) : null}
          <Badge $tone={stateTone(job.state)}>{stateLabel(job.state)}</Badge>
        </Row>
        <Row>
          <Muted>
            <Small>{elapsedLine(job)}</Small>
          </Muted>
          <Actions job={job} modal={modal} reload={reload} navigate={navigate} />
        </Row>
      </CardHead>

      <CardBody>
        {running ? (
          <ProgressTrack>
            <ProgressBar $percent={live?.percent ?? 0} $warn={live?.warn} />
          </ProgressTrack>
        ) : null}

        <Pipeline>
          {job.phases.map((phase) => {
            const state = live?.steps.get(phase.phase) ?? (phase.done ? "done" : phase.live ? "live" : "pending");
            return (
              <PipelineStep key={phase.phase} $state={state}>
                <span>{glyph(state)}</span>
                {phase.label}
              </PipelineStep>
            );
          })}
        </Pipeline>

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
            {tail.map((line, i) =>
              line.fromPortCloak ? (
                <LogCommand key={i}>{line.text}</LogCommand>
              ) : (
                <div key={i}>{line.text}</div>
              ),
            )}
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
      </CardBody>
    </Card>
  );
}

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

function LedgerTable({ job }: { job: JobView }) {
  return (
    <TableScroll style={{ marginTop: 12 }}>
      <Table>
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
                <Badge
                  $tone={
                    row.retryable ? "warn" : row.outcome.includes("destroy") ? "ok" : "neutral"
                  }
                >
                  {row.outcome}
                </Badge>
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
    </TableScroll>
  );
}

function elapsedLine(job: JobView): string {
  const done = job.phases.filter((phase) => phase.done).length;
  return `${done} of ${job.phases.length} phases${job.elapsed ? ` · ${job.elapsed} elapsed` : ""}`;
}

function glyph(state: "pending" | "done" | "live" | "failed"): string {
  switch (state) {
    case "done":
      return "✓";
    case "live":
      return "●";
    case "failed":
      return "✕";
    default:
      return "○";
  }
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
