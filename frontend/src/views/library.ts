import { SnapshotAPI, type FirstRun, type LibraryEntry, type LibraryView } from "../api";
import { badge, bytes, clear, count, h, modal, notice, spinner, when } from "../dom";
import { mark, onLight } from "../logo";
import { navigate, setHasSnapshots } from "../main";

/** The library is Tier 0: every snapshot, across every backend, with no key. */
export async function renderLibrary(root: HTMLElement): Promise<void> {
  clear(root);
  root.appendChild(spinner("Reading storage…"));

  let view: LibraryView;
  try {
    view = await SnapshotAPI.library();
  } catch (err) {
    clear(root);
    root.appendChild(notice("danger", "The snapshot library could not be read.", String(err)));
    return;
  }

  setHasSnapshots(view.entries.length > 0);
  clear(root);

  if (view.firstRun) {
    root.appendChild(renderFirstRun(view.firstRun));
    return;
  }

  root.appendChild(
    h(
      "div",
      { class: "page-head" },
      h(
        "div",
        null,
        h("h1", { class: "page-title" }, "Snapshots"),
        h("div", { class: "page-subtitle" }, view.summary),
      ),
      h(
        "button",
        { class: "primary", onClick: () => navigate({ name: "capture" }) },
        "Capture snapshot",
      ),
    ),
  );

  for (const s of view.storages) {
    if (!s.reachable) {
      root.appendChild(
        notice(
          "warn",
          `${s.name} could not be read, so this list may be short.`,
          s.error ?? "",
        ),
      );
    }
  }

  const state = { query: "", realm: "", storage: "" };
  const tbody = h("tbody");

  const draw = () => {
    clear(tbody);
    const rows = view.entries.filter((e) => {
      if (state.realm && e.realm !== state.realm) return false;
      if (state.storage && e.storage !== state.storage) return false;
      if (state.query) {
        const q = state.query.toLowerCase();
        if (!e.realm.toLowerCase().includes(q) && !e.snapshotId.toLowerCase().includes(q)) {
          return false;
        }
      }
      return true;
    });

    if (rows.length === 0) {
      tbody.appendChild(
        h(
          "tr",
          null,
          h("td", { colspan: "8", class: "muted" }, "No snapshots match those filters."),
        ),
      );
      return;
    }
    for (const e of rows) tbody.appendChild(row(e));
  };

  const search = h("input", {
    type: "text",
    placeholder: "Search realm or snapshot id",
    onInput: (ev: Event) => {
      state.query = (ev.target as HTMLInputElement).value;
      draw();
    },
  });

  const realmSelect = h(
    "select",
    {
      onChange: (ev: Event) => {
        state.realm = (ev.target as HTMLSelectElement).value;
        draw();
      },
    },
    h("option", { value: "" }, "Realm: all"),
    ...view.realms.map((r) => h("option", { value: r }, r)),
  );

  const storageSelect = h(
    "select",
    {
      onChange: (ev: Event) => {
        state.storage = (ev.target as HTMLSelectElement).value;
        draw();
      },
    },
    h("option", { value: "" }, "Storage: all"),
    ...view.storages.map((s) => h("option", { value: s.name }, s.name)),
  );

  root.appendChild(
    h(
      "div",
      { class: "toolbar" },
      h("div", { class: "search" }, search),
      realmSelect,
      storageSelect,
    ),
  );

  root.appendChild(
    h(
      "div",
      { class: "card" },
      h(
        "div",
        { class: "table-scroll" },
        h(
          "table",
          null,
          h(
            "thead",
            null,
            h(
              "tr",
              null,
              h("th", null, "Realm"),
              h("th", null, "Captured"),
              h("th", null, "Environment"),
              h("th", { class: "numeric" }, "Users"),
              h("th", null, "Completeness"),
              h("th", null, "Encryption"),
              h("th", null, "Storage"),
              h("th", null, ""),
            ),
          ),
          tbody,
        ),
      ),
    ),
  );
  draw();
}

