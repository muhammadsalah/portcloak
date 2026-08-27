import {
  ConfigAPI,
  SnapshotAPI,
  type BrowseResult,
  type ConfigSnapshot,
  type Storage,
  type StorageKind,
  type StorageProbeResult,
  type StorageView,
} from "../api";
import {
  badge,
  bytes,
  clear,
  failureNotice,
  field,
  h,
  input,
  modal,
  notice,
  select,
  spinner,
  toggle,
  when,
} from "../dom";
import { navigate } from "../main";

const kinds: { value: StorageKind; label: string }[] = [
  { value: "disk", label: "Disk" },
  { value: "ssh", label: "SSH" },
  { value: "s3", label: "S3" },
  { value: "azure", label: "Azure Blob" },
];

interface State {
  snapshot?: ConfigSnapshot;
  selected?: string;
  draft?: Storage;
  originalName: string;
  secret: string;
  probe?: StorageProbeResult;
  probing: boolean;
  saving: boolean;
  browsing: boolean;
  browse?: BrowseResult;
  error?: string;
}

export async function renderStorage(
  root: HTMLElement,
  select_?: string,
  openBrowser = false,
): Promise<void> {
  clear(root);
  root.appendChild(spinner("Loading configuration…"));

  const snapshot = await ConfigAPI.load();
  const state: State = {
    snapshot,
    selected: select_ ?? snapshot.storage[0]?.name,
    originalName: "",
    secret: "",
    probing: false,
    saving: false,
    browsing: openBrowser,
  };
  const chosen = snapshot.storage.find((s) => s.name === state.selected);
  if (chosen) {
    state.draft = { ...chosen };
    state.originalName = chosen.name;
  } else {
    // A name that is no longer there — a deleted storage still in a route — is
    // nobody's selection rather than an editor full of undefined.
    state.selected = undefined;
  }

  const draw = () => {
    clear(root);
    root.appendChild(page(state, draw, root));
  };
  draw();

  if (openBrowser && state.selected) {
    state.browse = await SnapshotAPI.browse(state.selected);
    draw();
  }
}

function page(state: State, draw: () => void, root: HTMLElement): HTMLElement {
  const head = h(
    "div",
    { class: "page-head" },
    h(
      "div",
      null,
      h("h1", { class: "page-title" }, state.browsing ? "Storage browser" : "Storage"),
      h(
        "div",
        { class: "page-subtitle" },
        state.browsing
          ? "What this storage really holds, including objects PortCloak did not write."
          : "Where snapshots live. Every kind is rooted at a folder or prefix, so one backend can hold several independent trees.",
      ),
    ),
    state.browsing
      ? h("button", { onClick: () => navigate({ name: "storage", select: state.selected }) }, "Back to storage")
      : h(
          "button",
          {
            class: "primary",
            onClick: () => {
              state.draft = { name: "", kind: "disk" };
              state.originalName = "";
              state.selected = undefined;
              state.probe = undefined;
              state.secret = "";
              draw();
            },
          },
          "Add storage",
        ),
  );

  if (state.browsing) {
    return h("div", null, head, browser(state));
  }
  if (state.snapshot!.storage.length === 0 && !state.draft) {
    return h("div", null, head, nothingYet(state.snapshot!));
  }
  return h(
    "div",
    null,
    head,
    h("div", { class: "split" }, list(state, draw), state.draft ? editor(state, draw, root) : placeholder()),
  );
}

/**
 * The first launch. What a storage is, and the one property of it that decides
 * everything else: PortCloak roots every kind at a folder or prefix, so a
 * bucket can hold several independent trees and deleting a definition here
 * never touches what is already stored in it.
 */
function nothingYet(snapshot: ConfigSnapshot): HTMLElement {
  return h(
    "div",
    { class: "card" },
    h(
      "div",
      { class: "card-body" },
      h("div", { class: "card-title" }, "No storage yet"),
      h(
        "p",
        null,
        "A storage is where snapshots are written — a folder on disk, a folder on a host over SSH, an S3 bucket, or an Azure Blob container. Mark one as requiring encryption and nothing plaintext will ever be written to it.",
      ),
      h(
        "p",
        { class: "muted small" },
        `Add one with the button above, or write it into ${snapshot.configFile} by hand — the file is the same one this screen edits. Credentials never go in it: each secret goes to this machine's keychain and only a handle is written to the file.`,
      ),
    ),
  );
}

