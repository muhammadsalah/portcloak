import {
  CaptureAPI,
  ConfigAPI,
  KeysAPI,
  type KeyRecipient,
  type CaptureOptions,
  type ProbeResult,
  type TargetFacts,
  type WizardDefaults,
} from "../api";
import {
  badge,
  bytes,
  checkbox,
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
} from "../dom";
import { navigate } from "../main";

type Step = "source" | "realms" | "options" | "storage" | "review";

const steps: { key: Step; label: string }[] = [
  { key: "source", label: "Source & probe" },
  { key: "realms", label: "Realms" },
  { key: "options", label: "Options" },
  { key: "storage", label: "Storage sink" },
  { key: "review", label: "Review & run" },
];

interface State {
  step: Step;
  defaults?: WizardDefaults;
  environment: string;
  probe?: ProbeResult;
  probing: boolean;
  realmsNote: string;
  discoveredRealms: string[];
  realmsDiscovered: boolean;
  realms: string[];
  manualRealm: string;
  storage: string;
  usersMode: string;
  usersPerFile: number;
  verify: boolean;
  detectDependencies: boolean;
  encrypt: boolean;
  encryptionMode: "passphrase" | "recipients";
  passphrase: string;
  recipients: string[];
  /** The keys already stored on this machine, offered by name. */
  storedKeys: KeyRecipient[];
  /** Remember this capture's passphrase in the keychain, under this name. */
  rememberPassphraseAs: string;
  acknowledgedUnencrypted: boolean;
  starting: boolean;
  error?: string;
}

export async function renderCapture(root: HTMLElement): Promise<void> {
  clear(root);
  root.appendChild(spinner("Loading environments…"));

  const [defaults, storedKeys] = await Promise.all([
    CaptureAPI.defaults(),
    // Offered by name rather than as a public key to paste. A key PortCloak
    // already holds is the one an operator will actually be able to restore
    // with.
    KeysAPI.recipients().catch(() => [] as KeyRecipient[]),
  ]);
  const state: State = {
    step: "source",
    defaults,
    environment: defaults.environments[0]?.name ?? "",
    probing: false,
    realmsNote: "",
    discoveredRealms: [],
    realmsDiscovered: false,
    realms: [],
    manualRealm: "",
    storage: defaults.defaultStorage || defaults.storages[0]?.name || "",
    usersMode: defaults.preferences.usersMode ?? "different_files",
    usersPerFile: defaults.preferences.usersPerFile ?? 1000,
    verify: defaults.preferences.verifyByDefault !== false,
    detectDependencies: defaults.preferences.verifyByDefault !== false,
    encrypt: defaults.preferences.encryptByDefault !== false,
    encryptionMode: "passphrase",
    passphrase: "",
    recipients: [],
    storedKeys,
    rememberPassphraseAs: "",
    acknowledgedUnencrypted: false,
    starting: false,
  };

  const draw = () => {
    clear(root);
    root.appendChild(
      h(
        "div",
        null,
        h("h1", { class: "page-title" }, "Capture snapshot"),
        h("div", { class: "page-subtitle" }, subtitle(state)),
      ),
    );
    root.appendChild(wizard(state, draw));
  };
  draw();
}

function subtitle(s: State): string {
  const env = s.defaults?.environments.find((e) => e.name === s.environment);
  if (!env) return "Choose where Keycloak runs and where the snapshot should go.";
  const realms = s.realms.length ? s.realms.join(", ") : "no realm selected";
  return `Realm ${realms} · source “${env.name}” · ${kindLabel(env.kind)}`;
}

function kindLabel(kind: string): string {
  switch (kind) {
    case "local":
      return "Local";
    case "ssh":
      return "SSH";
    case "docker":
      return "Docker";
    case "kubernetes":
      return "Kubernetes / OpenShift";
    default:
      return kind;
  }
}

