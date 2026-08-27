import {
  InspectAPI,
  type Entities,
  type LedgerView,
  type Overview,
  type UserRow,
  type UsersQuery,
  type UsersResult,
  type VerifyReport,
} from "../api";
import {
  badge,
  bytes,
  clear,
  count,
  failureNotice,
  h,
  modal,
  notice,
  spinner,
  when,
} from "../dom";
import { navigate } from "../main";
import { keyFields, noKey } from "./key";

type Tab = "overview" | "users" | "clients" | "keys" | "federations" | "flows" | "deps" | "secrets";

const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "users", label: "Users" },
  { key: "clients", label: "Clients" },
  { key: "keys", label: "Keys" },
  { key: "federations", label: "Federations" },
  { key: "flows", label: "Auth flows" },
  { key: "deps", label: "External deps" },
  { key: "secrets", label: "Secret ledger" },
];

interface Route {
  storage: string;
  bundleKey: string;
  snapshotId: string;
  tab?: string;
}

interface State {
  route: Route;
  tab: Tab;
  overview?: Overview;
  entities?: Entities;
  ledger?: LedgerView;
  users?: UsersResult;
  query: UsersQuery;
  loading: boolean;
}

export async function renderInspector(root: HTMLElement, route: Route): Promise<void> {
  clear(root);
  root.appendChild(spinner("Downloading, decrypting and verifying…"));

  const state: State = {
    route,
    tab: (route.tab as Tab) ?? "overview",
    loading: true,
    query: {
      snapshotId: route.snapshotId,
      query: "",
      enabled: "",
      origin: "",
      secondFactor: "",
      realmRole: "",
      clientRole: "",
      client: "",
      group: "",
      requiredAction: "",
      sort: "username",
      descending: false,
      offset: 0,
      limit: 25,
    },
  };

  // A snapshot already open is reused; a new one is fetched, decrypted and
  // verified before any of it is rendered.
  let overview: Overview | null = await InspectAPI.reopen(route.snapshotId);
  if (overview.failure) {
    overview = await openWithKey(root, route);
    if (!overview) return;
  }
  state.overview = overview;
  state.loading = false;

  const draw = () => {
    clear(root);
    root.appendChild(shell(state, draw, root));
  };
  draw();
}

/** Asks for a key only when the snapshot actually needs one. */
async function openWithKey(root: HTMLElement, route: Route): Promise<Overview | null> {
  let result = await InspectAPI.open({
    storage: route.storage,
    bundleKey: route.bundleKey,
    snapshotId: route.snapshotId,
    passphrase: "",
    identities: [],
  });
  if (!result.failure) return result;

  const needsKey =
    result.failure.message.toLowerCase().includes("encrypted") ||
    result.failure.message.toLowerCase().includes("decrypt");
  if (!needsKey) {
    clear(root);
    root.appendChild(failureNotice(result.failure));
    root.appendChild(
      h("button", { style: "margin-top:12px", onClick: () => navigate({ name: "library" }) }, "Back to snapshots"),
    );
    return null;
  }

  return await new Promise<Overview | null>((resolve) => {
    // The same question the restore wizard asks, in a modal rather than on a
    // step: see views/key.ts.
    const key = noKey();
    const error = h("div");

    modal({
      title: "This snapshot is encrypted",
      body: h(
        "div",
        null,
        h(
          "p",
          { class: "muted small" },
          "The library listing needed no key. Reading inside one does.",
        ),
        error,
        keyFields(key),
      ),
      confirmLabel: "Open",
      onConfirm: async () => {
        result = await InspectAPI.open({
          storage: route.storage,
          bundleKey: route.bundleKey,
          snapshotId: route.snapshotId,
          passphrase: key.passphrase,
          identities: key.identities,
        });
        if (result.failure) {
          clear(root);
          root.appendChild(failureNotice(result.failure));
          root.appendChild(
            h("button", { style: "margin-top:12px", onClick: () => navigate({ name: "library" }) }, "Back to snapshots"),
          );
          resolve(null);
          return;
        }
        resolve(result);
      },
      cancelLabel: "Back to snapshots",
    });

    // Cancelling returns to the library rather than leaving a blank screen.
    const backdrop = document.querySelector(".modal-backdrop");
    backdrop?.addEventListener("click", (e) => {
      if (e.target === backdrop) {
        navigate({ name: "library" });
        resolve(null);
      }
    });
  });
}

