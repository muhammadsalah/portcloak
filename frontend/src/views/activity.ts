import { JobsAPI, type ActivityView, type JobView, type ProgressEvent } from "../api";
import { badge, clear, failureNotice, h, modal, notice, setModalConfirmDisabled, spinner } from "../dom";
import { navigate, subscribeProgress } from "../main";

/**
 * The Activity screen is where resilience becomes legible. A dropped
 * connection has to look like a wait with a reason, not a hang.
 *
 * That only holds if the screen keeps up with the run. It used to draw itself
 * once and then patch three elements from the event stream: the phase pipeline,
 * the state badge, the elapsed time and the ledger were all frozen at whatever
 * they were when the screen opened, so a capture that had finished still looked
 * like one that was stuck, and the only way to see the truth was to navigate
 * away and back. Everything below exists to make that unnecessary:
 *
 *   · the event stream patches what it can reach immediately, so a streamed log
 *     line and a phase tick appear the instant they happen;
 *   · anything structural — a phase boundary, a job changing state — asks the
 *     engine for the job list again, coalesced so a burst costs one call;
 *   · a slow poll runs while anything is in flight, so a missed event is a
 *     second of staleness rather than a permanently wrong screen;
 *   · and a repaint only happens when something actually changed, so a screen
 *     that is merely ticking does not rebuild itself under the operator.
 */

/** Teardown for the render currently on screen: listener, timers, observer. */
let teardown: (() => void) | null = null;

/**
 * Streamed output per job, kept outside the render so a repaint does not throw
 * away what the export has already said. Bounded, because a large export talks
 * a lot and this is a log tail, not a log file.
 */
const logLines = new Map<string, string[]>();
const maxLogLines = 500;

/** The last painted shape, so an unchanged view is not rebuilt. */
let painted = "";

/** How long a burst of events is allowed to coalesce into one refresh. */
const coalesceMs = 400;
/** How often the screen re-reads the job list while anything is in flight. */
const pollMs = 2000;

export async function renderActivity(root: HTMLElement): Promise<void> {
  teardown?.();
  teardown = null;
  painted = "";

  clear(root);
  root.appendChild(spinner("Reading jobs…"));

  let view: ActivityView;
  try {
    view = await JobsAPI.list();
  } catch (err) {
    clear(root);
    root.appendChild(notice("danger", "The job list could not be read.", String(err)));
    return;
  }

  // Live handles for the job cards currently on screen, rebuilt on every paint.
  const live = new Map<string, LiveCard>();
  let disposed = false;

  const reload = async (): Promise<void> => {
    if (disposed) return;
    try {
      paint(await JobsAPI.list());
    } catch {
      // A single failed refresh is not worth replacing a screen that is still
      // showing the truth as of a moment ago. The poll will try again.
    }
  };

  let pending: number | undefined;
  const scheduleReload = (): void => {
    if (disposed || pending !== undefined) return;
    pending = window.setTimeout(() => {
      pending = undefined;
      void reload();
    }, coalesceMs);
  };

  const paint = (next: ActivityView): void => {
    if (disposed) return;
    const shape = shapeOf(next);
    if (shape === painted) {
      // Nothing structural moved. The clock still has to, or a running job
      // looks frozen for as long as it runs.
      for (const job of next.jobs) {
        const card = live.get(job.id);
        if (card) card.elapsed.textContent = elapsedLine(job);
      }
      return;
    }
    painted = shape;

    live.clear();
    clear(root);
    root.appendChild(header(next));
    if (next.jobs.length === 0) {
      root.appendChild(
        notice(
          "info",
          "Nothing has run yet",
          "Captures and restores appear here while they run, and stay afterwards with what they did.",
        ),
      );
      return;
    }
    for (const job of next.jobs) {
      root.appendChild(card(job, live, () => void reload()));
    }

    // A job that is no longer listed — discarded, purged — takes its buffered
    // output with it.
    const listed = new Set(next.jobs.map((j) => j.id));
    for (const id of logLines.keys()) {
      if (!listed.has(id)) logLines.delete(id);
    }
  };

  paint(view);

  const unsubscribe = subscribeProgress((e: ProgressEvent) => {
    if (e.message && e.kind === "log") remember(e.jobId, e.message);
    const target = live.get(e.jobId);
    if (target) apply(target, e);
    // A phase boundary or a state change alters the pipeline, the badge, the
    // buttons and the ledger — all of which come from the engine rather than
    // from the event. Ask for them rather than half-deriving them here.
    if (structural(e.kind)) scheduleReload();
  });

  const poll = window.setInterval(() => {
    if (live.size > 0) void reload();
  }, pollMs);

  // The listener and the timers belong to this render. Navigating away must not
  // leave one writing into a detached node, and — the older failure — must not
  // leave the previous one subscribed alongside the new one either.
  const dispose = (): void => {
    disposed = true;
    unsubscribe();
    observer.disconnect();
    window.clearInterval(poll);
    if (pending !== undefined) window.clearTimeout(pending);
    // Only if it is still ours. A newer render owns the slot by then, and
    // clearing it would leave that one with nothing to tear down.
    if (teardown === dispose) teardown = null;
  };
  const observer = new MutationObserver(() => {
    if (!document.body.contains(root)) dispose();
  });
  observer.observe(document.body, { childList: true, subtree: true });

  teardown = dispose;
}