function placeholder(): HTMLElement {
  return h(
    "div",
    { class: "card" },
    h("div", { class: "card-body muted" }, "Select a storage on the left, or add one."),
  );
}

function list(state: State, draw: () => void): HTMLElement {
  const stores = state.snapshot!.storage;
  const items = h("div");

  for (const st of stores) {
    items.appendChild(
      h(
        "div",
        {
          style: `padding:12px 16px;cursor:pointer;border-left:3px solid ${state.selected === st.name ? "var(--primary)" : "transparent"};background:${state.selected === st.name ? "#f0f7fd" : "transparent"};border-bottom:1px solid #ededed`,
          onClick: () => {
            state.selected = st.name;
            state.draft = { ...st };
            state.originalName = st.name;
            state.probe = undefined;
            state.secret = "";
            state.error = undefined;
            draw();
          },
        },
        h(
          "div",
          { class: "row" },
          h("span", { style: "font-weight:500" }, st.name),
          st.default ? badge("default", "info") : null,
          st.encryptionRequired ? badge("encryption required", "ok") : null,
        ),
        h("div", { class: "muted small" }, `${kindLabel(st.kind)} · ${st.root}`),
        probeLine(st),
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
        { class: "muted small" },
        `${stores.length} storage definition${stores.length === 1 ? "" : "s"}`,
      ),
    ),
    items,
  );
}

function probeLine(st: StorageView): HTMLElement {
  if (!st.lastProbe) {
    return h("div", { class: "small", style: "color:var(--text-muted)" }, "Never tested");
  }
  const writable = st.lastProbe.writable !== false;
  const tone = st.stale ? "var(--danger)" : st.lastProbe.ok ? "var(--success)" : "var(--danger)";
  const detail = st.lastProbe.ok ? (writable ? "writable" : "read-only") : "not reachable";
  return h(
    "div",
    { class: "small", style: `color:${tone}` },
    `Tested ${st.probeAge} · ${detail}${st.stale ? " — stale" : ""}`,
  );
}

function kindLabel(kind: string): string {
  return kinds.find((k) => k.value === kind)?.label ?? kind;
}

