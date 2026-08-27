import {
  ConfigAPI,
  type ConfigSnapshot,
  type Environment,
  type EnvironmentKind,
  type EnvironmentView,
  type ProbeResult,
} from "../api";
import { badge, clear, failureNotice, field, h, input, modal, notice, select, spinner } from "../dom";
import { navigate } from "../main";
import { probePanel } from "./capture";

const kinds: { value: EnvironmentKind; label: string }[] = [
  { value: "local", label: "Local" },
  { value: "ssh", label: "SSH" },
  { value: "docker", label: "Docker" },
  { value: "kubernetes", label: "Kubernetes / OpenShift" },
];

interface State {
  snapshot?: ConfigSnapshot;
  selected?: string;
  draft?: Environment;
  originalName: string;
  secret: string;
  adminSecret: string;
  probe?: ProbeResult;
  probing: boolean;
  saving: boolean;
  error?: string;
}

export async function renderEnvironments(root: HTMLElement, select_?: string): Promise<void> {
  clear(root);
  root.appendChild(spinner("Loading configuration…"));

  const snapshot = await ConfigAPI.load();
  const state: State = {
    snapshot,
    selected: select_ ?? snapshot.environments[0]?.name,
    originalName: "",
    secret: "",
    adminSecret: "",
    probing: false,
    saving: false,
  };
  if (state.selected) {
    state.draft = { ...snapshot.environments.find((e) => e.name === state.selected)! };
    state.originalName = state.selected;
  }

  const draw = () => {
    clear(root);
    root.appendChild(page(state, draw, root));
  };
  draw();
}

function page(state: State, draw: () => void, root: HTMLElement): HTMLElement {
  const snapshot = state.snapshot!;

  const head = h(
    "div",
    { class: "page-head" },
    h(
      "div",
      null,
      h("h1", { class: "page-title" }, "Environments"),
      h(
        "div",
        { class: "page-subtitle" },
        "Where Keycloak runs. PortCloak captures from these and restores into them.",
      ),
    ),
    h(
      "button",
      {
        class: "primary",
        onClick: () => {
          state.draft = { name: "", kind: "local" };
          state.originalName = "";
          state.selected = undefined;
          state.probe = undefined;
          state.secret = "";
          state.adminSecret = "";
          draw();
        },
      },
      "Add environment",
    ),
  );

  const container = h("div", null, head);

  if (snapshot.loadProblems && snapshot.loadProblems.length > 0) {
    container.appendChild(configProblems(snapshot, root));
  }

  container.appendChild(
    h("div", { class: "split" }, list(state, draw), state.draft ? editor(state, draw, root) : placeholder()),
  );
  return container;
}

/** A malformed config is shown with its line numbers rather than swallowed. */
function configProblems(snapshot: ConfigSnapshot, root: HTMLElement): HTMLElement {
  const list = h("ul", { class: "small", style: "margin:8px 0 0;padding-left:18px" });
  for (const p of snapshot.loadProblems ?? []) {
    list.appendChild(h("li", null, p.line > 0 ? `Line ${p.line}: ${p.message}` : p.message));
  }
  return h(
    "div",
    { class: "notice danger" },
    h("div", { class: "notice-title" }, `${snapshot.configFile} could not be loaded`),
    h(
      "div",
      { class: "small" },
      "PortCloak refuses to start with a half-parsed config rather than silently dropping entries. Fix these and reload.",
    ),
    list,
    h(
      "button",
      {
        style: "margin-top:10px",
        onClick: async () => {
          await ConfigAPI.reload();
          void renderEnvironments(root);
        },
      },
      "Reload config.yaml",
    ),
  );
}

function placeholder(): HTMLElement {
  return h(
    "div",
    { class: "card" },
    h(
      "div",
      { class: "card-body muted" },
      "Select an environment on the left, or add one.",
    ),
  );
}