function shell(state: State, draw: () => void, root: HTMLElement): HTMLElement {
  const o = state.overview!;

  const head = h(
    "div",
    null,
    h(
      "div",
      { class: "breadcrumb" },
      h("a", { onClick: () => navigate({ name: "library" }) }, "Snapshots"),
      ` / ${o.realm} · ${when(String(o.provenance?.finishedAt ?? ""))}`,
    ),
    h(
      "div",
      { class: "page-head" },
      h(
        "div",
        { class: "row" },
        h("h1", { class: "page-title", style: "margin:0" }, o.realm),
        badge(o.completeness?.verdict ?? "—", o.completeness?.verdict === "Complete" ? "ok" : "warn"),
        o.encrypted
          ? badge(`Encrypted · ${o.encryptionMode}`, "neutral")
          : badge("Unencrypted", "danger"),
        o.integrityOk ? badge("Integrity verified", "ok") : badge("Integrity failed", "danger"),
      ),
      h(
        "div",
        { class: "row" },
        h("button", { onClick: () => void verify(state) }, "Verify"),
        h(
          "button",
          {
            disabled: o.degraded,
            onClick: () => navigate({ name: "restore", snapshotId: o.snapshotId }),
          },
          "Restore…",
        ),
        h("button", { class: "danger", onClick: () => closeSnapshot(state, root) }, "Close snapshot"),
      ),
    ),
    h(
      "div",
      { class: "page-subtitle mono" },
      `${o.storage} / ${o.bundleKey} · captured from ${o.provenance?.environmentName ?? "—"} · Keycloak ${o.provenance?.keycloakVersion ?? "—"}`,
    ),
  );

  const tabBar = h("div", { class: "tabs" });
  for (const t of tabs) {
    let suffix = "";
    if (t.key === "users") suffix = ` ${count(o.counts?.users)}`;
    if (t.key === "deps" && o.dependencies?.length) suffix = ` ${o.dependencies.length}`;
    if (t.key === "secrets") suffix = ` ${o.secretCount}`;
    tabBar.appendChild(
      h(
        "div",
        {
          class: `tab ${state.tab === t.key ? "active" : ""}`,
          onClick: () => {
            state.tab = t.key;
            draw();
          },
        },
        t.label + suffix,
      ),
    );
  }

  const panel = h("div");
  const container = h(
    "div",
    null,
    head,
    o.degraded ? notice("danger", "This snapshot did not verify", o.degradedNote ?? "") : null,
    !o.encrypted ? notice("danger", "Unencrypted snapshot", o.warning ?? "") : null,
    tabBar,
    panel,
  );

  switch (state.tab) {
    case "overview":
      panel.appendChild(overviewTab(o));
      break;
    case "users":
      panel.appendChild(spinner("Building the inspection index…"));
      void loadUsers(state, panel, draw);
      break;
    case "secrets":
      panel.appendChild(spinner("Reading the ledger…"));
      void loadLedger(state, panel);
      break;
    default:
      panel.appendChild(spinner("Reading…"));
      void loadEntities(state, panel);
      break;
  }
  return container;
}

