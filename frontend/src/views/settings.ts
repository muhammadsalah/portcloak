import {
  SettingsAPI,
  type AboutView,
  type LocationView,
  type OrphanReport,
  type WorkingData,
} from "../api";
import {
  badge,
  bytes,
  clear,
  failureNotice,
  h,
  input,
  modal,
  notice,
  setModalConfirmDisabled,
  spinner,
} from "../dom";

/**
 * Settings is what PortCloak does to itself: where it keeps its files, what a
 * crashed session left running in someone else's cluster, and what is sitting
 * on this disk.
 *
 * These three panels used to sit beside the audit log, which meant the record
 * of what happened shared a screen with four buttons that make things happen.
 * They are here now and the audit screen is a record again.
 */
export async function renderSettings(root: HTMLElement): Promise<void> {
  clear(root);
  root.appendChild(spinner("Reading your settings…"));

  const draw = async () => {
    const [location, orphans, working, about] = await Promise.all([
      SettingsAPI.location(),
      SettingsAPI.orphans(),
      SettingsAPI.workingData(),
      SettingsAPI.about(),
    ]);
    clear(root);
    root.appendChild(
      h(
        "div",
        null,
        h("h1", { class: "page-title" }, "Settings"),
        h(
          "div",
          { class: "page-subtitle" },
          "Where PortCloak keeps its files, and everything it is holding — here and elsewhere.",
        ),
      ),
    );
    root.appendChild(locationPanel(location, () => void draw()));
    root.appendChild(
      h(
        "div",
        { class: "split-wide" },
        orphanPanel(orphans, () => void draw()),
        workingPanel(working, () => void draw()),
      ),
    );
    root.appendChild(aboutPanel(about));
  };
  await draw();
}

/* ── Where PortCloak keeps its files ───────────────────────────────────── */

function locationPanel(loc: LocationView, reload: () => void): HTMLElement {
  const missing = loc.credentials.filter((c) => !c.present);

  const body = h(
    "div",
    { class: "card-body" },
    h("div", { class: "path-box" }, loc.root),
    h(
      "dl",
      { class: "kv", style: "margin-top:12px" },
      h("dt", null, "Configuration"),
      h("dd", { class: "mono small" }, loc.configFile),
      h("dt", null, "Chosen how"),
      h("dd", null, loc.sourceNote),
      loc.source === "chosen" ? h("dt", null, "Recorded in") : null,
      loc.source === "chosen" ? h("dd", { class: "mono small" }, loc.pointer) : null,
    ),
    h("p", { class: "muted small" }, loc.note),
  );

  if (loc.failure) body.appendChild(failureNotice(loc.failure));

  // A reason the move would be refused is worth showing before the button is
  // pressed, not after — an open snapshot is something the operator can go and
  // close.
  if (loc.blocked) {
    body.appendChild(notice("info", "Not movable right now", loc.blocked));
  }

  body.appendChild(
    h(
      "div",
      { class: "row", style: "margin-top:12px" },
      h(
        "button",
        {
          class: "primary",
          disabled: !loc.movable || Boolean(loc.blocked),
          onClick: () => confirmMove(loc, reload),
        },
        "Move to another folder…",
      ),
      !loc.atDefault
        ? h(
            "button",
            {
              disabled: !loc.movable || Boolean(loc.blocked),
              onClick: () => confirmDefault(loc, reload),
            },
            "Use the default folder",
          )
        : null,
      h("button", { class: "plain", onClick: reload }, "Refresh"),
    ),
  );

  body.appendChild(
    missing.length > 0
      ? notice(
          "warn",
          `${missing.length} credential${missing.length === 1 ? "" : "s"} not on this machine`,
          "Configuration is portable between machines; the secrets deliberately are not. " +
            missing.map((m) => m.name).join(", "),
        )
      : h(
          "div",
          { class: "small", style: "color:var(--success);margin-top:12px" },
          `✓ Every credential referenced by this config is in this machine's keychain (${loc.credentials.length}).`,
        ),
  );

  return h(
    "div",
    { class: "card" },
    h(
      "div",
      { class: "card-head" },
      h("span", { class: "card-title" }, "Where PortCloak keeps its files"),
      sourceBadge(loc),
    ),
    body,
  );
}

function sourceBadge(loc: LocationView): HTMLElement {
  switch (loc.source) {
    case "environment":
      return badge("PORTCLOAK_HOME", "warn");
    case "chosen":
      return badge("Chosen folder", "info");
    default:
      return badge("Default folder", "neutral");
  }
}

/** What moves and what stays, stated before the folder is picked. */
function movedAndKept(): HTMLElement {
  const moves = h("ul", { class: "small", style: "margin:4px 0 0;padding-left:18px" });
  for (const item of [
    "config.yaml — your environments, storage definitions and preferences",
    "the audit log",
    "job checkpoints, including any interrupted job waiting to be resumed",
    "logs, inspection indexes and decrypted working files",
  ]) {
    moves.appendChild(h("li", null, item));
  }

  const keeps = h("ul", { class: "small", style: "margin:4px 0 0;padding-left:18px" });
  for (const item of [
    "this machine's keychain — every credential stays exactly where it is",
    "every snapshot in storage; moving this folder moves no backup",
  ]) {
    keeps.appendChild(h("li", null, item));
  }

  return h(
    "div",
    null,
    h("div", { style: "font-weight:600;margin-bottom:2px" }, "What moves:"),
    moves,
    h("div", { style: "font-weight:600;margin:10px 0 2px" }, "What does not:"),
    keeps,
  );
}