function list(state: State, draw: () => void): HTMLElement {
  const envs = state.snapshot!.environments;
  const items = h("div");

  for (const env of envs) {
    items.appendChild(
      h(
        "div",
        {
          class: `nav-item ${state.selected === env.name ? "selected" : ""}`,
          style: `padding:12px 16px;border-left:3px solid ${state.selected === env.name ? "var(--primary)" : "transparent"};background:${state.selected === env.name ? "#f0f7fd" : "transparent"};color:var(--text);display:block;border-bottom:1px solid #ededed`,
          onClick: () => {
            state.selected = env.name;
            state.draft = { ...env };
            state.originalName = env.name;
            state.probe = undefined;
            state.secret = "";
            state.adminSecret = "";
            state.error = undefined;
            draw();
          },
        },
        h("div", { style: "font-weight:500" }, env.name),
        h("div", { class: "muted small" }, `${kindLabel(env.kind)} · ${env.target}`),
        probeLine(env),
      ),
    );
  }

  return h(
    "div",
    { class: "card" },
    h("div", { class: "card-head" }, h("span", { class: "muted small" }, `${envs.length} environments`)),
    items,
  );
}

/**
 * A cached "reachable" from three weeks ago is worse than no information,
 * because it is believed. Staleness is shown, not hidden.
 */
function probeLine(env: EnvironmentView): HTMLElement {
  if (!env.lastProbe) {
    return h("div", { class: "small", style: "color:var(--text-muted)" }, "Never tested");
  }
  const tone = env.stale ? "var(--danger)" : env.lastProbe.ok ? "var(--success)" : "var(--danger)";
  const label = env.lastProbe.ok ? `Tested ${env.probeAge}` : `Failed ${env.probeAge}`;
  return h(
    "div",
    { class: "small", style: `color:${tone}` },
    `${label}${env.lastProbe.keycloakVersion ? ` · Keycloak ${env.lastProbe.keycloakVersion}` : ""}${env.stale ? " — stale" : ""}`,
  );
}

function kindLabel(kind: string): string {
  return kinds.find((k) => k.value === kind)?.label ?? kind;
}