function wizard(state: State, draw: () => void): HTMLElement {
  const currentIndex = steps.findIndex((s) => s.key === state.step);

  const stepList = h("div", { class: "wizard-steps" });
  steps.forEach((step, i) => {
    const done = i < currentIndex;
    const active = i === currentIndex;
    stepList.appendChild(
      h(
        "div",
        {
          class: `wizard-step ${active ? "active" : ""} ${done ? "done" : ""}`,
          onClick: () => {
            // Steps already passed can be revisited; later ones cannot be
            // jumped to, because each depends on the one before.
            if (i <= currentIndex) {
              state.step = step.key;
              draw();
            }
          },
        },
        h("span", { class: `step-mark ${done ? "done" : active ? "active" : ""}` }, done ? "✓" : String(i + 1)),
        step.label,
      ),
    );
  });

  const panel = h("div", { class: "wizard-panel" });
  switch (state.step) {
    case "source":
      panel.appendChild(sourceStep(state, draw));
      break;
    case "realms":
      panel.appendChild(realmsStep(state, draw));
      break;
    case "options":
      panel.appendChild(optionsStep(state, draw));
      break;
    case "storage":
      panel.appendChild(storageStep(state, draw));
      break;
    case "review":
      panel.appendChild(reviewStep(state));
      break;
  }

  const canAdvance = advanceable(state);
  const isLast = state.step === "review";

  const foot = h(
    "div",
    { class: "wizard-foot" },
    h(
      "button",
      {
        class: "primary",
        disabled: !canAdvance.ok || state.starting,
        onClick: () => {
          if (isLast) {
            void start(state, draw);
          } else {
            state.step = steps[currentIndex + 1].key;
            draw();
          }
        },
      },
      isLast ? "Start capture" : "Next",
    ),
    currentIndex > 0
      ? h(
          "button",
          {
            onClick: () => {
              state.step = steps[currentIndex - 1].key;
              draw();
            },
          },
          "Back",
        )
      : null,
    !canAdvance.ok && canAdvance.reason
      ? h("span", { class: "muted small" }, canAdvance.reason)
      : null,
    h("span", { class: "spacer" }),
    h("a", { onClick: () => navigate({ name: "library" }) }, "Cancel"),
  );

  return h(
    "div",
    null,
    state.error ? notice("danger", "The capture did not start", state.error) : null,
    h("div", { class: "wizard" }, stepList, panel),
    h("div", { class: "card", style: "margin-top:-1px" }, foot),
  );
}

function advanceable(s: State): { ok: boolean; reason?: string } {
  switch (s.step) {
    case "source":
      if (!s.environment) return { ok: false, reason: "Choose an environment." };
      if (!s.probe) return { ok: false, reason: "Run Test first, so a failure is a sentence now rather than a surprise later." };
      if (!s.probe.ok) return { ok: false, reason: "The probe found something that would stop a capture." };
      return { ok: true };
    case "realms":
      return s.realms.length > 0
        ? { ok: true }
        : { ok: false, reason: "Select at least one realm." };
    case "options":
      if (s.encrypt && s.encryptionMode === "passphrase" && !s.passphrase) {
        return { ok: false, reason: "Enter a passphrase, or switch to recipients." };
      }
      if (s.encrypt && s.encryptionMode === "recipients" && s.recipients.length === 0) {
        return { ok: false, reason: "Add at least one age recipient." };
      }
      if (!s.encrypt && !s.acknowledgedUnencrypted) {
        return { ok: false, reason: "Confirm that this snapshot may be written unencrypted." };
      }
      return { ok: true };
    case "storage":
      return s.storage ? { ok: true } : { ok: false, reason: "Choose where the snapshot should go." };
    default:
      return { ok: true };
  }
}

function sourceStep(state: State, draw: () => void): HTMLElement {
  const envs = state.defaults?.environments ?? [];

  const body = h(
    "div",
    null,
    h("h2", { style: "font-size:17px;margin-bottom:12px" }, "Source & probe"),
    field(
      "Environment",
      select(
        envs.map((e) => ({ value: e.name, label: `${e.name} — ${kindLabel(e.kind)} · ${e.target}` })),
        state.environment,
        (v) => {
          state.environment = v;
          state.probe = undefined;
          state.realms = [];
          state.discoveredRealms = [];
          draw();
        },
      ),
      "PortCloak reads this environment. It never restarts or reconfigures the instance serving your logins.",
    ),
    h(
      "button",
      {
        disabled: !state.environment || state.probing,
        onClick: async () => {
          state.probing = true;
          draw();
          state.probe = await ConfigAPI.testEnvironment(state.environment);
          state.probing = false;
          if (state.probe.ok) await loadRealms(state);
          draw();
        },
      },
      state.probing ? "Testing…" : "Test connection",
    ),
  );

  if (state.probing) body.appendChild(h("div", { style: "margin-top:12px" }, spinner("Probing…")));
  if (state.probe?.failure) body.appendChild(h("div", { style: "margin-top:12px" }, failureNotice(state.probe.failure)));
  if (state.probe && !state.probe.failure) {
    body.appendChild(h("div", { style: "margin-top:16px" }, probePanel(state.probe.facts, state.probe.ok)));
  }
  return body;
}

