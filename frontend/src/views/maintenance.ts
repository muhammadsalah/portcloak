import { MaintenanceAPI, type AuditView, type ConfigFileView, type OrphanReport, type WorkingData } from "../api";
import { badge, bytes, clear, h, modal, notice, select, spinner, time, when } from "../dom";

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
  { value: "keyCreated", label: "Keys created" },
  { value: "keyDeleted", label: "Keys deleted" },
];

export async function renderMaintenance(root: HTMLElement): Promise<void> {
  clear(root);
  root.appendChild(spinner("Reading the audit log…"));

  const state = { action: "", days: 0 };

  const draw = async () => {
    const [audit, configFile, orphans, working] = await Promise.all([
      MaintenanceAPI.audit(state.action, state.days),
      MaintenanceAPI.configFile(),
      MaintenanceAPI.orphans(),
      MaintenanceAPI.workingData(),
    ]);
    clear(root);
    root.appendChild(
      h(
        "div",
        null,
        h("h1", { class: "page-title" }, "Audit log & maintenance"),
        h("div", { class: "page-subtitle" }, "What PortCloak did, and everything it is holding on this machine."),
      ),
    );
    root.appendChild(
      h(
        "div",
        { class: "split-wide" },
        auditPanel(audit, state, () => void draw()),
        h(
          "div",
          null,
          configPanel(configFile),
          orphanPanel(orphans, () => void draw()),
          workingPanel(working, () => void draw()),
        ),
      ),
    );
  };
  await draw();
}

function auditPanel(audit: AuditView, state: { action: string; days: number }, reload: () => void): HTMLElement {
  const list = h("div", { class: "card-body" });

  for (const e of audit.entries) {
    const tone = toneFor(e.action, e.outcome);
    list.appendChild(
      h(
        "div",
        { class: "row", style: "align-items:flex-start;gap:12px;padding:8px 0;border-bottom:1px solid #ededed" },
        h("div", { class: "muted small", style: "width:64px;flex:none" }, `${time(e.at)}`),
        h("span", { class: `dot ${tone}`, style: "margin-top:6px" }),
        h(
          "div",
          { class: "grow" },
          h("div", null, describe(e)),
          h(
            "div",
            { class: "muted small" },
            [e.snapshotId, e.environment, e.storage, e.detail].filter(Boolean).join(" · "),
          ),
          e.reason ? h("div", { class: "muted small" }, `Reason: ${e.reason}`) : null,
        ),
        h("div", { class: "muted small", style: "flex:none" }, when(e.at).slice(0, 10)),
      ),
    );
  }
  if (audit.entries.length === 0) {
    list.appendChild(h("div", { class: "muted" }, "Nothing has been recorded yet."));
  }

  return h(
    "div",
    { class: "card" },
    h(
      "div",
      { class: "card-head" },
      h("span", { class: "card-title" }, "Audit log"),
      h(
        "div",
        { class: "row" },
        select(actions, state.action, (v) => {
          state.action = v;
          reload();
        }),
        h("span", { class: "muted small mono" }, audit.path),
      ),
    ),
    list,
    h("div", { class: "card-foot muted small" }, audit.note),
  );
}