function editor(state: State, draw: () => void, root: HTMLElement): HTMLElement {
  const env = state.draft!;

  const tabs = h("div", { class: "tabs" });
  for (const k of kinds) {
    tabs.appendChild(
      h(
        "div",
        {
          class: `tab ${env.kind === k.value ? "active" : ""}`,
          onClick: () => {
            env.kind = k.value;
            state.probe = undefined;
            draw();
          },
        },
        k.label,
      ),
    );
  }

  const form = h("div", null);
  form.appendChild(
    field("Name", input(env.name, (v) => (env.name = v)), "How PortCloak refers to this environment everywhere."),
  );

  switch (env.kind) {
    case "local":
      form.appendChild(
        field(
          "Keycloak server folder",
          input(env.serverFolder, (v) => (env.serverFolder = v), { placeholder: "/opt/keycloak" }),
          "The install root — the folder containing bin/kc.sh, not the bin folder itself.",
        ),
      );
      form.appendChild(
        field("Java home (optional)", input(env.javaHome, (v) => (env.javaHome = v))),
      );
      break;

    case "ssh":
      form.appendChild(
        h(
          "div",
          { class: "field-row" },
          field("Host", input(env.host, (v) => (env.host = v))),
          field(
            "Port",
            input(String(env.port ?? 22), (v) => (env.port = Number(v) || 22), { type: "number" }),
          ),
        ),
      );
      form.appendChild(
        h(
          "div",
          { class: "field-row" },
          field("User", input(env.user, (v) => (env.user = v))),
          field(
            "Authentication",
            select(
              [
                { value: "key", label: "Private key" },
                { value: "agent", label: "SSH agent" },
                { value: "password", label: "Password" },
              ],
              env.auth ?? "key",
              (v) => {
                env.auth = v as Environment["auth"];
                draw();
              },
            ),
          ),
        ),
      );
      form.appendChild(
        field(
          "Keycloak server folder on the host",
          input(env.serverFolder, (v) => (env.serverFolder = v), { placeholder: "/opt/keycloak" }),
        ),
      );
      if (env.auth === "key" || env.auth === "password") {
        form.appendChild(credentialField(state, env.auth === "key" ? "Private key" : "Password", draw));
      } else {
        form.appendChild(
          h(
            "div",
            { class: "field-hint", style: "margin-bottom:16px" },
            "Agent authentication stores no secret at all — PortCloak asks the running agent when it connects.",
          ),
        );
      }
      break;

    case "docker":
      form.appendChild(
        field(
          "Docker endpoint",
          input(env.dockerEndpoint, (v) => (env.dockerEndpoint = v), {
            placeholder: "unix:///var/run/docker.sock",
          }),
          "A local socket, a DOCKER_HOST over TLS, or Docker over SSH.",
        ),
      );
      form.appendChild(
        field(
          "Service or container running Keycloak",
          input(env.container, (v) => (env.container = v), { placeholder: "keycloak" }),
          "PortCloak inspects this container read-only and runs the export inside a throwaway clone of it.",
        ),
      );
      break;

    case "kubernetes":
      form.appendChild(
        h(
          "div",
          { class: "field-row" },
          field("Name", input(env.name, (v) => (env.name = v))),
          field(
            "Kubeconfig context",
            input(env.context, (v) => (env.context = v), { placeholder: "prod-cluster" }),
          ),
        ),
      );
      form.appendChild(
        h(
          "div",
          { class: "field-row" },
          field("Namespace", input(env.namespace, (v) => (env.namespace = v), { placeholder: "iam-prod" })),
          field(
            "Deployment or StatefulSet",
            input(env.workload, (v) => (env.workload = v), { placeholder: "statefulset/keycloak" }),
          ),
        ),
      );
      form.appendChild(
        field(
          "Container name (optional)",
          input(env.containerName, (v) => (env.containerName = v)),
          "Only needed when the pod runs more than one container.",
        ),
      );
      break;
  }

  form.appendChild(
    field(
      "Admin API base URL · optional, used only to verify secrets and detect themes",
      input(env.adminBaseUrl, (v) => (env.adminBaseUrl = v), { placeholder: "https://sso.example" }),
      "The export reads the realm from the database and does not need this. Without it, verification and dependency detection are reported as skipped.",
    ),
  );
  if (env.adminBaseUrl) {
    form.appendChild(
      h(
        "div",
        { class: "field-row" },
        field("Admin user", input(env.adminUser, (v) => (env.adminUser = v))),
        field(
          "Admin credential",
          h(
            "div",
            { class: "row" },
            h("input", {
              type: "password",
              placeholder: env.adminCredentialRef ? "•••••••• (stored)" : "password",
              onInput: (e: Event) => {
                state.adminSecret = (e.target as HTMLInputElement).value;
              },
            }),
          ),
          env.adminCredentialRef
            ? `Stored in this machine's keychain as ${env.adminCredentialRef}. config.yaml holds only the handle.`
            : "Stored in this machine's keychain; config.yaml will hold only a handle.",
        ),
      ),
    );
  }

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
              onClick: async () => {
                await ConfigAPI.duplicateEnvironment(state.originalName);
                void renderEnvironments(root);
              },
            },
            "Duplicate",
          )
        : null,
      state.originalName
        ? h(
            "a",
            { style: "color:var(--danger)", onClick: () => confirmDelete(state, root) },
            "Delete",
          )
        : null,
    ),
    h(
      "div",
      { class: "row" },
      h("button", { onClick: () => navigate({ name: "environments" }) }, "Cancel"),
      h(
        "button",
        {
          class: "primary",
          disabled: state.saving || !env.name,
          onClick: () => void save(state, root),
        },
        state.saving ? "Saving…" : "Save",
      ),
    ),
  );

  const probeArea = h("div", { class: "card-body", style: "border-top:1px solid var(--border)" });
  probeArea.appendChild(
    h(
      "div",
      { class: "row" },
      h("span", { style: "font-weight:500" }, "Test connection"),
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
              state.probe = await ConfigAPI.testEnvironment(state.originalName);
              state.probing = false;
              draw();
            },
          },
          state.probing ? "Testing…" : "Test connection",
        ),
      ),
    ),
  );
  if (!state.originalName) {
    probeArea.appendChild(
      h("div", { class: "muted small" }, "Save the environment first, then test it."),
    );
  }
  if (state.probing) probeArea.appendChild(spinner("Probing…"));
  if (state.probe?.failure) probeArea.appendChild(failureNotice(state.probe.failure));
  if (state.probe && !state.probe.failure) {
    probeArea.appendChild(probePanel(state.probe.facts, state.probe.ok));
  }

  const selected = state.snapshot!.environments.find((e) => e.name === state.originalName);
  const readiness =
    selected && !selected.readiness.ready
      ? notice("warn", "Not usable yet", selected.readiness.reason ?? "")
      : null;
  const credentialGap =
    selected && !selected.credentialPresent
      ? notice(
          "warn",
          "This environment's credential is not in this machine's keychain",
          "Configuration is portable between machines; the secrets deliberately are not. Enter it again below.",
        )
      : null;

  return h(
    "div",
    { class: "card" },
    h(
      "div",
      { class: "card-head" },
      h(
        "div",
        { class: "row" },
        h("span", { class: "card-title" }, env.name || "New environment"),
        badge(kindLabel(env.kind), "info"),
      ),
    ),
    h(
      "div",
      { class: "card-body" },
      tabs,
      state.error ? notice("danger", "Not saved", state.error) : null,
      readiness,
      credentialGap,
      form,
    ),
    probeArea,
    foot,
  );
}