/** The probe result reports the facts a capture depends on, not a green tick. */
export function probePanel(facts: TargetFacts, ok: boolean): HTMLElement {
  const rows = h("dl", { class: "kv", style: "margin-top:10px" });
  for (const check of facts.checks) {
    const tone =
      check.status === "fail" ? "danger" : check.status === "warn" ? "warn" : check.status === "skipped" ? "neutral" : "";
    rows.appendChild(h("dt", null, check.name));
    rows.appendChild(
      h(
        "dd",
        null,
        h("span", { class: tone ? `badge ${tone}` : "" }, check.value),
        check.advice ? h("div", { class: "muted small" }, check.advice) : null,
      ),
    );
  }

  return h(
    "div",
    { class: `notice ${ok ? "ok" : "danger"}` },
    h(
      "div",
      { class: "notice-title" },
      ok ? "Probe passed — capture will not touch the serving instance" : "The probe found a blocking problem",
    ),
    rows,
    h("div", { class: "muted small", style: "margin-top:10px" }, facts.readOnlyNote),
  );
}

async function loadRealms(state: State): Promise<void> {
  const res = await CaptureAPI.realms(state.environment);
  state.discoveredRealms = res.realms ?? [];
  state.realmsDiscovered = res.discovered;
  state.realmsNote = res.note;
}

function realmsStep(state: State, draw: () => void): HTMLElement {
  const body = h(
    "div",
    null,
    h("h2", { style: "font-size:17px;margin-bottom:6px" }, "Realms"),
    h("p", { class: "muted small" }, state.realmsNote),
  );

  if (state.realmsDiscovered && state.discoveredRealms.length > 0) {
    const list = h("div", { class: "card" });
    const inner = h("div", { class: "card-body" });
    for (const realm of state.discoveredRealms) {
      inner.appendChild(
        checkbox(state.realms.includes(realm), realm, "", (on) => {
          state.realms = on
            ? [...state.realms, realm]
            : state.realms.filter((r) => r !== realm);
          draw();
        }),
      );
    }
    list.appendChild(inner);
    body.appendChild(list);
  } else {
    body.appendChild(
      field(
        "Realm name",
        h("input", {
          type: "text",
          value: state.manualRealm,
          placeholder: "acme",
          onInput: (e: Event) => {
            state.manualRealm = (e.target as HTMLInputElement).value.trim();
            state.realms = state.manualRealm ? [state.manualRealm] : [];
          },
          onChange: draw,
        }),
        "The export names its output after the realm, so this has to match exactly.",
      ),
    );
  }

  if (state.realms.length > 1) {
    body.appendChild(
      notice(
        "info",
        `${state.realms.length} realms selected — that is ${state.realms.length} snapshots`,
        "One snapshot holds exactly one realm, so each is sealed, uploaded and reported independently. They share one execution context, and on Docker or Kubernetes one ephemeral clone. If one realm fails, the others still complete.",
      ),
    );
  }
  return body;
}

function optionsStep(state: State, draw: () => void): HTMLElement {
  const body = h("div", null, h("h2", { style: "font-size:17px;margin-bottom:12px" }, "Options"));

  if (state.probe?.ok) {
    body.appendChild(probeSummary(state.probe.facts));
  }

  body.appendChild(
    field(
      "Users export mode",
      select(
        [
          { value: "different_files", label: `different_files — ${state.usersPerFile} users per file` },
          { value: "realm_file", label: "realm_file — users inside the realm document" },
        ],
        state.usersMode,
        (v) => {
          state.usersMode = v;
          draw();
        },
      ),
      "Bounded file sizes let PortCloak checkpoint per file — better behaviour on flaky links, and what makes a very large realm survivable.",
    ),
  );

  body.appendChild(
    checkbox(
      state.verify,
      "Verify secrets are unmasked (Admin API)",
      "Catches version-specific masking so a dud client secret is flagged, not shipped. Skipped without complaint if the Admin API is not reachable.",
      (v) => {
        state.verify = v;
        draw();
      },
    ),
  );
  body.appendChild(
    checkbox(
      state.detectDependencies,
      "Detect external dependencies (themes, provider JARs)",
      "Reported as restore preconditions. PortCloak never migrates these files.",
      (v) => {
        state.detectDependencies = v;
        draw();
      },
    ),
  );

  body.appendChild(h("hr", { style: "border:none;border-top:1px solid var(--border);margin:18px 0" }));
  body.appendChild(encryptionSection(state, draw));
  return body;
}