/** The handles on one card that the event stream can write into directly. */
interface LiveCard {
  log: HTMLElement;
  bar: HTMLElement;
  note: HTMLElement;
  elapsed: HTMLElement;
  steps: Map<string, HTMLElement>;
}

/**
 * The shape a repaint depends on. Elapsed time is deliberately excluded: it
 * changes every second and needs a text update, not a rebuilt screen.
 */
function shapeOf(view: ActivityView): string {
  return JSON.stringify({
    summary: view.summary,
    jobs: view.jobs.map((j) => ({
      id: j.id,
      state: j.state,
      message: j.message,
      hint: j.hint,
      phase: j.phases.map((p) => (p.done ? "d" : p.live ? "l" : "-")).join(""),
      ledger: j.ledger?.length ?? 0,
      resumable: j.resumable,
      discardable: j.discardable,
      cancellable: j.cancellable,
      checkpoint: j.checkpointNote,
      clone: j.provenance?.cloneRef,
    })),
  });
}

/** Kinds that change what the engine would say about a job, not just its text. */
function structural(kind: string): boolean {
  return (
    kind === "phaseStarted" ||
    kind === "phaseCompleted" ||
    kind === "phaseFailed" ||
    kind === "jobState" ||
    kind === "cloneCreated" ||
    kind === "cloneDestroyed"
  );
}

function remember(jobId: string, line: string): void {
  const lines = logLines.get(jobId) ?? [];
  lines.push(line);
  if (lines.length > maxLogLines) lines.splice(0, lines.length - maxLogLines);
  logLines.set(jobId, lines);
}

function header(view: ActivityView): HTMLElement {
  return h(
    "div",
    null,
    h("h1", { class: "page-title" }, "Activity"),
    h("div", { class: "page-subtitle" }, view.summary),
  );
}

function elapsedLine(job: JobView): string {
  const done = job.phases.filter((p) => p.done).length;
  return `${done} of ${job.phases.length} phases${job.elapsed ? ` · ${job.elapsed} elapsed` : ""}`;
}

function apply(target: LiveCard, e: ProgressEvent): void {
  switch (e.kind) {
    case "log":
      if (e.message) {
        target.log.appendChild(h("div", null, e.message));
        target.log.scrollTop = target.log.scrollHeight;
      }
      break;
    case "progress":
      if (e.total && e.total > 0 && e.current !== undefined) {
        const pct = Math.min(100, Math.round((e.current / e.total) * 100));
        target.bar.style.width = `${pct}%`;
        target.bar.classList.remove("warn");
        target.note.textContent = `${pct}% · ${e.item ?? ""}`;
      } else if (e.current !== undefined) {
        target.note.textContent = `${e.current.toLocaleString()} ${e.unit ?? ""} · ${e.item ?? ""}`;
      }
      break;
    case "retry":
      target.bar.classList.add("warn");
      target.note.textContent = `Attempt ${e.attempt} failed — retrying in ${Math.round((e.retryIn ?? 0) / 1e9)}s. ${e.message ?? ""}`;
      break;
    case "breakerOpen":
      target.bar.classList.add("warn");
      target.note.textContent = `Paused — ${e.item} has been unreachable. Retrying in ${Math.round((e.retryIn ?? 0) / 1e9)}s. Nothing is lost.`;
      break;
    case "phaseStarted":
      target.note.textContent = e.label ?? e.phase ?? "";
      // Moved here rather than waiting for the refresh: the tick is the one
      // piece of feedback that has to feel immediate.
      markStep(target, e.phase, "live");
      break;
    case "phaseCompleted":
      markStep(target, e.phase, "done");
      break;
    case "phaseFailed":
      markStep(target, e.phase, "failed");
      target.bar.classList.add("warn");
      if (e.message) target.note.textContent = e.message;
      break;
    case "cloneCreated":
      target.log.appendChild(h("div", { class: "cmd" }, `Ephemeral clone ${e.item} is running.`));
      break;
    case "cloneDestroyed":
      target.log.appendChild(h("div", { class: "cmd" }, `Ephemeral clone ${e.item} destroyed.`));
      break;
  }
}