function confirmMove(loc: LocationView, reload: () => void): void {
  let folder = "";

  const field = h(
    "div",
    { class: "field" },
    h("label", null, "New folder"),
    input("", (v) => {
      folder = v.trim();
      setModalConfirmDisabled(folder === "");
    }, { placeholder: "/Volumes/work/portcloak" }),
    h(
      "div",
      { class: "field-hint" },
      "The full path. It has to be empty or not exist yet, so nothing already there can be overwritten.",
    ),
  );

  modal({
    title: "Move the PortCloak folder?",
    body: h(
      "div",
      null,
      field,
      movedAndKept(),
      h(
        "p",
        { class: "muted small", style: "margin-top:12px" },
        `Moving from ${loc.root}. The running application follows the folder — nothing has to be restarted.`,
      ),
    ),
    confirmLabel: "Move",
    confirmDisabled: true,
    onConfirm: async () => {
      const res = await SettingsAPI.move(folder);
      reportMove(res, folder);
      reload();
    },
  });
}

function confirmDefault(loc: LocationView, reload: () => void): void {
  modal({
    title: "Go back to the default folder?",
    body: h(
      "div",
      null,
      h("p", null, `Everything moves back to ${loc.default}, and PortCloak stops recording a chosen folder.`),
      movedAndKept(),
    ),
    confirmLabel: "Move it back",
    onConfirm: async () => {
      const res = await SettingsAPI.useDefault();
      reportMove(res, loc.default);
      reload();
    },
  });
}

/**
 * A move that failed has to say so out loud. The panel behind the modal
 * re-reads the location either way, so a silent failure would look exactly like
 * a success that did nothing.
 */
function reportMove(res: LocationView, wanted: string): void {
  if (res.failure) {
    modal({
      title: "Not moved",
      body: h(
        "div",
        null,
        failureNotice(res.failure),
        h("p", { class: "muted small" }, `PortCloak is still using ${res.root}.`),
      ),
      cancelLabel: "Close",
    });
    return;
  }
  modal({
    title: "Moved",
    body: h(
      "div",
      null,
      h("p", null, `PortCloak is now reading and writing under ${res.root}.`),
      res.root !== wanted
        ? h("p", { class: "muted small" }, `You asked for ${wanted}.`)
        : null,
      h(
        "p",
        { class: "muted small" },
        "Nothing was left behind at the old location, and the next launch will find it here.",
      ),
    ),
    cancelLabel: "Close",
  });
}

/* ── Orphaned clones ───────────────────────────────────────────────────── */

function orphanPanel(report: OrphanReport, reload: () => void): HTMLElement {
  const body = h("div", { class: "card-body" });

  for (const o of report.orphans) {
    body.appendChild(
      h(
        "div",
        { style: "margin-bottom:12px" },
        h("div", { class: "path-box" }, o.ref),
        h("div", { class: "muted small" }, `${o.environment} · created ${o.age} ago · ${o.state ?? ""}`),
        h(
          "div",
          { class: "row", style: "margin-top:8px" },
          h(
            "button",
            {
              class: "primary",
              onClick: async () => {
                const failure = await SettingsAPI.removeOrphan(o.environment, o.ref);
                if (failure) {
                  modal({ title: "Not removed", body: failureNotice(failure), cancelLabel: "Close" });
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
    body.appendChild(notice("warn", `${u.environment} could not be checked`, u.reason));
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

/* ── Local working data ────────────────────────────────────────────────── */

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
      const res = await SettingsAPI.purge();
      if (res.failure) {
        modal({ title: "Not purged", body: failureNotice(res.failure), cancelLabel: "Close" });
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

/* ── Which build this is ───────────────────────────────────────────────── */

/**
 * The one screen that answers "which PortCloak wrote this bundle?".
 *
 * A snapshot manifest records the version that produced it, so when a restore
 * refuses the first thing anyone needs is the identity of the binary in front
 * of them. Copying it has its own button because the alternative is a reporter
 * transcribing a commit hash by eye, and half of those arrive wrong.
 */
function aboutPanel(a: AboutView): HTMLElement {
  const dl = h("dl", { class: "kv" });
  const rows: [string, string, boolean][] = [
    ["Version", a.version, false],
    ["Commit", a.commit, true],
    ["Built", a.date, false],
    ["Platform", a.platform, true],
    ["Go", a.go, true],
    ["Licence", a.licence, false],
    ["Copyright", a.copyright, false],
    ["Log file", a.logFile, true],
  ];
  for (const [k, v, mono] of rows) {
    dl.appendChild(h("dt", null, k));
    dl.appendChild(h("dd", { class: mono ? "mono small" : "" }, v));
  }

  const copy = h(
    "button",
    {
      class: "plain",
      onClick: (e: Event) => {
        void navigator.clipboard.writeText(a.support).then(() => {
          const btn = e.currentTarget as HTMLButtonElement;
          const was = btn.textContent;
          btn.textContent = "Copied";
          window.setTimeout(() => (btn.textContent = was), 1500);
        });
      },
    },
    "Copy build details",
  );

  return h(
    "div",
    { class: "card" },
    h("div", { class: "card-head" }, h("span", { class: "card-title" }, "About PortCloak")),
    h(
      "div",
      { class: "card-body" },
      dl,
      h(
        "p",
        { class: "muted small" },
        "A commit marked dirty was built from a tree with uncommitted changes, so it is not " +
          "exactly the commit it names.",
      ),
      h("div", { class: "row", style: "margin-top:12px" }, copy),
    ),
  );
}