function row(e: LibraryEntry): HTMLElement {
  const open = () =>
    navigate({
      name: "inspect",
      storage: e.storage,
      bundleKey: e.bundleKey,
      snapshotId: e.snapshotId,
    });

  let completeness: HTMLElement;
  if (!e.metadataReadable) {
    completeness = badge("Unreadable metadata", "warn");
  } else if (e.dependencyCount > 0) {
    completeness = badge(`${e.dependencyCount} external deps`, "warn");
  } else {
    completeness = badge(e.verdict || "Complete", e.verdict === "Complete" ? "ok" : "warn");
  }

  const encryption = e.encrypted
    ? h("span", { class: "muted small" }, `🔒 Encrypted${e.encryptionMode ? ` · ${e.encryptionMode}` : ""}`)
    : badge("Unencrypted", "danger");

  return h(
    "tr",
    { class: "selectable" },
    h("td", null, h("a", { onClick: open }, e.realm || "(unknown)")),
    h("td", null, when(e.createdAt)),
    h(
      "td",
      { class: "small" },
      e.environment ? `${e.environment}${e.executionMode ? ` · ${describeMode(e.executionMode)}` : ""}` : "—",
    ),
    h("td", { class: "numeric" }, e.metadataReadable ? count(e.users) : "—"),
    h("td", null, completeness),
    h("td", null, encryption),
    h("td", { class: "small muted" }, `${e.storage} · ${bytes(e.bytes)}`),
    h(
      "td",
      { class: "right" },
      h(
        "div",
        { class: "row" },
        h("a", { onClick: open }, "Inspect"),
        h(
          "a",
          {
            class: "muted",
            onClick: () => confirmDelete(e),
          },
          "Delete",
        ),
      ),
    ),
  );
}

function describeMode(mode: string): string {
  return mode === "ephemeral-clone" ? "clone" : "in place";
}

function confirmDelete(e: LibraryEntry): void {
  modal({
    title: `Delete this snapshot of ${e.realm}?`,
    body: h(
      "div",
      null,
      h(
        "p",
        null,
        `Captured ${when(e.createdAt)} from ${e.environment ?? "an unknown environment"}, ${count(e.users)} users.`,
      ),
      h(
        "p",
        { class: "muted small" },
        "The bundle and both sidecars are removed from " +
          e.storage +
          ". PortCloak does not keep another copy, and this cannot be undone.",
      ),
    ),
    confirmLabel: "Delete snapshot",
    confirmTone: "danger-solid",
    onConfirm: async () => {
      const res = await SnapshotAPI.remove(e.storage, e.bundleKey);
      if (res.failure) {
        modal({ title: "The snapshot was not deleted", body: h("div", null, res.failure.message) });
        return;
      }
      navigate({ name: "library" });
    },
  });
}

function renderFirstRun(fr: FirstRun): HTMLElement {
  const card = (
    n: number,
    done: boolean,
    title: string,
    body: string,
    action: string,
    onClick: () => void,
  ) =>
    h(
      "div",
      { class: "card" },
      h(
        "div",
        { class: "card-body" },
        h(
          "div",
          { class: "row", style: "margin-bottom:8px" },
          h("span", { class: `step-number ${done ? "" : "pending"}` }, String(n)),
          h("span", { style: "font-weight:500" }, title),
        ),
        h("p", { class: "muted small", style: "margin-top:0" }, body),
        h("button", { class: done ? "primary" : "", onClick }, action),
      ),
    );

  return h(
    "div",
    { class: "empty" },
    h("div", { class: "empty-mark" }, mark(44, onLight)),
    h("h2", null, fr.heading),
    h("p", { class: "muted" }, fr.body),
    h(
      "div",
      { class: "empty-cards" },
      card(
        1,
        fr.needsEnvironment,
        "Add an environment",
        fr.environmentBody,
        "Add an environment",
        () => navigate({ name: "environments" }),
      ),
      card(2, !fr.needsEnvironment && fr.needsStorage, "Add a storage", fr.storageBody, "Add a storage", () =>
        navigate({ name: "storage" }),
      ),
    ),
    h(
      "div",
      { class: "card", style: "margin-top:16px;text-align:left" },
      h(
        "div",
        { class: "card-body" },
        h(
          "div",
          { class: "row" },
          h("div", { style: "font-weight:500" }, fr.noAccountHeading),
          h("div", { class: "right muted small mono" }, fr.configFile),
        ),
        h("p", { class: "muted small" }, fr.noAccountBody),
      ),
    ),
  );
}