function markStep(target: LiveCard, phase: string | undefined, state: "live" | "done" | "failed"): void {
  if (!phase) return;
  const step = target.steps.get(phase);
  if (!step) return;
  if (state === "live") {
    for (const other of target.steps.values()) other.classList.remove("live");
    step.classList.add("live");
    setGlyph(step, "●");
    return;
  }
  step.classList.remove("live");
  if (state === "done") {
    step.classList.add("done");
    setGlyph(step, "✓");
  } else {
    step.classList.add("failed");
    setGlyph(step, "✕");
  }
}

function setGlyph(step: HTMLElement, glyph: string): void {
  const mark = step.firstElementChild;
  if (mark) mark.textContent = glyph;
}

function card(job: JobView, live: Map<string, LiveCard>, reload: () => void): HTMLElement {
  const running = job.state === "running" || job.state === "queued";
  const interrupted = job.state === "interrupted";

  const bar = h("div", { class: "progress-bar", style: "width:0%" });
  const note = h("div", { class: "muted small" }, job.message ?? "");
  const log = h("div", { class: "log" });
  const elapsed = h("span", { class: "muted small" }, elapsedLine(job));

  const pipeline = h("div", { class: "pipeline" });
  const steps = new Map<string, HTMLElement>();
  for (const p of job.phases) {
    const step = h(
      "div",
      { class: `pipeline-step ${p.done ? "done" : ""} ${p.live ? "live" : ""}` },
      h("span", null, p.done ? "✓" : p.live ? "●" : "○"),
      p.label,
    );
    steps.set(p.phase, step);
    pipeline.appendChild(step);
  }

  // The tail this job has produced survives the repaint that brought this card
  // back — and survives the job finishing, so the last thing the export said is
  // still on screen at the moment it matters most.
  const tail = logLines.get(job.id) ?? [];
  for (const line of tail) {
    log.appendChild(h("div", null, line));
  }
  if (running) {
    live.set(job.id, { log, bar, note, elapsed, steps });
  }
  if (tail.length > 0) {
    // Appending happens before the node is in the document, so the scroll has
    // to be asked for once it is.
    window.setTimeout(() => {
      log.scrollTop = log.scrollHeight;
    }, 0);
  }

  const actions = h("div", { class: "row" });
  if (job.cancellable) {
    actions.appendChild(
      h(
        "button",
        {
          class: "danger",
          onClick: async () => {
            await JobsAPI.cancel(job.id);
            reload();
          },
        },
        "Cancel",
      ),
    );
  }
  if (job.resumable) {
    // Resuming can mean two very different things — repeating an upload that
    // is already sealed, or running the whole export again — so the button
    // says which before it is pressed rather than after.
    const resumeNote = job.resumeNote ?? "";
    const rerunsExport =
      resumeNote.includes("runs it again") || resumeNote.includes("runs the export again");
    actions.appendChild(
      h(
        "button",
        {
          class: "primary",
          title: resumeNote,
          onClick: () => {
            if (job.needsPassphrase) {
              askPassphrase(job, resumeNote, reload);
              return;
            }
            if (rerunsExport) {
              confirmResume(job, resumeNote, reload);
              return;
            }
            void doResume(job, reload);
          },
        },
        rerunsExport ? "Resume (re-exports)" : "Resume",
      ),
    );
    if (resumeNote) {
      actions.appendChild(h("span", { class: "muted note" }, resumeNote));
    }
  }
  if (job.discardable) {
    actions.appendChild(
      h("button", { onClick: () => confirmDiscard(job, reload) }, "Discard"),
    );
  }
  if (job.state === "completed" && job.kind === "capture" && job.snapshotId) {
    actions.appendChild(
      h(
        "button",
        {
          onClick: () =>
            navigate({
              name: "inspect",
              storage: job.storage ?? "",
              bundleKey: job.storageKey ?? "",
              snapshotId: job.snapshotId ?? "",
            }),
        },
        "Inspect",
      ),
    );
  }

  const body = h(
    "div",
    { class: "card-body" },
    running ? h("div", { class: "progress" }, bar) : null,
    pipeline,
    job.provenance?.cloneRef
      ? notice(
          running ? "ok" : "info",
          running
            ? `Ephemeral clone ${job.provenance.cloneRef} is running — the serving instance is untouched.`
            : `Ephemeral clone ${job.provenance.cloneRef} was created and destroyed.`,
          running
            ? "The clone is destroyed on completion, on failure, and on cancel."
            : "",
        )
      : null,
    note,
    job.checkpointNote ? h("div", { class: "muted small" }, job.checkpointNote) : null,
    running || tail.length > 0 ? log : null,
    job.ledger && job.ledger.length > 0 ? ledgerTable(job) : null,
    job.hint ? h("div", { class: "muted small", style: "margin-top:8px" }, job.hint) : null,
  );

  return h(
    "div",
    { class: "card", style: interrupted ? "border-color:var(--warning)" : "" },
    h(
      "div",
      { class: "card-head" },
      h(
        "div",
        { class: "row" },
        h("span", { class: "card-title" }, `${titleCase(job.kind)} · ${job.realm ?? ""}`),
        job.storage ? h("span", { class: "muted small" }, `→ ${job.storage}`) : null,
        stateBadge(job.state),
      ),
      h("div", { class: "row" }, elapsed, actions),
    ),
    body,
  );
}

