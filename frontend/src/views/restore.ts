import {
  InspectAPI,
  RestoreAPI,
  SnapshotAPI,
  type EnvironmentView,
  type LibraryEntry,
  type Plan,
  type Strategy,
} from "../api";
import { badge, clear, count, failureNotice, h, modal, notice, select, spinner, when } from "../dom";
import { navigate } from "../main";

type Step = "snapshot" | "destination" | "preconditions" | "strategy" | "apply";

const steps: { key: Step; label: string }[] = [
  { key: "snapshot", label: "Snapshot" },
  { key: "destination", label: "Destination" },
  { key: "preconditions", label: "Preconditions" },
  { key: "strategy", label: "Strategy & dry run" },
  { key: "apply", label: "Apply" },
];

interface State {
  step: Step;
  entries: LibraryEntry[];
  destinations: EnvironmentView[];
  strategies: Strategy[];
  snapshot?: LibraryEntry;
  opened: boolean;
  environment: string;
  strategy: string;
  plan?: Plan;
  planning: boolean;
  confirmRealm: string;
  applying: boolean;
  error?: string;
  outOfScope: string[];
}

export async function renderRestore(root: HTMLElement, snapshotId?: string): Promise<void> {
  clear(root);
  root.appendChild(spinner("Loading snapshots and destinations…"));

  const [library, destinations, strategies, outOfScope] = await Promise.all([
    SnapshotAPI.library(),
    RestoreAPI.destinations(),
    RestoreAPI.strategies(),
    RestoreAPI.outOfScopeNote(),
  ]);

  const state: State = {
    step: "snapshot",
    entries: library.entries,
    destinations,
    strategies,
    opened: false,
    environment: destinations[0]?.name ?? "",
    strategy: "overwrite",
    planning: false,
    confirmRealm: "",
    applying: false,
    outOfScope,
  };
  if (snapshotId) {
    state.snapshot = library.entries.find((e) => e.snapshotId === snapshotId);
    if (state.snapshot) state.step = "destination";
  }

  const draw = () => {
    clear(root);
    root.appendChild(page(state, draw));
  };
  draw();
}

function page(state: State, draw: () => void): HTMLElement {
  const idx = steps.findIndex((s) => s.key === state.step);

  const rail = h("div", { class: "row", style: "gap:20px;margin-bottom:18px;flex-wrap:wrap" });
  steps.forEach((step, i) => {
    const done = i < idx;
    const active = i === idx;
    rail.appendChild(
      h(
        "div",
        { class: "row", style: `gap:8px;color:${active ? "var(--text)" : "var(--text-secondary)"}` },
        h("span", { class: `step-mark ${done ? "done" : active ? "active" : ""}` }, done ? "✓" : String(i + 1)),
        step.label,
      ),
    );
  });

  const panel = h("div");
  switch (state.step) {
    case "snapshot":
      panel.appendChild(snapshotStep(state, draw));
      break;
    case "destination":
      panel.appendChild(destinationStep(state, draw));
      break;
    case "preconditions":
      panel.appendChild(preconditionsStep(state));
      break;
    case "strategy":
      panel.appendChild(strategyStep(state, draw));
      break;
    case "apply":
      panel.appendChild(applyStep(state, draw));
      break;
  }

  const canAdvance = advanceable(state);
  const isLast = state.step === "apply";

  const foot = h(
    "div",
    { class: "card-foot" },
    idx > 0
      ? h(
          "button",
          {
            onClick: () => {
              state.step = steps[idx - 1].key;
              draw();
            },
          },
          "Back",
        )
      : h("span"),
    h(
      "div",
      { class: "row" },
      !canAdvance.ok && canAdvance.reason
        ? h("span", { class: "muted small" }, canAdvance.reason)
        : null,
      h("button", { onClick: () => navigate({ name: "library" }) }, "Cancel"),
      isLast
        ? null
        : h(
            "button",
            {
              class: "primary",
              disabled: !canAdvance.ok,
              onClick: () => void advance(state, draw),
            },
            "Next",
          ),
    ),
  );

  const title = state.snapshot
    ? `Restore ${state.snapshot.realm}${state.environment ? ` into ${state.environment}` : ""}`
    : "Restore a snapshot";

  return h(
    "div",
    null,
    h("h1", { class: "page-title" }, title),
    h(
      "div",
      { class: "page-subtitle mono" },
      state.snapshot
        ? `${state.snapshot.storage} / ${state.snapshot.bundleKey}${state.environment ? ` → ${state.environment}` : ""}`
        : "Whole-realm import. There is no cherry-picking.",
    ),
    rail,
    state.error ? notice("danger", "The restore did not start", state.error) : null,
    panel,
    h("div", { class: "card" }, foot),
  );
}