function probeSummary(facts: TargetFacts): HTMLElement {
  const parts: string[] = [];
  if (facts.keycloakVersion) parts.push(`Keycloak ${facts.keycloakVersion}`);
  if (facts.kcPath) parts.push(`kc.sh at ${facts.kcPath}`);
  if (facts.cloneCapable) parts.push("ephemeral clone permitted");
  if (facts.ports?.http) {
    parts.push(`ports ${facts.ports.http} / ${facts.ports.https} / ${facts.ports.management} allocated`);
  }
  if (facts.freeBytes) parts.push(`${bytes(facts.freeBytes)} free`);
  parts.push(facts.adminReachable ? "Admin API reachable" : "Admin API not reachable");

  return h(
    "div",
    { class: "notice ok" },
    h("div", { class: "notice-title" }, "Probe passed — capture will not touch the serving instance"),
    h("div", { class: "small" }, parts.join(" · ")),
  );
}

function encryptionSection(state: State, draw: () => void): HTMLElement {
  const head = h(
    "div",
    { class: "row" },
    h("span", { style: "font-weight:500" }, "🔒 Encrypt this snapshot"),
    h("span", { class: "right" }, toggle(state.encrypt, (on) => {
      state.encrypt = on;
      if (!on) {
        // Declining is one deliberate action: the notice is shown in full and
        // confirmed, rather than being a default nobody noticed.
        confirmDecline(state, draw);
      } else {
        state.acknowledgedUnencrypted = false;
      }
      draw();
    })),
  );

  const body = h("div", null, head, h("p", { class: "muted small" }, state.defaults?.encryptionNotice ?? ""));

  if (state.encrypt) {
    const mode = select(
      [
        { value: "passphrase", label: "Passphrase" },
        { value: "recipients", label: "Recipients (age)" },
      ],
      state.encryptionMode,
      (v) => {
        state.encryptionMode = v as "passphrase" | "recipients";
        draw();
      },
    );
    mode.style.maxWidth = "200px";

    const detail =
      state.encryptionMode === "passphrase"
        ? h("input", {
            type: "password",
            placeholder: "Passphrase",
            value: state.passphrase,
            onInput: (e: Event) => {
              state.passphrase = (e.target as HTMLInputElement).value;
            },
            onChange: draw,
          })
        : recipientsEditor(state, draw);

    body.appendChild(h("div", { class: "row", style: "align-items:flex-start;gap:12px" }, mode, h("div", { class: "grow" }, detail)));
    if (state.encryptionMode === "passphrase") {
      body.appendChild(rememberPassphrase(state));
    }
  } else if (state.acknowledgedUnencrypted) {
    body.appendChild(
      notice("danger", "This snapshot will be written unencrypted", state.defaults?.declineNotice ?? ""),
    );
  }
  return body;
}

/**
 * Who a snapshot is sealed to.
 *
 * The keys PortCloak already holds come first, by name. Pasting a public key
 * still works — a colleague's key is a legitimate recipient and PortCloak will
 * never hold its private half — but it is no longer the only way in, which is
 * what made recipient mode something operators read about and then skipped.
 */