function overviewTab(o: Overview): HTMLElement {
  const stat = (value: string, label: string) =>
    h("div", null, h("div", { class: "stat-value" }, value), h("div", { class: "stat-label" }, label));

  const contents = h(
    "div",
    { class: "card" },
    h("div", { class: "card-head" }, h("span", { class: "card-title" }, "Contents")),
    h(
      "div",
      { class: "card-body" },
      h(
        "div",
        { class: "stat-grid" },
        stat(count(o.counts?.users), "Users"),
        stat(count(o.credentials?.passwordHashes), "Password hashes"),
        stat(count(o.credentials?.otp), "OTP enrolments"),
        stat(count(o.credentials?.webauthn), "Passkeys"),
      ),
      h(
        "div",
        { class: "stat-grid", style: "margin-top:18px" },
        stat(count(o.counts?.clients), "Clients"),
        stat(count(o.counts?.keyProviders), "Key providers"),
        stat(count(o.counts?.identityProviders), "Identity providers"),
        stat(count(o.counts?.federations), "User federation"),
      ),
      o.credentials?.algorithms
        ? h(
            "div",
            { class: "muted small", style: "margin-top:16px" },
            "Password hashing: " +
              Object.entries(o.credentials.algorithms)
                .map(([algo, n]) => `${algo} (${count(n)})`)
                .join(", ") +
              ". The destination's password policy has to match, or these stop verifying.",
          )
        : null,
    ),
  );

  const provenance = h("dl", { class: "kv" });
  const addProv = (k: string, v: unknown) => {
    if (v === undefined || v === null || v === "") return;
    provenance.appendChild(h("dt", null, k));
    provenance.appendChild(h("dd", null, String(v)));
  };
  const p = o.provenance as Record<string, unknown>;
  addProv("Source", `${p.environmentKind ?? ""} · ${p.target ?? ""}`);
  addProv("Keycloak version", p.keycloakVersion);
  addProv("Capture mode", p.captureMode);
  addProv(
    "Execution",
    p.executionMode === "ephemeral-clone"
      ? "ephemeral clone — the serving instance was untouched"
      : "in place, on isolated ports",
  );
  if (p.cloneRef) addProv("Clone reference", `${p.cloneRef} (destroyed)`);
  addProv("Ports", p.ports);
  addProv("Users mode", p.usersMode);
  addProv("Secret verification", p.secretVerification);
  addProv("Dependency scan", p.dependencyScan);
  addProv("Integrity", o.integrityMessage);

  const completeness = h("div", { class: "card" });
  const cbody = h("div", { class: "card-body" });
  const captured = (o.completeness?.categories ?? []).filter((c) => c.status === "captured");
  const partial = (o.completeness?.categories ?? []).filter((c) => c.status === "partial");
  const missing = (o.completeness?.categories ?? []).filter((c) => c.status === "missing");
  const outOfScope = (o.completeness?.categories ?? []).filter((c) => c.status === "outOfScope");
  const notChecked = (o.completeness?.categories ?? []).filter((c) => c.status === "notChecked");

  cbody.appendChild(
    h("div", { class: "row", style: "color:var(--success)" }, `✓ ${captured.length} categories captured`),
  );
  cbody.appendChild(
    h(
      "div",
      { class: "row", style: missing.length ? "color:var(--danger)" : "color:var(--success)" },
      `${missing.length ? "✕" : "✓"} ${missing.length} missing · ${partial.length} partial`,
    ),
  );
  for (const c of [...partial, ...missing]) {
    cbody.appendChild(h("div", { class: "muted small", style: "margin-left:16px" }, `${c.name}: ${c.reason}`));
  }
  for (const c of notChecked) {
    cbody.appendChild(
      h("div", { class: "small", style: "color:var(--warning);margin-top:6px" }, `${c.name}: ${c.reason}`),
    );
  }
  cbody.appendChild(
    h("div", { class: "facet-title", style: "margin-top:14px" }, "Out of scope by design"),
  );
  for (const c of outOfScope) {
    cbody.appendChild(h("div", { class: "muted small" }, `· ${c.name}`));
  }
  cbody.appendChild(
    h(
      "div",
      { class: "notice info small", style: "margin-top:10px" },
      "“Out of scope” is a design decision, not a failure — users re-authenticate after restore.",
    ),
  );
  completeness.appendChild(h("div", { class: "card-head" }, h("span", { class: "card-title" }, "Completeness")));
  completeness.appendChild(cbody);

  const deps = h("div", { class: "card" });
  if (o.dependencies?.length) {
    const dbody = h("div", { class: "card-body" });
    dbody.appendChild(
      h(
        "p",
        { class: "muted small", style: "margin-top:0" },
        "Provision these on the destination before importing, or the realm imports cleanly and then fails at login.",
      ),
    );
    for (const d of o.dependencies) {
      dbody.appendChild(
        h(
          "div",
          { class: "notice warn", style: "margin-bottom:8px" },
          h("div", { class: "notice-title" }, d.name),
          h("div", { class: "small" }, `${d.type}${d.detectedAt ? ` · ${d.detectedAt}` : ""}`),
        ),
      );
    }
    deps.appendChild(
      h("div", { class: "card-head" }, h("span", { class: "card-title" }, "⚠ External dependencies")),
    );
    deps.appendChild(dbody);
  }

  return h(
    "div",
    null,
    o.tokenContinuity
      ? notice("info", "Token continuity preserved", o.tokenContinuityNote)
      : notice("warn", "Token continuity not established", o.tokenContinuityNote),
    h(
      "div",
      { class: "split-wide" },
      h(
        "div",
        null,
        contents,
        h(
          "div",
          { class: "card" },
          h("div", { class: "card-head" }, h("span", { class: "card-title" }, "Provenance")),
          h("div", { class: "card-body" }, provenance),
        ),
      ),
      h("div", null, completeness, o.dependencies?.length ? deps : null),
    ),
  );
}

