import "./styles.css";

import { ConfigAPI, JobsAPI, onProgress, type ProgressEvent } from "./api";
import { clear, h } from "./dom";
import { renderLibrary } from "./views/library";
import { renderCapture } from "./views/capture";
import { renderRestore } from "./views/restore";
import { renderActivity } from "./views/activity";
import { renderEnvironments } from "./views/environments";
import { renderStorage } from "./views/storage";
import { renderMaintenance } from "./views/maintenance";
import { renderInspector } from "./views/inspector";

export type Route =
  | { name: "capture" }
  | { name: "library" }
  | { name: "restore"; snapshotId?: string }
  | { name: "activity" }
  | { name: "environments"; select?: string }
  | { name: "storage"; select?: string }
  | { name: "browse"; storage: string }
  | { name: "maintenance" }
  | { name: "inspect"; storage: string; bundleKey: string; snapshotId: string; tab?: string };

/** App-wide state the shell owns: the current route and the live counters. */
interface Shell {
  route: Route;
  environments: number;
  storages: number;
  activeJobs: number;
  hasSnapshots: boolean;
}

const shell: Shell = {
  route: { name: "library" },
  environments: 0,
  storages: 0,
  activeJobs: 0,
  hasSnapshots: true,
};

/**
 * Progress events all arrive on one stream. Views subscribe through here rather
 * than each opening its own listener, so navigating away cannot leave one
 * behind.
 */
const progressListeners = new Set<(e: ProgressEvent) => void>();

export function subscribeProgress(fn: (e: ProgressEvent) => void): () => void {
  progressListeners.add(fn);
  return () => progressListeners.delete(fn);
}

export function navigate(route: Route): void {
  shell.route = route;
  render();
}

export function currentRoute(): Route {
  return shell.route;
}

const nav: { section: string; items: { label: string; route: Route; key: string }[] }[] = [
  {
    section: "Workspace",
    items: [
      { key: "capture", label: "Capture", route: { name: "capture" } },
      { key: "library", label: "Snapshots", route: { name: "library" } },
      { key: "restore", label: "Restore", route: { name: "restore" } },
      { key: "activity", label: "Activity", route: { name: "activity" } },
    ],
  },
  {
    section: "Configuration",
    items: [
      { key: "environments", label: "Environments", route: { name: "environments" } },
      { key: "storage", label: "Storage", route: { name: "storage" } },
      { key: "maintenance", label: "Audit log", route: { name: "maintenance" } },
    ],
  },
];

function routeKey(route: Route): string {
  switch (route.name) {
    case "inspect":
    case "browse":
      return route.name === "browse" ? "storage" : "library";
    default:
      return route.name;
  }
}

function renderNav(): HTMLElement {
  const el = h("nav", { class: "nav" });
  const active = routeKey(shell.route);

  for (const group of nav) {
    el.appendChild(h("div", { class: "nav-section" }, group.section));
    for (const item of group.items) {
      // Until there is an environment and a storage, a capture cannot start.
      // The item is dimmed rather than hidden, so the shape of the tool is
      // visible from the first launch.
      const blocked =
        item.key === "capture" && (shell.environments === 0 || shell.storages === 0);
      const restoreBlocked = item.key === "restore" && !shell.hasSnapshots;
      const disabled = blocked || restoreBlocked;

      let trailing: HTMLElement | null = null;
      if (item.key === "activity" && shell.activeJobs > 0) {
        trailing = h("span", { class: "nav-badge" }, String(shell.activeJobs));
      } else if (item.key === "environments") {
        trailing = h("span", { class: "nav-count" }, String(shell.environments));
      } else if (item.key === "storage") {
        trailing = h("span", { class: "nav-count" }, String(shell.storages));
      }

      el.appendChild(
        h(
          "div",
          {
            class: `nav-item ${active === item.key ? "active" : ""} ${disabled ? "disabled" : ""}`,
            onClick: () => {
              if (!disabled) navigate(item.route);
            },
          },
          h("span", null, item.label),
          trailing,
        ),
      );
    }
  }
  return el;
}

function render(): void {
  const root = document.getElementById("app");
  if (!root) return;
  clear(root);

  const content = h("main", { class: "content" });
  root.appendChild(
    h(
      "div",
      { class: "masthead" },
      h("div", { class: "mark" }),
      h("div", { class: "wordmark" }, "PortCloak"),
    ),
  );
  root.appendChild(h("div", { class: "body" }, renderNav(), content));

  const route = shell.route;
  switch (route.name) {
    case "library":
      void renderLibrary(content);
      break;
    case "capture":
      void renderCapture(content);
      break;
    case "restore":
      void renderRestore(content, route.snapshotId);
      break;
    case "activity":
      void renderActivity(content);
      break;
    case "environments":
      void renderEnvironments(content, route.select);
      break;
    case "storage":
      void renderStorage(content, route.select);
      break;
    case "browse":
      void renderStorage(content, route.storage, true);
      break;
    case "maintenance":
      void renderMaintenance(content);
      break;
    case "inspect":
      void renderInspector(content, route);
      break;
  }
}

/** Keeps the navigation counters honest without each view having to push them. */
async function refreshCounters(): Promise<void> {
  try {
    const cfg = await ConfigAPI.load();
    shell.environments = cfg.environments.length;
    shell.storages = cfg.storage.length;
  } catch {
    // A configuration that will not load is reported by the library view,
    // which has room to say which line is wrong.
  }
  try {
    const activity = await JobsAPI.list();
    shell.activeJobs = activity.running + activity.interrupted;
  } catch {
    shell.activeJobs = 0;
  }
  const navEl = document.querySelector(".nav");
  if (navEl) navEl.replaceWith(renderNav());
}

export function setHasSnapshots(has: boolean): void {
  shell.hasSnapshots = has;
}

function start(): void {
  onProgress((e) => {
    for (const fn of progressListeners) fn(e);
    // A job reaching a terminal state changes the Activity badge.
    if (e.kind === "jobState") void refreshCounters();
  });

  render();
  void refreshCounters();
  // The counters are cheap and the alternative is a stale badge, which is
  // worse than a small poll.
  window.setInterval(() => void refreshCounters(), 5000);
}

start();
