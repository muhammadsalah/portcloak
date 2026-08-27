import { AuditAPI, type AuditEntry, type AuditView } from "../api";
import { clear, failureNotice, h, select, spinner, stamp } from "../dom";

/**
 * The audit log, and nothing else.
 *
 * It used to share a screen with the maintenance panels — the configuration
 * file, the orphan sweep, the purge — which put four buttons that change things
 * next to a record of what has already happened. The panels are on Settings
 * now. What is here is read-only by design: filtered, never edited, never
 * cleared from the app.
 */

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

export async function renderAudit(root: HTMLElement): Promise<void> {
  clear(root);
  root.appendChild(spinner("Reading the audit log…"));

  const state = { action: "", days: 0 };

  const draw = async () => {
    const audit = await AuditAPI.entries(state.action, state.days);
    clear(root);
    root.appendChild(
      h(
        "div",
        null,
        h("h1", { class: "page-title" }, "Audit log"),
        h(
          "div",
          { class: "page-subtitle" },
          "Everything PortCloak has done, in the order it did it. Append-only, and never cleared from here.",
        ),
      ),
    );
    if (audit.failure) {
      root.appendChild(failureNotice(audit.failure));
      return;
    }
    root.appendChild(auditPanel(audit, state, () => void draw()));
  };
  await draw();
}

function auditPanel(
  audit: AuditView,
  state: { action: string; days: number },
  reload: () => void,
): HTMLElement {
  const list = h("div", { class: "card-body" });

  for (const e of audit.entries) {
    list.appendChild(
      h(
        "div",
        { class: "audit-row" },
        // The full stamp leads the row: an audit entry is read to answer "when
        // exactly, and in which zone", and that answer should not be split
        // across two columns at opposite ends of the line.
        h("div", { class: "muted small audit-time" }, stamp(e.at)),
        h("span", { class: `dot ${toneFor(e.action, e.outcome)}` }),
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
      ),
    );
  }
  if (audit.entries.length === 0) {
    list.appendChild(
      h(
        "div",
        { class: "muted" },
        state.action || state.days
          ? "Nothing matches that filter."
          : "Nothing has been recorded yet.",
      ),
    );
  }

  return h(
    "div",
    { class: "card" },
    h(
      "div",
      { class: "card-head" },
      h(
        "span",
        { class: "card-title" },
        `${audit.entries.length} entr${audit.entries.length === 1 ? "y" : "ies"}`,
      ),
      h(
        "div",
        { class: "row" },
        select(actions, state.action, (v) => {
          state.action = v;
          reload();
        }),
        select(ranges, String(state.days), (v) => {
          state.days = Number(v);
          reload();
        }),
        h("span", { class: "muted small mono" }, audit.path),
      ),
    ),
    list,
    h("div", { class: "card-foot muted small" }, audit.note),
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

function toneFor(action: string, outcome: string): string {
  if (action === "encryptionDeclined") return "danger";
  if (outcome.includes("not finish") || outcome.includes("failed")) return "danger";
  if (action === "keyDeleted") return "danger";
  if (action === "secretReveal" || action === "exportView" || action === "keyRevealed") return "warn";
  if (action === "snapshotDelete" || action === "jobDiscarded") return "neutral";
  return "ok";
}