async function loadUsers(state: State, panel: HTMLElement, draw: () => void): Promise<void> {
  const res = await InspectAPI.users(state.query);
  state.users = res;
  clear(panel);
  if (res.failure) {
    panel.appendChild(failureNotice(res.failure));
    return;
  }
  panel.appendChild(usersTab(state, panel, draw));
}

function usersTab(state: State, panel: HTMLElement, draw: () => void): HTMLElement {
  const res = state.users!;
  const reload = () => void loadUsers(state, panel, draw);

  const facetGroup = (
    title: string,
    values: { value: string; label: string; count: number }[],
    current: string,
    onPick: (v: string) => void,
  ) => {
    if (!values || values.length === 0) return null;
    const g = h("div", { class: "facet-group" }, h("div", { class: "facet-title" }, title));
    for (const v of values.slice(0, 12)) {
      g.appendChild(
        h(
          "label",
          { class: "facet" },
          h("input", {
            type: "checkbox",
            checked: current === v.value,
            onChange: () => {
              onPick(current === v.value ? "" : v.value);
              state.query.offset = 0;
              reload();
            },
          }),
          h("span", null, v.label),
          h("span", { class: "count" }, count(v.count)),
        ),
      );
    }
    return g;
  };

  const facets = h(
    "div",
    { class: "card" },
    h("div", { class: "card-head" }, h("span", { class: "card-title" }, "Filters")),
    h(
      "div",
      { class: "card-body" },
      facetGroup("Status", res.facets.status, state.query.enabled, (v) => (state.query.enabled = v)),
      facetGroup("Origin", res.facets.origin, state.query.origin, (v) => (state.query.origin = v)),
      facetGroup("Second factor", res.facets.secondFactor, state.query.secondFactor, (v) => (state.query.secondFactor = v)),
      facetGroup("Realm role", res.facets.realmRoles, state.query.realmRole, (v) => (state.query.realmRole = v)),
      facetGroup("Group", res.facets.groups, state.query.group, (v) => (state.query.group = v)),
      facetGroup("Required action", res.facets.requiredActions, state.query.requiredAction, (v) => (state.query.requiredAction = v)),
      h(
        "div",
        { class: "notice info small", style: "margin-top:12px" },
        state.overview?.indexNote ?? "",
      ),
    ),
  );

  const search = h("input", {
    type: "text",
    placeholder: "Search username, email, name or user id",
    value: state.query.query,
    onInput: (e: Event) => {
      state.query.query = (e.target as HTMLInputElement).value;
      state.query.offset = 0;
    },
    onChange: reload,
  });
  search.addEventListener("keyup", (e) => {
    if ((e as KeyboardEvent).key === "Enter") reload();
  });

  const chips = h("div", { class: "row", style: "gap:6px;flex-wrap:wrap" });
  const addChip = (label: string, clear_: () => void) => {
    chips.appendChild(
      h(
        "span",
        { class: "chip" },
        label,
        h(
          "button",
          {
            onClick: () => {
              clear_();
              state.query.offset = 0;
              reload();
            },
          },
          "×",
        ),
      ),
    );
  };
  if (state.query.enabled) addChip(state.query.enabled === "true" ? "Enabled" : "Disabled", () => (state.query.enabled = ""));
  if (state.query.origin) addChip(state.query.origin, () => (state.query.origin = ""));
  if (state.query.secondFactor) addChip(state.query.secondFactor, () => (state.query.secondFactor = ""));
  if (state.query.realmRole) addChip(state.query.realmRole, () => (state.query.realmRole = ""));
  if (state.query.group) addChip(state.query.group, () => (state.query.group = ""));

  const tbody = h("tbody");
  for (const u of res.page.rows) {
    tbody.appendChild(userRow(state, u));
  }
  if (res.page.rows.length === 0) {
    tbody.appendChild(h("tr", null, h("td", { colspan: "6", class: "muted" }, res.empty ?? "No users.")));
  }

  const from = res.page.total === 0 ? 0 : res.page.offset + 1;
  const to = Math.min(res.page.offset + res.page.limit, res.page.total);

  const table = h(
    "div",
    { class: "card" },
    h("div", { class: "card-head" }, h("div", { class: "search" }, search), chips),
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
            h("th", null, "Username"),
            h("th", null, "Email"),
            h("th", null, "Status"),
            h("th", null, "Origin"),
            h("th", null, "Second factor"),
            h("th", null, "Groups"),
          ),
        ),
        tbody,
      ),
    ),
    h(
      "div",
      { class: "card-foot" },
      h(
        "div",
        null,
        h("div", { class: "small" }, `${from}–${to} of ${count(res.page.total)} matching`),
        h("div", { class: "muted small" }, res.note),
      ),
      h(
        "div",
        { class: "row" },
        h(
          "button",
          {
            disabled: res.page.offset === 0,
            onClick: () => {
              state.query.offset = Math.max(0, state.query.offset - state.query.limit);
              reload();
            },
          },
          "‹",
        ),
        h(
          "button",
          {
            disabled: to >= res.page.total,
            onClick: () => {
              state.query.offset += state.query.limit;
              reload();
            },
          },
          "›",
        ),
        h(
          "button",
          { onClick: () => exportView(state, "users") },
          "Export CSV",
        ),
      ),
    ),
  );

  return h("div", { class: "split" }, facets, table);
}