function ledgerTable(job: JobView): HTMLElement {
  const tbody = h("tbody");
  for (const row of job.ledger ?? []) {
    tbody.appendChild(
      h(
        "tr",
        null,
        h("td", null, row.phase),
        h("td", null, row.item ?? "—"),
        h("td", { class: "numeric" }, String(row.attempts)),
        h("td", { class: "small muted" }, row.lastError ?? "—"),
        h(
          "td",
          null,
          badge(row.outcome, row.retryable ? "warn" : row.outcome.includes("destroy") ? "ok" : "neutral"),
        ),
      ),
    );
  }
  return h(
    "div",
    { class: "table-scroll", style: "margin-top:12px" },
    h(
      "table",
      null,
      h(
        "thead",
        null,
        h(
          "tr",
          null,
          h("th", null, "Phase"),
          h("th", null, "Item"),
          h("th", { class: "numeric" }, "Attempts"),
          h("th", null, "Last error"),
          h("th", null, "Outcome"),
        ),
      ),
      tbody,
    ),
  );
}

function stateBadge(state: string): HTMLElement {
  switch (state) {
    case "running":
    case "queued":
      return badge(titleCase(state), "info");
    case "completed":
      return badge("Completed", "ok");
    case "interrupted":
      return badge("Interrupted", "warn");
    case "failed":
      return badge("Failed", "danger");
    default:
      return badge(titleCase(state), "neutral");
  }
}

function titleCase(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

async function doResume(job: JobView, reload: () => void, passphrase = ""): Promise<void> {
  const res = await JobsAPI.resume(job.id, passphrase);
  if (res.failure) {
    modal({
      title: "This job was not resumed",
      body: failureNotice(res.failure),
      cancelLabel: "Close",
    });
    return;
  }
  reload();
}

/**
 * A capture sealed with a passphrase has to be given it again.
 *
 * The mode and the recipients are on the job and are rebuilt without asking;
 * the passphrase is not, because nothing sensitive is written to a job file.
 * Asking is the cost of that, and it is asked here rather than discovered from
 * a rejected resume.
 */
function askPassphrase(job: JobView, note: string, reload: () => void): void {
  let passphrase = "";
  modal({
    title: "The passphrase this capture was sealed with",
    body: h(
      "div",
      null,
      h(
        "div",
        { class: "field" },
        h("label", null, "Passphrase"),
        h("input", {
          type: "password",
          placeholder: "passphrase",
          onInput: (e: Event) => {
            passphrase = (e.target as HTMLInputElement).value;
            setModalConfirmDisabled(passphrase === "");
          },
        }),
        h(
          "div",
          { class: "field-hint" },
          "PortCloak does not keep it. Sealing the resumed snapshot with a different one would produce a second bundle nobody could tell apart from the first.",
        ),
      ),
      note ? h("p", { class: "muted small" }, note) : null,
    ),
    confirmLabel: "Resume",
    confirmDisabled: true,
    onConfirm: () => doResume(job, reload, passphrase),
  });
}

// Resuming a job whose export never finished re-reads the realm out of the
// database. That is the expensive half and it touches the source environment
// again, so it is confirmed rather than assumed — unlike repeating an upload
// from a bundle that is already sealed on this machine.
function confirmResume(job: JobView, note: string, reload: () => void): void {
  modal({
    title: `Resume this ${job.kind}?`,
    body: h(
      "div",
      null,
      h("p", null, note),
      h(
        "p",
        { class: "muted small" },
        "The export runs against the source environment again. Nothing already stored is changed.",
      ),
    ),
    confirmLabel: "Run the export again",
    onConfirm: () => doResume(job, reload),
  });
}

function confirmDiscard(job: JobView, reload: () => void): void {
  modal({
    title: `Discard this ${job.kind}?`,
    body: h(
      "div",
      null,
      h(
        "p",
        null,
        "PortCloak will abort any incomplete upload on the storage side, remove the local checkpoint and any partial bundle, and record the discard.",
      ),
      h(
        "p",
        { class: "muted small" },
        "Nothing already stored is touched. This job will not be resumable afterwards.",
      ),
    ),
    confirmLabel: "Discard",
    confirmTone: "danger-solid",
    onConfirm: async () => {
      const res = await JobsAPI.discard(job.id);
      if (res.failure) {
        modal({ title: "Not discarded", body: h("div", null, res.failure.message) });
        return;
      }
      reload();
    },
  });
}