function advanceable(state: State): { ok: boolean; reason?: string } {
  switch (state.step) {
    case "snapshot":
      return state.snapshot ? { ok: true } : { ok: false, reason: "Choose a snapshot." };
    case "destination":
      return state.environment ? { ok: true } : { ok: false, reason: "Choose a destination environment." };
    case "preconditions":
      // Informative only. Next stays enabled even when every dependency is
      // missing — the operator manages these environments.
      return { ok: !state.plan?.blocked, reason: state.plan?.blockedNote };
    case "strategy":
      if (state.plan?.confirmationRequired && state.confirmRealm !== state.snapshot?.realm) {
        return { ok: false, reason: "Type the realm name to confirm an overwrite." };
      }
      return { ok: true };
    default:
      return { ok: true };
  }
}

async function advance(state: State, draw: () => void): Promise<void> {
  const idx = steps.findIndex((s) => s.key === state.step);

  // Opening the snapshot happens once, on the way into preconditions: verify
  // and decrypt before the destination is contacted at all.
  if (state.step === "destination" && !state.opened && state.snapshot) {
    state.planning = true;
    draw();
    const overview = await InspectAPI.open({
      storage: state.snapshot.storage,
      bundleKey: state.snapshot.bundleKey,
      snapshotId: state.snapshot.snapshotId,
      passphrase: "",
      identities: [],
    });
    if (overview.failure) {
      state.planning = false;
      state.error = overview.failure.message;
      draw();
      return;
    }
    state.opened = true;
  }

  if (state.step === "destination" || state.step === "preconditions") {
    state.planning = true;
    draw();
    state.plan = await RestoreAPI.plan({
      snapshotId: state.snapshot!.snapshotId,
      environment: state.environment,
      strategy: state.strategy,
    });
    state.planning = false;
  }

  state.step = steps[idx + 1].key;
  draw();
}

function snapshotStep(state: State, draw: () => void): HTMLElement {
  const tbody = h("tbody");
  for (const e of state.entries) {
    tbody.appendChild(
      h(
        "tr",
        {
          class: `selectable ${state.snapshot?.snapshotId === e.snapshotId ? "selected" : ""}`,
          onClick: () => {
            state.snapshot = e;
            state.opened = false;
            state.plan = undefined;
            draw();
          },
        },
        h("td", null, e.realm),
        h("td", null, when(e.createdAt)),
        h("td", { class: "numeric" }, e.metadataReadable ? count(e.users) : "—"),
        h("td", null, e.encrypted ? badge("Encrypted", "neutral") : badge("Unencrypted", "danger")),
        h("td", { class: "small muted" }, e.storage),
      ),
    );
  }
  if (state.entries.length === 0) {
    tbody.appendChild(h("tr", null, h("td", { colspan: "5", class: "muted" }, "No snapshots to restore.")));
  }

  return h(
    "div",
    { class: "card" },
    h("div", { class: "card-head" }, h("span", { class: "card-title" }, "Pick a snapshot")),
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
            h("th", null, "Storage"),
          ),
        ),
        tbody,
      ),
    ),
  );
}

function destinationStep(state: State, draw: () => void): HTMLElement {
  if (state.planning) return spinner("Downloading, decrypting and verifying the snapshot…");

  return h(
    "div",
    { class: "card" },
    h("div", { class: "card-head" }, h("span", { class: "card-title" }, "Destination environment")),
    h(
      "div",
      { class: "card-body" },
      h(
        "div",
        { class: "field" },
        h("label", null, "Environment"),
        select(
          state.destinations.map((e) => ({ value: e.name, label: `${e.name} — ${e.kind} · ${e.target}` })),
          state.environment,
          (v) => {
            state.environment = v;
            state.plan = undefined;
            draw();
          },
        ),
        h(
          "div",
          { class: "field-hint" },
          "Keep capture and restore environments separate where you can: a restore target carries the higher privilege, and defining it as its own entry means that credential is only present where a restore is actually intended.",
        ),
      ),
      notice(
        "info",
        "PortCloak verifies the snapshot before contacting this environment",
        "Integrity and decryption are checked first. A snapshot that cannot be proven intact is never written to a target.",
      ),
    ),
  );
}