function recipientsEditor(state: State, draw: () => void): HTMLElement {
  const body = h("div");

  if (state.storedKeys.length > 0) {
    const list = h("div", { style: "margin-bottom:10px" });
    for (const key of state.storedKeys) {
      const chosen = state.recipients.includes(key.publicKey);
      list.appendChild(
        checkbox(
          chosen,
          key.name,
          key.openable
            ? "This machine holds the private half, so a snapshot sealed to it can be opened here without being asked for a key."
            : "Only the public half is here. A snapshot sealed to this key cannot be opened on this machine.",
          (on) => {
            const at = state.recipients.indexOf(key.publicKey);
            if (on && at < 0) state.recipients.push(key.publicKey);
            if (!on && at >= 0) state.recipients.splice(at, 1);
            draw();
          },
        ),
      );
    }
    body.appendChild(list);
  }

  // Anything sealed to a key PortCloak does not hold, shown as a chip so it is
  // never silently part of the decision.
  const known = new Set(state.storedKeys.map((k) => k.publicKey));
  const pasted = state.recipients.filter((r) => !known.has(r));
  if (pasted.length > 0) {
    const chips = h("div", { class: "row", style: "flex-wrap:wrap;gap:6px;margin-bottom:8px" });
    for (const r of pasted) {
      chips.appendChild(
        h(
          "span",
          { class: "chip" },
          `${r.slice(0, 10)}…${r.slice(-4)}`,
          h(
            "button",
            {
              onClick: () => {
                state.recipients.splice(state.recipients.indexOf(r), 1);
                draw();
              },
            },
            "×",
          ),
        ),
      );
    }
    body.appendChild(chips);
  }

  let pending = "";
  body.appendChild(
    h(
      "div",
      { class: "row" },
      h("input", {
        type: "text",
        placeholder: "…or paste an age public key (age1…)",
        onInput: (e: Event) => {
          pending = (e.target as HTMLInputElement).value.trim();
        },
      }),
      h(
        "button",
        {
          onClick: () => {
            if (pending && !state.recipients.includes(pending)) {
              state.recipients.push(pending);
              draw();
            }
          },
        },
        "Add",
      ),
    ),
  );

  if (state.storedKeys.length === 0) {
    body.appendChild(
      h(
        "div",
        { class: "field-hint" },
        "There are no keys on this machine yet. Create one under Keys and it appears here by name — and opens this snapshot again without being asked for.",
      ),
    );
  }
  return body;
}

/**
 * Remembering the passphrase.
 *
 * A passphrase typed at capture and typed again at every restore is the reason
 * encryption gets turned off. Naming it stores it in this machine's keychain,
 * where a restore finds it without asking. Leaving the name empty keeps the old
 * behaviour exactly: PortCloak holds nothing and cannot recover it.
 */
function rememberPassphrase(state: State): HTMLElement {
  return h(
    "div",
    { style: "margin-top:10px" },
    field(
      "Remember this passphrase as (optional)",
      input(state.rememberPassphraseAs, (v) => (state.rememberPassphraseAs = v), {
        placeholder: "nightly-captures",
      }),
      state.rememberPassphraseAs
        ? `Stored in this machine's keychain as the key “${state.rememberPassphraseAs}”, and tried automatically whenever a snapshot needs opening.`
        : "Leave this empty and PortCloak stores nothing: the passphrase will be asked for every time this snapshot is opened, and cannot be recovered if it is lost.",
    ),
  );
}

function confirmDecline(state: State, draw: () => void): void {
  modal({
    title: "Write this snapshot unencrypted?",
    body: h(
      "div",
      null,
      h("p", null, state.defaults?.declineNotice ?? ""),
      h(
        "p",
        { class: "muted small" },
        "This is a supported choice and PortCloak will not nag about it again for this capture. It will label the snapshot in the library, the manifest and the completeness report, and record the decision in the audit log.",
      ),
    ),
    confirmLabel: "Write it unencrypted",
    confirmTone: "danger-solid",
    cancelLabel: "Keep encryption on",
    onConfirm: () => {
      state.acknowledgedUnencrypted = true;
      draw();
    },
  });
  // Cancelling leaves encryption on, which is what the toggle should show.
  window.setTimeout(() => {
    if (!state.acknowledgedUnencrypted && !document.querySelector(".modal-backdrop")) {
      state.encrypt = true;
      draw();
    }
  }, 0);
}

function storageStep(state: State, draw: () => void): HTMLElement {
  const stores = state.defaults?.storages ?? [];
  const chosen = stores.find((s) => s.name === state.storage);

  const body = h(
    "div",
    null,
    h("h2", { style: "font-size:17px;margin-bottom:12px" }, "Storage sink"),
    field(
      "Storage",
      select(
        stores.map((s) => ({
          value: s.name,
          label: `${s.name} — ${s.kind} · ${s.root}${s.default ? " (default)" : ""}`,
        })),
        state.storage,
        (v) => {
          state.storage = v;
          draw();
        },
      ),
      "A capture writes to exactly one storage. The bundle is checksummed before it reaches any of them, so corruption is caught on retrieval whichever you choose.",
    ),
  );

  if (chosen?.encryptionRequired && !state.encrypt) {
    body.appendChild(
      notice(
        "danger",
        `${chosen.name} requires encryption`,
        "A snapshot cannot be written there unencrypted. Turn encryption back on, or choose a different storage.",
      ),
    );
  }
  if (chosen && !chosen.credentialPresent) {
    body.appendChild(
      notice(
        "warn",
        "This storage has no credential on this machine",
        "Configuration is portable between machines; the secrets deliberately are not. Enter it again on the Storage screen.",
      ),
    );
  }
  return body;
}