function userRow(state: State, u: UserRow): HTMLElement {
  const factors = h("div", { class: "row", style: "gap:4px" });
  if (u.otpCount > 0) factors.appendChild(badge(u.otpCount > 1 ? `OTP ×${u.otpCount}` : "OTP", "info"));
  if (u.webauthnCount > 0) {
    factors.appendChild(badge(u.webauthnCount > 1 ? `Passkey ×${u.webauthnCount}` : "Passkey", "info"));
  }
  if (u.otpCount === 0 && u.webauthnCount === 0) factors.appendChild(h("span", { class: "muted" }, "—"));

  return h(
    "tr",
    { class: "selectable" },
    h("td", null, h("a", { onClick: () => void showUser(state, u.id) }, u.username)),
    h("td", null, u.email || "—"),
    h(
      "td",
      null,
      h("span", { class: `dot ${u.enabled ? "ok" : "danger"}` }),
      u.enabled ? "Enabled" : "Disabled",
    ),
    h("td", null, u.origin),
    h("td", null, factors),
    h("td", { class: "small muted" }, (u.groups ?? []).join(", ") || "—"),
  );
}

async function showUser(state: State, userId: string): Promise<void> {
  const [detail, failure] = await InspectAPI.user(state.route.snapshotId, userId);
  if (failure) {
    modal({ title: "That user could not be read", body: h("div", null, failure.message) });
    return;
  }

  const creds = h("div");
  for (const c of detail.credentials ?? []) {
    creds.appendChild(
      h(
        "div",
        { class: "row small", style: "padding:4px 0" },
        badge(c.type, "neutral"),
        c.algorithm ? h("span", { class: "muted" }, `${c.algorithm}${c.iterations ? ` · ${count(c.iterations)} iterations` : ""}`) : null,
        c.created ? h("span", { class: "muted right" }, when(c.created)) : null,
      ),
    );
  }

  const attrs = h("dl", { class: "kv" });
  for (const [k, v] of Object.entries(detail.attributes ?? {})) {
    attrs.appendChild(h("dt", null, k));
    attrs.appendChild(h("dd", null, (v as string[]).join(", ")));
  }

  modal({
    title: detail.username,
    body: h(
      "div",
      null,
      h(
        "dl",
        { class: "kv" },
        h("dt", null, "Email"),
        h("dd", null, detail.email || "—"),
        h("dt", null, "Name"),
        h("dd", null, `${detail.firstName ?? ""} ${detail.lastName ?? ""}`.trim() || "—"),
        h("dt", null, "Status"),
        h("dd", null, detail.enabled ? "Enabled" : "Disabled"),
        h("dt", null, "Origin"),
        h("dd", null, detail.origin),
        h("dt", null, "Realm roles"),
        h("dd", null, (detail.realmRoles ?? []).join(", ") || "—"),
        h("dt", null, "Groups"),
        h("dd", null, (detail.groups ?? []).join(", ") || "—"),
        h("dt", null, "Required actions"),
        h("dd", null, (detail.requiredActions ?? []).join(", ") || "—"),
      ),
      h("div", { class: "facet-title", style: "margin-top:16px" }, "Credentials"),
      creds,
      h(
        "div",
        { class: "muted small", style: "margin-top:6px" },
        "Presence and metadata only. No credential value is shown, and there is no action here that would reveal one.",
      ),
      Object.keys(detail.attributes ?? {}).length > 0
        ? h("div", null, h("div", { class: "facet-title", style: "margin-top:16px" }, "Attributes"), attrs)
        : null,
    ),
    cancelLabel: "Close",
  });
}