function describe(e: { action: string; outcome: string; realm?: string; environment?: string }): string {
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

function toneFor(action: string, outcome: string): string {
  if (action === "encryptionDeclined") return "danger";
  if (outcome.includes("not finish") || outcome.includes("failed")) return "danger";
  if (action === "keyDeleted") return "danger";
  if (action === "secretReveal" || action === "exportView" || action === "keyRevealed") return "warn";
  if (action === "snapshotDelete" || action === "jobDiscarded") return "neutral";
  return "ok";
}

function configPanel(cfg: ConfigFileView): HTMLElement {
  const missing = cfg.credentials.filter((c) => !c.present);

  return h(
    "div",
    { class: "card" },
    h("div", { class: "card-head" }, h("span", { class: "card-title" }, "Configuration file")),
    h(
      "div",
      { class: "card-body" },
      h(
        "div",
        {
          class: "mono small",
          style: "background:var(--surface-subtle);border:1px solid var(--border);border-radius:3px;padding:8px 10px;margin-bottom:10px",
        },
        cfg.path,
      ),
      h("p", { class: "muted small", style: "margin-top:0" }, cfg.note),
      missing.length > 0
        ? notice(
            "warn",
            `${missing.length} credential${missing.length === 1 ? "" : "s"} not on this machine`,
            "Configuration is portable between machines; the secrets deliberately are not. " +
              missing.map((m) => m.name).join(", "),
          )
        : h(
            "div",
            { class: "small", style: "color:var(--success)" },
            `✓ Every credential referenced by this config is in this machine's keychain (${cfg.credentials.length}).`,
          ),
    ),
  );
}

function orphanPanel(report: OrphanReport, reload: () => void): HTMLElement {
  const body = h("div", { class: "card-body" });

  for (const o of report.orphans) {
    body.appendChild(
      h(
        "div",
        { style: "margin-bottom:12px" },
        h(
          "div",
          {
            class: "mono small",
            style: "background:var(--surface-subtle);border:1px solid var(--border);border-radius:3px;padding:8px 10px",
          },
          o.ref,
        ),
        h("div", { class: "muted small" }, `${o.environment} · created ${o.age} ago · ${o.state ?? ""}`),
        h(
          "div",
          { class: "row", style: "margin-top:8px" },
          h(
            "button",
            {
              class: "primary",
              onClick: async () => {
                const failure = await MaintenanceAPI.removeOrphan(o.environment, o.ref);
                if (failure) {
                  modal({ title: "Not removed", body: h("div", null, failure.message) });
                  return;
                }
                reload();
              },
            },
            "Remove it",
          ),
          h("button", { onClick: reload }, "Leave it"),
        ),
      ),
    );
  }

  for (const u of report.unchecked) {
    body.appendChild(
      notice("warn", `${u.environment} could not be checked`, u.reason),
    );
  }
  body.appendChild(h("p", { class: "muted small", style: "margin-bottom:0" }, report.note));
  body.appendChild(
    h(
      "p",
      { class: "muted small" },
      "Found by PortCloak's own label on launch. Offered, never removed without asking — your cluster is not ours to garbage-collect.",
    ),
  );

  const heading =
    report.orphans.length > 0
      ? `⚠ ${report.orphans.length} orphaned clone${report.orphans.length === 1 ? "" : "s"} found`
      : "Orphaned clones";

  return h(
    "div",
    { class: "card", style: report.orphans.length ? "border-color:var(--warning)" : "" },
    h(
      "div",
      { class: "card-head" },
      h("span", { class: "card-title" }, heading),
      h("button", { class: "plain", onClick: reload }, "Check again"),
    ),
    body,
  );
}

function workingPanel(w: WorkingData, reload: () => void): HTMLElement {
  const rows: [string, Node | string][] = [
    ["Inspection indexes", w.indexNote],
    ["Finished job records", `${w.finishedJobs} · ${bytes(w.finishedBytes)}`],
    [
      "Interrupted jobs (resumable)",
      w.interruptedJobs > 0 ? badge(`${w.interruptedJobs} · kept`, "warn") : "none",
    ],
    ["Decrypted working files", bytes(w.workBytes)],
    ["Rotated logs", bytes(w.logBytes)],
  ];

  const dl = h("dl", { class: "kv" });
  for (const [k, v] of rows) {
    dl.appendChild(h("dt", null, k));
    dl.appendChild(h("dd", null, v));
  }

  const keeps = h("ul", { class: "small", style: "margin:6px 0 0;padding-left:18px" });
  for (const k of w.keeps) keeps.appendChild(h("li", null, k));

  return h(
    "div",
    { class: "card" },
    h("div", { class: "card-head" }, h("span", { class: "card-title" }, "Local working data")),
    h(
      "div",
      { class: "card-body" },
      dl,
      h(
        "div",
        { class: "notice info small", style: "margin-top:12px" },
        h("div", null, w.note),
        h("div", { style: "margin-top:6px;font-weight:600" }, "It never touches:"),
        keeps,
      ),
      h(
        "button",
        {
          class: "danger",
          style: "margin-top:12px",
          onClick: () => confirmPurge(w, reload),
        },
        "Purge local data",
      ),
    ),
  );
}

function confirmPurge(w: WorkingData, reload: () => void): void {
  const keeps = h("ul", { class: "small", style: "margin:6px 0 0;padding-left:18px" });
  for (const k of w.keeps) keeps.appendChild(h("li", null, k));

  modal({
    title: "Purge local working data?",
    body: h(
      "div",
      null,
      h("p", null, w.note),
      h("p", { style: "font-weight:600;margin-bottom:2px" }, "It will not touch:"),
      keeps,
      h(
        "p",
        { class: "muted small", style: "margin-top:12px" },
        "Discarding an interrupted job's checkpoint is a separate action on the Activity screen. This is housekeeping, not job control.",
      ),
    ),
    confirmLabel: "Purge",
    confirmTone: "danger-solid",
    onConfirm: async () => {
      const res = await MaintenanceAPI.purge();
      if (res.failure) {
        modal({ title: "Not purged", body: h("div", null, res.failure.message) });
        return;
      }
      modal({
        title: "Purged",
        body: h("div", null, h("p", null, res.note), h("p", { class: "muted small" }, `${bytes(res.bytes)} freed.`)),
        cancelLabel: "Close",
      });
      reload();
    },
  });
}