function preconditionsStep(state: State): HTMLElement {
  if (state.planning) return spinner("Reading the destination…");
  const plan = state.plan;
  if (!plan) return spinner("Preparing…");
  if (plan.failure) return failureNotice(plan.failure);
  if (plan.blocked) return notice("danger", "This snapshot cannot be restored", plan.blockedNote ?? "");

  const pre = plan.preconditions;
  const container = h("div");

  container.appendChild(
    h(
      "div",
      { class: "card" },
      h("div", { class: "card-head" }, h("span", { class: "card-title" }, "Already passed")),
      h(
        "div",
        { class: "card-body" },
        h(
          "div",
          { class: "row small", style: "color:var(--success)" },
          "✓ Integrity verified — every artifact matches what was sealed",
        ),
        pre.decrypted
          ? h("div", { class: "row small", style: "color:var(--success)" }, "✓ Decrypted with the key supplied")
          : h("div", { class: "row small muted" }, "· This snapshot is not encrypted, so nothing needed decrypting"),
      ),
    ),
  );

  const body = h("div", { class: "card-body" });
  body.appendChild(h("p", { class: "muted small", style: "margin-top:0" }, pre.summary));
  for (const d of pre.dependencies) {
    body.appendChild(
      h(
        "div",
        { class: "notice warn", style: "margin-bottom:8px" },
        h("div", { class: "notice-title" }, `${d.name} — ${d.type}`),
        d.detectedAt ? h("div", { class: "small mono" }, d.detectedAt) : null,
        h("div", { class: "small" }, d.consequence),
        h("div", { class: "muted small" }, d.action),
      ),
    );
  }
  body.appendChild(
    notice(
      "info",
      "This step is informative and does not block",
      "Nothing here is checked off and Next stays enabled. You manage these environments and are assumed to know what is deployed where.",
    ),
  );

  container.appendChild(
    h(
      "div",
      { class: "card" },
      h(
        "div",
        { class: "card-head" },
        h("span", { class: "card-title" }, "What this realm expects to find"),
        pre.checked ? null : badge("Not checked", "warn"),
      ),
      body,
    ),
  );
  return container;
}