async function loadEntities(state: State, panel: HTMLElement): Promise<void> {
  if (!state.entities) state.entities = await InspectAPI.entities(state.route.snapshotId);
  const e = state.entities;
  clear(panel);
  if (e.failure) {
    panel.appendChild(failureNotice(e.failure));
    return;
  }

  switch (state.tab) {
    case "clients":
      panel.appendChild(
        table(
          "Clients",
          ["Client ID", "Protocol", "Type", "Secret", "Mappers", "Authz"],
          e.clients.map((c) => [
            c.clientId,
            c.protocol,
            c.confidential ? "Confidential" : "Public",
            c.secretMasked
              ? badge("Masked at source", "danger")
              : c.secretPresent
                ? badge("Carried", "ok")
                : c.confidential
                  ? badge("Not carried", "warn")
                  : h("span", { class: "muted" }, "n/a"),
            String(c.mappers),
            c.authorization ? "yes" : "—",
          ]),
          "secretPresent is the column that decides whether an imported client authenticates unchanged.",
        ),
      );
      break;

    case "keys":
      panel.appendChild(
        table(
          "Key providers",
          ["KID", "Provider", "Type", "Algorithm", "Use", "Active", "Private carried"],
          e.keys.map((k) => [
            k.kid ?? "—",
            k.provider,
            k.type ?? "—",
            k.algorithm ?? "—",
            k.use ?? "—",
            k.active ? "yes" : "no",
            k.privateCarried ? badge("Carried", "ok") : badge("Not carried", "danger"),
          ]),
          "privateCarried is the token-continuity signal: tokens signed before the move stay verifiable only if the private material travelled.",
        ),
      );
      break;

    case "federations":
      panel.appendChild(
        table(
          "User federation",
          ["Name", "Provider", "Connection", "Users DN", "Bind credential", "Mappers"],
          e.federations.map((f) => [
            f.name,
            f.provider,
            f.connectionUrl ?? "—",
            f.usersDn ?? "—",
            f.bindCarried ? badge("Carried", "ok") : badge("Not carried", "danger"),
            String(f.mappers),
          ]),
          "Federated users are not duplicated into the export, so the directory has to be reachable at the destination.",
        ),
      );
      panel.appendChild(
        table(
          "Identity providers",
          ["Alias", "Protocol", "Enabled", "Secret", "Mappers"],
          e.identityProviders.map((i) => [
            i.alias,
            i.protocol,
            i.enabled ? "yes" : "no",
            i.secretCarried ? badge("Carried", "ok") : badge("Not carried", "warn"),
            String(i.mappers),
          ]),
        ),
      );
      break;

    case "flows":
      panel.appendChild(
        table(
          "Authentication flows",
          ["Alias", "Bound as", "Executions", "Built in", "Config secret"],
          e.flows.map((f) => [
            f.alias,
            f.boundAs ?? "—",
            String(f.executions),
            f.builtIn ? "yes" : "no",
            f.configSecret ? badge("Carried", "ok") : "—",
          ]),
        ),
      );
      break;

    case "deps":
      panel.appendChild(dependenciesTab(e));
      break;
  }
}