function editor(state: State, draw: () => void, root: HTMLElement): HTMLElement {
  const st = state.draft!;

  const tabs = h("div", { class: "tabs" });
  for (const k of kinds) {
    tabs.appendChild(
      h(
        "div",
        {
          class: `tab ${st.kind === k.value ? "active" : ""}`,
          onClick: () => {
            st.kind = k.value;
            state.probe = undefined;
            draw();
          },
        },
        k.label,
      ),
    );
  }

  const form = h("div", null);
  form.appendChild(field("Name", input(st.name, (v) => (st.name = v))));

  switch (st.kind) {
    case "disk":
      form.appendChild(
        field(
          "Root folder",
          input(st.folder, (v) => (st.folder = v), { placeholder: "~/PortCloak/snapshots" }),
          "The tree under here is browsable: an operator who lost the app can still find and identify a snapshot with ls.",
        ),
      );
      break;

    case "ssh":
      form.appendChild(
        h(
          "div",
          { class: "field-row" },
          field("Host", input(st.host, (v) => (st.host = v))),
          field("Port", input(String(st.port ?? 22), (v) => (st.port = Number(v) || 22), { type: "number" })),
        ),
      );
      form.appendChild(
        h(
          "div",
          { class: "field-row" },
          field("User", input(st.user, (v) => (st.user = v))),
          field(
            "Authentication",
            select(
              [
                { value: "key", label: "Private key" },
                { value: "agent", label: "SSH agent" },
                { value: "password", label: "Password" },
              ],
              st.auth ?? "key",
              (v) => {
                st.auth = v as Storage["auth"];
                draw();
              },
            ),
          ),
        ),
      );
      form.appendChild(field("Remote folder", input(st.folder, (v) => (st.folder = v))));
      break;

    case "s3":
      form.appendChild(
        h(
          "div",
          { class: "field-row" },
          field(
            "Endpoint",
            input(st.endpoint, (v) => (st.endpoint = v), { placeholder: "s3.eu-west-1.amazonaws.com" }),
            "Point this at MinIO to use the same code path.",
          ),
          field("Region", input(st.region, (v) => (st.region = v), { placeholder: "eu-west-1" })),
        ),
      );
      form.appendChild(
        h(
          "div",
          { class: "field-row" },
          field("Bucket", input(st.bucket, (v) => (st.bucket = v))),
          field(
            "Prefix (folder)",
            input(st.prefix, (v) => (st.prefix = v), { placeholder: "portcloak/" }),
            "One bucket can hold several independent snapshot trees.",
          ),
        ),
      );
      form.appendChild(
        h(
          "div",
          { class: "field-row" },
          field(
            "Part size (MB)",
            input(String(st.partSizeMb ?? 8), (v) => (st.partSizeMb = Number(v) || 8), { type: "number" }),
            "Smaller parts resume faster on a flaky link.",
          ),
          h(
            "div",
            { class: "field" },
            h("label", null, "Path-style addressing"),
            h(
              "div",
              { class: "row" },
              toggle(Boolean(st.pathStyle), (on) => {
                st.pathStyle = on;
                draw();
              }),
              h("span", { class: "muted small" }, "Required by MinIO and most S3-compatible stores."),
            ),
          ),
        ),
      );
      break;

    case "azure":
      form.appendChild(
        h(
          "div",
          { class: "field-row" },
          field("Account", input(st.account, (v) => (st.account = v))),
          field(
            "Endpoint (optional)",
            input(st.endpoint, (v) => (st.endpoint = v), { placeholder: "http://127.0.0.1:10000/devstoreaccount1" }),
            "Point this at Azurite's dev endpoint to use the emulator.",
          ),
        ),
      );
      form.appendChild(
        h(
          "div",
          { class: "field-row" },
          field("Container", input(st.container, (v) => (st.container = v))),
          field("Prefix (folder)", input(st.prefix, (v) => (st.prefix = v))),
        ),
      );
      break;
  }

  if (st.kind !== "disk" && !(st.kind === "ssh" && st.auth === "agent")) {
    form.appendChild(
      field(
        credentialLabel(st.kind),
        h("input", {
          type: "password",
          placeholder: st.credentialRef ? "•••••••• (stored)" : credentialLabel(st.kind),
          onInput: (e: Event) => {
            state.secret = (e.target as HTMLInputElement).value;
          },
        }),
        st.credentialRef
          ? `Stored in this machine's keychain as ${st.credentialRef}. config.yaml holds only the handle.`
          : "Stored in this machine's keychain; config.yaml will hold only a handle.",
      ),
    );
  }

  form.appendChild(
    h(
      "div",
      { class: "field" },
      h(
        "div",
        { class: "row" },
        toggle(Boolean(st.default), (on) => {
          st.default = on;
          draw();
        }),
        h(
          "div",
          null,
          h("div", null, "Default for new captures"),
          h("div", { class: "field-hint" }, "Exactly one storage can be the default. Setting this clears the others."),
        ),
      ),
    ),
  );
  form.appendChild(
    h(
      "div",
      { class: "field" },
      h(
        "div",
        { class: "row" },
        toggle(Boolean(st.encryptionRequired), (on) => {
          st.encryptionRequired = on;
          draw();
        }),
        h(
          "div",
          null,
          h("div", null, "Encryption required"),
          h(
            "div",
            { class: "field-hint" },
            "Removes the opt-out for anything written here. A snapshot cannot be written to this storage unencrypted, and the engine enforces it — a hand-edited config cannot bypass it.",
          ),
        ),
      ),
    ),
  );

  const probeArea = h("div", { class: "card-body", style: "border-top:1px solid var(--border)" });
  probeArea.appendChild(
    h(
      "div",
      { class: "row" },
      h("span", { style: "font-weight:500" }, "Test storage"),
      h(
        "span",
        { class: "right" },
        h(
          "button",
          {
            disabled: !state.originalName || state.probing,
            onClick: async () => {
              state.probing = true;
              draw();
              state.probe = await ConfigAPI.testStorage(state.originalName);
              state.probing = false;
              draw();
            },
          },
          state.probing ? "Testing…" : "Test storage",
        ),
      ),
    ),
  );
  if (!state.originalName) {
    probeArea.appendChild(h("div", { class: "muted small" }, "Save the storage first, then test it."));
  }
  if (state.probing) probeArea.appendChild(spinner("Listing, writing a probe object, verifying, removing it…"));
  if (state.probe?.failure) probeArea.appendChild(failureNotice(state.probe.failure));
  if (state.probe && !state.probe.failure) probeArea.appendChild(reachPanel(state, draw));

  const foot = h(
    "div",
    { class: "card-foot" },
    h(
      "div",
      { class: "row" },
      state.originalName
        ? h(
            "a",
            {
              onClick: () => navigate({ name: "browse", storage: state.originalName }),
            },
            "Browse",
          )
        : null,
      state.originalName
        ? h(
            "a",
            {
              onClick: async () => {
                await ConfigAPI.duplicateStorage(state.originalName);
                void renderStorage(root);
              },
            },
            "Duplicate",
          )
        : null,
      state.originalName
        ? h("a", { style: "color:var(--danger)", onClick: () => confirmDelete(state, root) }, "Delete")
        : null,
    ),
    h(
      "div",
      { class: "row" },
      h("button", { onClick: () => navigate({ name: "storage" }) }, "Cancel"),
      h(
        "button",
        {
          class: "primary",
          disabled: state.saving || !st.name,
          onClick: async () => {
            state.saving = true;
            state.error = undefined;
            const failure = await ConfigAPI.saveStorage(state.originalName, st, state.secret);
            state.saving = false;
            if (failure) {
              state.error = failure.message;
              draw();
              return;
            }
            void renderStorage(root, st.name);
          },
        },
        state.saving ? "Saving…" : "Save",
      ),
    ),
  );

  const selected = state.snapshot!.storage.find((s) => s.name === state.originalName);

  return h(
    "div",
    { class: "card" },
    h(
      "div",
      { class: "card-head" },
      h(
        "div",
        { class: "row" },
        h("span", { class: "card-title" }, st.name || "New storage"),
        badge(kindLabel(st.kind), "info"),
      ),
    ),
    h(
      "div",
      { class: "card-body" },
      tabs,
      state.error ? notice("danger", "Not saved", state.error) : null,
      selected && !selected.readiness.ready
        ? notice("warn", "Not usable yet", selected.readiness.reason ?? "")
        : null,
      selected && !selected.credentialPresent
        ? notice(
            "warn",
            "This storage's credential is not in this machine's keychain",
            "Configuration is portable between machines; the secrets deliberately are not.",
          )
        : null,
      form,
    ),
    probeArea,
    foot,
  );
}