function strategyStep(state: State, draw: () => void): HTMLElement {
  if (state.planning) return spinner("Computing the dry run…");
  const plan = state.plan;
  if (!plan) return spinner("Preparing…");

  const cards = h("div", { style: "display:grid;grid-template-columns:repeat(3,1fr);gap:12px;margin-bottom:16px" });
  for (const s of state.strategies) {
    const chosen = state.strategy === s.value;
    cards.appendChild(
      h(
        "div",
        {
          class: "card",
          style: `margin:0;cursor:pointer;${chosen ? "border-color:var(--primary);background:#f0f7fd" : ""}`,
          onClick: async () => {
            state.strategy = s.value;
            state.confirmRealm = "";
            state.planning = true;
            draw();
            state.plan = await RestoreAPI.plan({
              snapshotId: state.snapshot!.snapshotId,
              environment: state.environment,
              strategy: state.strategy,
            });
            state.planning = false;
            draw();
          },
        },
        h(
          "div",
          { class: "card-body" },
          h(
            "div",
            { class: "row" },
            h("input", { type: "radio", checked: chosen, style: "width:auto" }),
            h("span", { style: "font-weight:500" }, s.label),
            s.needsAdminApi ? badge("Admin API", "info") : null,
          ),
          h("div", { class: "muted small", style: "margin-top:6px" }, s.description),
        ),
      ),
    );
  }

  const container = h("div", null, cards);

  if (!plan.dryRun.available) {
    container.appendChild(
      notice("warn", "No preview is available", plan.dryRun.unavailable ?? ""),
    );
    return container;
  }

  const tbody = h("tbody");
  for (const row of plan.dryRun.categories) {
    const noteColour =
      row.noteLevel === "warning" ? "var(--danger)" : row.noteLevel === "caution" ? "var(--warning)" : "var(--text-secondary)";
    tbody.appendChild(
      h(
        "tr",
        null,
        h("td", null, row.category),
        h("td", { class: "numeric", style: "color:var(--success)" }, row.create ? count(row.create) : "0"),
        h("td", { class: "numeric", style: "color:var(--warning)" }, row.overwrite ? count(row.overwrite) : "0"),
        h("td", { class: "numeric muted" }, count(row.leaveAlone)),
        h("td", { class: "small", style: `color:${noteColour}` }, row.note ?? ""),
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
        h(
          "div",
          null,
          h("span", { class: "card-title" }, "Dry run against the live realm"),
          h(
            "span",
            { class: "muted small", style: "margin-left:8px" },
            `computed for ${plan.dryRun.strategy} · nothing has been written`,
          ),
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
            h(
              "tr",
              null,
              h("th", null, "Category"),
              h("th", { class: "numeric" }, "Create"),
              h("th", { class: "numeric" }, "Overwrite"),
              h("th", { class: "numeric" }, "Leave alone"),
              h("th", null, "Note"),
            ),
          ),
          tbody,
        ),
      ),
      h("div", { class: "card-foot muted small" }, plan.dryRun.summary + " " + plan.dryRun.caveat),
    ),
  );

  if (plan.confirmationRequired) {
    container.appendChild(
      h(
        "div",
        { class: "notice danger" },
        h("div", { class: "notice-title" }, `Overwriting ${state.snapshot?.realm} replaces the realm already on ${state.environment}`),
        h(
          "div",
          { class: "small", style: "margin-bottom:8px" },
          "This is destructive and cannot be undone. Type the realm name to confirm.",
        ),
        h("input", {
          type: "text",
          placeholder: state.snapshot?.realm ?? "",
          value: state.confirmRealm,
          onInput: (e: Event) => {
            state.confirmRealm = (e.target as HTMLInputElement).value;
          },
          onChange: draw,
        }),
      ),
    );
  }
  return container;
}

function applyStep(state: State, draw: () => void): HTMLElement {
  const notes = h("ul", { class: "small", style: "margin:6px 0 0;padding-left:18px" });
  for (const n of state.outOfScope) notes.appendChild(h("li", null, n));

  return h(
    "div",
    { class: "card" },
    h("div", { class: "card-head" }, h("span", { class: "card-title" }, "Apply the import")),
    h(
      "div",
      { class: "card-body" },
      h(
        "p",
        null,
        `PortCloak will import ${state.snapshot?.realm} into ${state.environment} using the ${state.strategy} strategy.`,
      ),
      notice(
        "warn",
        "Keycloak's import is not transactional",
        "If it fails part-way, the destination is left in whatever state Keycloak reached. PortCloak reports what was applied rather than claiming a rollback it cannot perform.",
      ),
      h("div", { class: "facet-title", style: "margin-top:14px" }, "What was never carried"),
      notes,
      h(
        "div",
        { style: "margin-top:16px" },
        h(
          "button",
          {
            class: "primary",
            disabled: state.applying,
            onClick: () => void apply(state, draw),
          },
          state.applying ? "Applying…" : "Apply import",
        ),
      ),
    ),
  );
}

async function apply(state: State, draw: () => void): Promise<void> {
  state.applying = true;
  state.error = undefined;
  draw();

  const res = await RestoreAPI.apply({
    snapshotId: state.snapshot!.snapshotId,
    storage: state.snapshot!.storage,
    bundleKey: state.snapshot!.bundleKey,
    realm: state.snapshot!.realm,
    environment: state.environment,
    strategy: state.strategy,
    passphrase: "",
    identities: [],
    confirmRealm: state.confirmRealm,
  });
  state.applying = false;

  if (res.failure) {
    state.error = res.failure.message + (res.failure.hint ? ` ${res.failure.hint}` : "");
    draw();
    return;
  }
  modal({
    title: "Restore started",
    body: h(
      "div",
      null,
      h("p", null, "The import is running. Its progress, and the validation that follows it, are on the Activity screen."),
    ),
    cancelLabel: "Go to Activity",
  });
  navigate({ name: "activity" });
}