function dependenciesTab(e: Entities): HTMLElement {
  const container = h("div", null, notice(e.dependencies.length ? "warn" : "info", e.dependencyNote));
  for (const d of e.dependencies) {
    container.appendChild(
      h(
        "div",
        { class: "card" },
        h(
          "div",
          { class: "card-body" },
          h("div", { class: "row" }, h("span", { style: "font-weight:500" }, d.name), badge(d.type, "warn")),
          d.detectedAt ? h("div", { class: "muted small mono" }, d.detectedAt) : null,
          d.referencedBy ? h("div", { class: "muted small" }, `Needed by ${d.referencedBy}`) : null,
          h("div", { class: "small", style: "margin-top:6px" }, d.consequence),
          h("div", { class: "muted small" }, d.action),
        ),
      ),
    );
  }
  return container;
}

function table(
  title: string,
  headers: string[],
  rows: (string | Node)[][],
  note?: string,
): HTMLElement {
  const tbody = h("tbody");
  for (const r of rows) {
    tbody.appendChild(h("tr", null, ...r.map((cell) => h("td", null, cell))));
  }
  if (rows.length === 0) {
    tbody.appendChild(h("tr", null, h("td", { colspan: String(headers.length), class: "muted" }, "None.")));
  }
  return h(
    "div",
    { class: "card" },
    h("div", { class: "card-head" }, h("span", { class: "card-title" }, title)),
    h(
      "div",
      { class: "table-scroll" },
      h("table", null, h("thead", null, h("tr", null, ...headers.map((x) => h("th", null, x)))), tbody),
    ),
    note ? h("div", { class: "card-foot muted small" }, note) : null,
  );
}

async function loadLedger(state: State, panel: HTMLElement): Promise<void> {
  const ledger = await InspectAPI.ledger(state.route.snapshotId);
  state.ledger = ledger;
  clear(panel);
  if (ledger.failure) {
    panel.appendChild(failureNotice(ledger.failure));
    return;
  }

  const tbody = h("tbody");
  for (const entry of ledger.entries) {
    const valueCell = h("td", { class: "mono muted" }, "••••••••••••");
    const actionCell = h("td", { class: "right" });

    if (entry.revealable && ledger.revealAllowed) {
      actionCell.appendChild(
        h(
          "a",
          {
            onClick: () => revealSecret(state, entry.location, valueCell, actionCell),
          },
          "Reveal",
        ),
      );
    } else if (!entry.revealable) {
      valueCell.textContent = entry.note ?? "not carried";
      valueCell.className = "small";
      valueCell.style.color = "var(--danger)";
    } else {
      actionCell.appendChild(h("span", { class: "muted small" }, "Reveal is off"));
    }

    tbody.appendChild(
      h(
        "tr",
        null,
        h("td", { class: "mono small" }, entry.location),
        h("td", null, entry.kindLabel),
        h(
          "td",
          { style: entry.masked ? "color:var(--danger)" : "color:var(--success)" },
          entry.status,
        ),
        valueCell,
        actionCell,
      ),
    );
  }

  panel.appendChild(notice("warn", ledger.note));
  panel.appendChild(
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
              h("th", null, "Location"),
              h("th", null, "Kind"),
              h("th", null, "Status"),
              h("th", null, "Value"),
              h("th", null, ""),
            ),
          ),
          tbody,
        ),
      ),
      h(
        "div",
        { class: "card-foot" },
        h("span", { class: "small" }, ledger.summary),
        h("a", { onClick: () => exportView(state, "secretLedger") }, "Export ledger (redacted) ↓"),
      ),
    ),
  );
}

function revealSecret(
  state: State,
  location: string,
  valueCell: HTMLElement,
  actionCell: HTMLElement,
): void {
  let reason = "";
  modal({
    title: "Reveal this secret?",
    body: h(
      "div",
      null,
      h("p", { class: "mono small" }, location),
      h(
        "p",
        { class: "muted small" },
        "The value is shown once and an entry is written to the audit log naming what was revealed and when. The value itself is never written anywhere.",
      ),
      h("label", null, "Reason (optional)"),
      h("input", {
        type: "text",
        placeholder: "ticket OPS-12",
        onInput: (e: Event) => {
          reason = (e.target as HTMLInputElement).value;
        },
      }),
    ),
    confirmLabel: "Reveal",
    onConfirm: async () => {
      const res = await InspectAPI.reveal(state.route.snapshotId, location, reason);
      if (res.failure) {
        modal({ title: "Not revealed", body: h("div", null, res.failure.message) });
        return;
      }
      valueCell.textContent = res.value;
      valueCell.className = "mono";
      clear(actionCell);
      actionCell.appendChild(
        h(
          "a",
          {
            onClick: () => {
              valueCell.textContent = "••••••••••••";
              valueCell.className = "mono muted";
              clear(actionCell);
              actionCell.appendChild(
                h("a", { onClick: () => revealSecret(state, location, valueCell, actionCell) }, "Reveal"),
              );
            },
          },
          "Hide",
        ),
      );
      const row = valueCell.parentElement;
      if (row) {
        const note = h("tr", null, h("td", { colspan: "5", class: "muted small" }, res.note));
        row.after(note);
      }
    },
  });
}