function reviewStep(state: State): HTMLElement {
  const env = state.defaults?.environments.find((e) => e.name === state.environment);
  const store = state.defaults?.storages.find((s) => s.name === state.storage);

  const rows: [string, string][] = [
    ["Source", env ? `${env.name} · ${kindLabel(env.kind)} · ${env.target}` : "—"],
    ["Realms", state.realms.join(", ") || "—"],
    ["Snapshots produced", String(state.realms.length)],
    ["Users mode", state.usersMode],
    ["Verify secrets", state.verify ? "yes" : "no"],
    ["Detect dependencies", state.detectDependencies ? "yes" : "no"],
    [
      "Encryption",
      state.encrypt ? describeEncryption(state) : "NONE — unmasked secrets in the clear",
    ],
    ["Storage", store ? `${store.name} · ${store.root}` : "—"],
  ];

  const dl = h("dl", { class: "kv" });
  for (const [k, v] of rows) {
    dl.appendChild(h("dt", null, k));
    dl.appendChild(h("dd", null, v));
  }

  return h(
    "div",
    null,
    h("h2", { style: "font-size:17px;margin-bottom:12px" }, "Review & run"),
    h("div", { class: "card" }, h("div", { class: "card-body" }, dl)),
    !state.encrypt
      ? notice("danger", "Unencrypted", state.defaults?.declineNotice ?? "")
      : null,
    notice(
      "info",
      "What this capture will not carry",
      "Sessions are out of scope by design — users re-authenticate after a restore, and token continuity comes from the realm's signing keys travelling with the snapshot. Custom theme files and provider JARs are detected and reported, never migrated.",
    ),
  );
}

/**
 * What the review step says about encryption.
 *
 * Naming the keys matters here more than counting them: "2 recipient(s)" tells
 * an operator nothing about whether they will be able to open this snapshot
 * afterwards, and that is the only question the review step is for.
 */
function describeEncryption(state: State): string {
  if (state.encryptionMode === "passphrase") {
    return state.rememberPassphraseAs
      ? `passphrase, remembered as “${state.rememberPassphraseAs}”`
      : "passphrase, not stored anywhere";
  }
  const named = state.storedKeys
    .filter((k) => state.recipients.includes(k.publicKey))
    .map((k) => k.name);
  const pasted = state.recipients.length - named.length;
  const parts = [...named];
  if (pasted > 0) parts.push(`${pasted} pasted key(s)`);
  return parts.length > 0 ? `sealed to ${parts.join(", ")}` : "no recipients chosen";
}

async function start(state: State, draw: () => void): Promise<void> {
  state.starting = true;
  state.error = undefined;
  draw();

  const opts: CaptureOptions = {
    environment: state.environment,
    realms: state.realms,
    storage: state.storage,
    usersMode: state.usersMode,
    usersPerFile: state.usersPerFile,
    verify: state.verify,
    detectDependencies: state.detectDependencies,
    encrypt: state.encrypt,
    encryptionMode: state.encryptionMode,
    passphrase: state.passphrase,
    recipients: state.recipients,
    acknowledgedUnencrypted: state.acknowledgedUnencrypted,
  };

  // The passphrase is remembered before the capture starts, not after: a
  // capture that fails at upload was still sealed with this passphrase, and a
  // snapshot half-written to storage is exactly the one nobody wants to find
  // they cannot open.
  if (state.encrypt && state.encryptionMode === "passphrase" && state.rememberPassphraseAs) {
    const failure = await KeysAPI.savePassphrase(
      state.rememberPassphraseAs,
      state.passphrase,
      `Remembered while capturing ${state.realms.join(", ")}.`,
    );
    if (failure) {
      state.starting = false;
      state.error = `The passphrase could not be remembered: ${failure.message} Nothing was captured.`;
      draw();
      return;
    }
  }

  const res = await CaptureAPI.start(opts);
  state.starting = false;
  if (res.failure) {
    state.error = res.failure.message + (res.failure.hint ? ` ${res.failure.hint}` : "");
    draw();
    return;
  }
  navigate({ name: "activity" });
}

export { badge };