function credentialField(state: State, label: string, draw: () => void): HTMLElement {
  const env = state.draft!;
  return field(
    label,
    h(
      "div",
      { class: "row" },
      h("input", {
        type: "password",
        placeholder: env.credentialRef ? "•••••••• (stored)" : label,
        onInput: (e: Event) => {
          state.secret = (e.target as HTMLInputElement).value;
        },
        onChange: draw,
      }),
    ),
    env.credentialRef
      ? `Stored in this machine's keychain as ${env.credentialRef}. config.yaml holds only the handle above.`
      : "Stored in this machine's keychain; config.yaml will hold only a handle.",
  );
}

async function save(state: State, root: HTMLElement): Promise<void> {
  state.saving = true;
  state.error = undefined;

  const env = state.draft!;
  const adminSecret = state.adminSecret;

  const failure = await ConfigAPI.saveEnvironment(state.originalName, env, state.secret);
  if (failure) {
    state.saving = false;
    state.error = failure.message;
    void renderEnvironments(root, state.originalName || undefined);
    return;
  }
  if (adminSecret) {
    await ConfigAPI.saveAdminCredential(env.name, adminSecret);
  }
  void renderEnvironments(root, env.name);
}

function confirmDelete(state: State, root: HTMLElement): void {
  const name = state.originalName;
  modal({
    title: `Delete the environment “${name}”?`,
    body: h(
      "div",
      null,
      h(
        "p",
        null,
        "The definition and its keychain secrets are removed from this machine.",
      ),
      h(
        "p",
        { class: "muted small" },
        "Snapshots already captured from it keep its name in their provenance — deleting an environment does not rewrite history, and nothing on the Keycloak itself is touched.",
      ),
    ),
    confirmLabel: "Delete environment",
    confirmTone: "danger-solid",
    onConfirm: async () => {
      const failure = await ConfigAPI.deleteEnvironment(name);
      if (failure) {
        modal({ title: "Not deleted", body: h("div", null, failure.message) });
        return;
      }
      void renderEnvironments(root);
    },
  });
}