function credentialLabel(kind: StorageKind): string {
  switch (kind) {
    case "s3":
      return "Access key and secret (key:secret)";
    case "azure":
      return "Connection string, account key or SAS";
    default:
      return "Credential";
  }
}

/** The three-way result is described, never collapsed into pass or fail. */
function reachPanel(state: State, draw: () => void): HTMLElement {
  const r = state.probe!.reach;
  const tone = r.access === "writable" ? "ok" : r.access === "read-only" ? "warn" : "danger";

  const rows = h("dl", { class: "kv", style: "margin-top:10px" });
  const add = (k: string, v: string) => {
    rows.appendChild(h("dt", null, k));
    rows.appendChild(h("dd", null, v));
  };
  add("Root", r.root);
  add("Access", r.access);
  add("Integrity", r.integrity || "—");
  add("Resumable upload", r.resumable ? "yes" : "no");
  if (r.latency) add("Round trip", `${Math.round(r.latency / 1e6)} ms`);
  if (r.freeBytes) add("Free space", bytes(r.freeBytes));
  if (r.failedStep) add("Failed at", r.failedStep);

  const panel = h(
    "div",
    { class: `notice ${tone}` },
    h("div", { class: "notice-title" }, state.probe!.note),
    rows,
  );

  // A disk folder that does not exist yet is offered rather than rejected.
  if (r.access === "unreachable" && state.draft?.kind === "disk" && r.detail?.includes("does not exist")) {
    panel.appendChild(
      h(
        "button",
        {
          style: "margin-top:10px",
          onClick: async () => {
            await ConfigAPI.createStorageFolder(state.originalName);
            state.probe = await ConfigAPI.testStorage(state.originalName);
            draw();
          },
        },
        "Create the folder",
      ),
    );
  }
  return panel;
}