async function verify(state: State): Promise<void> {
  const [report, failure] = await InspectAPI.verify(state.route.snapshotId);
  if (failure) {
    modal({ title: "Verification could not run", body: h("div", null, failure.message) });
    return;
  }
  modal({ title: "Verification", body: verifyBody(report), cancelLabel: "Close" });
}

function verifyBody(report: VerifyReport): HTMLElement {
  const rows = h("tbody");
  for (const a of report.artifacts) {
    rows.appendChild(
      h(
        "tr",
        null,
        h("td", { class: "mono small" }, a.name),
        h("td", null, a.ok ? badge("OK", "ok") : badge("Failed", "danger")),
        h("td", { class: "muted small" }, a.note ?? ""),
      ),
    );
  }
  return h(
    "div",
    null,
    notice(report.ok ? "ok" : "danger", report.message, report.note),
    h(
      "div",
      { class: "table-scroll" },
      h(
        "table",
        null,
        h("thead", null, h("tr", null, h("th", null, "Artifact"), h("th", null, ""), h("th", null, ""))),
        rows,
      ),
    ),
  );
}

function exportView(state: State, view: string): void {
  let path = `${view}.${view === "users" || view === "secretLedger" ? "csv" : "json"}`;
  let format = path.endsWith(".csv") ? "csv" : "json";

  modal({
    title: "Export this view",
    body: h(
      "div",
      null,
      h(
        "p",
        { class: "muted small" },
        "The export carries the rows currently shown, redacted by the same rules as the screen — presence, never values. Exporting is itself recorded in the audit log.",
      ),
      h("label", null, "Destination path"),
      h("input", {
        type: "text",
        value: path,
        onInput: (e: Event) => {
          path = (e.target as HTMLInputElement).value;
          format = path.endsWith(".json") ? "json" : "csv";
        },
      }),
    ),
    confirmLabel: "Export",
    onConfirm: async () => {
      const [res, failure] = await InspectAPI.exportView(
        state.route.snapshotId,
        { view, format, path },
        state.query,
      );
      if (failure) {
        modal({ title: "Not exported", body: h("div", null, failure.message) });
        return;
      }
      modal({
        title: "Exported",
        body: h(
          "div",
          null,
          h("p", { class: "mono small" }, res.path),
          h("p", null, `${count(res.rows)} rows · ${bytes(res.bytes)}`),
          h("p", { class: "muted small" }, res.note),
        ),
        cancelLabel: "Close",
      });
    },
  });
}

function closeSnapshot(state: State, root: HTMLElement): void {
  modal({
    title: "Close this snapshot?",
    body: h(
      "div",
      null,
      h(
        "p",
        null,
        "PortCloak will drop the inspection index and shred every decrypted working file for this snapshot.",
      ),
      h(
        "p",
        { class: "muted small" },
        "Re-opening pays the index build again. That cost is deliberate: an index is a searchable copy of your user directory, and leaving one on this workstation between sessions is the worse liability.",
      ),
    ),
    confirmLabel: "Close snapshot",
    onConfirm: async () => {
      const res = await InspectAPI.close(state.route.snapshotId);
      if (res.failure) {
        modal({ title: "Not closed", body: h("div", null, res.failure.message) });
        return;
      }
      clear(root);
      root.appendChild(notice("ok", "Snapshot closed", res.confirmed));
      root.appendChild(
        h(
          "button",
          { style: "margin-top:12px", onClick: () => navigate({ name: "library" }) },
          "Back to snapshots",
        ),
      );
    },
  });
}
