import { JobsAPI, type ActivityView, type JobView, type ProgressEvent } from "../api";
import { badge, clear, h, modal, notice, spinner } from "../dom";
import { navigate, subscribeProgress } from "../main";

/**
 * The Activity screen is where resilience becomes legible. A dropped
 * connection has to look like a wait with a reason, not a hang.
 */
export async function renderActivity(root: HTMLElement): Promise<void> {
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

  clear(root);
  root.appendChild(
    h(
      "div",
      null,
      h("h1", { class: "page-title" }, "Activity"),
      h("div", { class: "page-subtitle" }, view.summary),
    ),
  );

  if (view.jobs.length === 0) {
    root.appendChild(
      notice(
        "info",
        "Nothing has run yet",
        "Captures and restores appear here while they run, and stay afterwards with what they did.",
      ),
    );
    return;
  }

  // Live output per job, so a running export streams rather than appearing at
  // the end.
  const live = new Map<string, { log: HTMLElement; bar: HTMLElement; note: HTMLElement }>();

  for (const job of view.jobs) {
    root.appendChild(card(job, live, () => void renderActivity(root)));
  }

  const unsubscribe = subscribeProgress((e: ProgressEvent) => {
    const target = live.get(e.jobId);
    if (!target) return;
    apply(target, e);
  });

  // The listener belongs to this render. Navigating away must not leave one
  // writing into a detached node.
  const observer = new MutationObserver(() => {
    if (!document.body.contains(root) || root.childElementCount === 0) {
      unsubscribe();
      observer.disconnect();
    }
  });
  observer.observe(document.body, { childList: true, subtree: true });
}

function apply(
  target: { log: HTMLElement; bar: HTMLElement; note: HTMLElement },
  e: ProgressEvent,
): void {
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
        (target.bar as HTMLElement).style.width = `${pct}%`;
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
      break;
    case "cloneCreated":
      target.log.appendChild(h("div", { class: "cmd" }, `Ephemeral clone ${e.item} is running.`));
      break;
    case "cloneDestroyed":
      target.log.appendChild(h("div", { class: "cmd" }, `Ephemeral clone ${e.item} destroyed.`));
      break;
  }
}

function card(
  job: JobView,
  live: Map<string, { log: HTMLElement; bar: HTMLElement; note: HTMLElement }>,
  reload: () => void,
): HTMLElement {
  const running = job.state === "running" || job.state === "queued";
  const interrupted = job.state === "interrupted";

  const bar = h("div", { class: "progress-bar", style: "width:0%" });
  const note = h("div", { class: "muted small" }, job.message ?? "");
  const log = h("div", { class: "log" });
  if (running) live.set(job.id, { log, bar, note });

  const pipeline = h("div", { class: "pipeline" });
  for (const p of job.phases) {
    pipeline.appendChild(
      h(
        "div",
        { class: `pipeline-step ${p.done ? "done" : ""} ${p.live ? "live" : ""}` },
        h("span", null, p.done ? "✓" : p.live ? "●" : "○"),
        p.label,
      ),
    );
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
    const note = job.resumeNote ?? "";
    const rerunsExport = note.includes("runs it again") || note.includes("runs the export again");
    actions.appendChild(
      h(
        "button",
        {
          class: "primary",
          title: note,
          onClick: () => {
            if (rerunsExport) {
              confirmResume(job, note, reload);
              return;
            }
            void doResume(job, reload);
          },
        },
        rerunsExport ? "Resume (re-exports)" : "Resume",
      ),
    );
    if (note) {
      actions.appendChild(h("span", { class: "muted note" }, note));
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
    running ? log : null,
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
      h(
        "div",
        { class: "row" },
        h(
          "span",
          { class: "muted small" },
          `${job.phases.filter((p) => p.done).length} of ${job.phases.length} phases${job.elapsed ? ` · ${job.elapsed} elapsed` : ""}`,
        ),
        actions,
      ),
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

async function doResume(job: JobView, reload: () => void): Promise<void> {
  const res = await JobsAPI.resume(job.id);
  if (res.failure) {
    modal({ title: "This job was not resumed", body: h("div", null, res.failure.message) });
    return;
  }
  reload();
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