function confirmDelete(state: State, root: HTMLElement): void {
  const name = state.originalName;
  modal({
    title: `Delete the storage “${name}”?`,
    body: h(
      "div",
      null,
      h("p", null, "The definition and its keychain secret are removed from this machine."),
      h(
        "p",
        { class: "muted small" },
        "Stored snapshot files are not deleted. Removing a storage definition forgets how to reach the data; it does not destroy it.",
      ),
    ),
    confirmLabel: "Delete storage",
    confirmTone: "danger-solid",
    onConfirm: async () => {
      const failure = await ConfigAPI.deleteStorage(name);
      if (failure) {
        modal({ title: "Not deleted", body: h("div", null, failure.message) });
        return;
      }
      void renderStorage(root);
    },
  });
}

function browser(state: State): HTMLElement {
  if (!state.browse) return spinner("Reading storage…");
  const b = state.browse;

  const container = h("div", null);
  container.appendChild(
    notice(b.status.reachable ? "info" : "danger", `${b.storage} · ${b.status.kind}`, b.note),
  );

  const snapRows = h("tbody");
  for (const s of b.snapshots) {
    snapRows.appendChild(
      h(
        "tr",
        { class: "selectable" },
        h(
          "td",
          null,
          h(
            "a",
            {
              onClick: () =>
                navigate({
                  name: "inspect",
                  storage: b.storage,
                  bundleKey: s.bundleKey,
                  snapshotId: s.snapshotId,
                }),
            },
            s.realm,
          ),
        ),
        h("td", null, when(s.createdAt)),
        h("td", { class: "numeric" }, s.metadataReadable ? s.users.toLocaleString() : "—"),
        h("td", null, s.encrypted ? badge("Encrypted", "neutral") : badge("Unencrypted", "danger")),
        h("td", { class: "small muted mono" }, s.bundleKey),
        h("td", { class: "numeric small muted" }, bytes(s.bytes)),
      ),
    );
  }
  container.appendChild(
    h(
      "div",
      { class: "card" },
      h("div", { class: "card-head" }, h("span", { class: "card-title" }, "Snapshots")),
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
              h("th", { class: "numeric" }, "Users"),
              h("th", null, "Encryption"),
              h("th", null, "Object"),
              h("th", { class: "numeric" }, "Size"),
            ),
          ),
          snapRows,
        ),
      ),
    ),
  );

  if (b.foreign.length > 0) {
    const rows = h("tbody");
    for (const o of b.foreign) {
      rows.appendChild(
        h(
          "tr",
          null,
          h("td", { class: "mono small" }, o.key),
          h("td", { class: "numeric small muted" }, bytes(o.size)),
          h("td", { class: "small muted" }, when(o.modTime)),
          h("td", null, badge("Unrecognised", "neutral")),
        ),
      );
    }
    container.appendChild(
      h(
        "div",
        { class: "card" },
        h(
          "div",
          { class: "card-head" },
          h("span", { class: "card-title" }, "Objects PortCloak did not write"),
          h(
            "span",
            { class: "muted small" },
            "Shown rather than hidden — a mistyped prefix looks exactly like an empty one.",
          ),
        ),
        h(
          "div",
          { class: "table-scroll" },
          h(
            "table",
            null,
            h(
              "thead",
              null,
              h("tr", null, h("th", null, "Key"), h("th", { class: "numeric" }, "Size"), h("th", null, "Modified"), h("th", null, "")),
            ),
            rows,
          ),
        ),
      ),
    );
  }
  return container;
}
