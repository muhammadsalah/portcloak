import { KeysAPI, type KeyAvailability, type KeysView, type StoredKey } from "../api";
import {
  badge,
  clear,
  closeModal,
  field,
  h,
  input,
  modal,
  notice,
  setModalConfirmDisabled,
  spinner,
} from "../dom";

/**
 * The Keys screen.
 *
 * Encryption used to be something an operator supplied from outside PortCloak
 * every single time: a passphrase typed at capture and typed again at every
 * restore, or an age keypair generated elsewhere and pasted in. PortCloak could
 * generate a keypair, but showed it once and stored nothing — so the operator
 * was still the key management system. That works exactly once, and then it
 * becomes the reason encryption gets turned off.
 *
 * A key here has a name, a kind and a home. The secret half goes to this
 * machine's OS keychain like every other secret PortCloak holds; config.yaml
 * carries the name, the kind, the public half where there is one, and a handle.
 * Restore and Inspect then try these keys without asking, and say which one
 * worked.
 */
export async function renderKeys(root: HTMLElement): Promise<void> {
  clear(root);
  root.appendChild(spinner("Reading keys…"));

  const draw = async (): Promise<void> => {
    let view: KeysView;
    let availability: KeyAvailability | null;
    try {
      [view, availability] = await Promise.all([
        KeysAPI.list(),
        KeysAPI.availability().catch(() => null),
      ]);
    } catch (err) {
      clear(root);
      root.appendChild(notice("danger", "The keys could not be read.", String(err)));
      return;
    }
    clear(root);
    root.appendChild(page(view, availability, () => void draw()));
  };
  await draw();
}

function page(view: KeysView, availability: KeyAvailability | null, reload: () => void): HTMLElement {
  const head = h(
    "div",
    { class: "page-head" },
    h(
      "div",
      null,
      h("h1", { class: "page-title" }, "Encryption keys"),
      h("div", { class: "page-subtitle" }, view.note),
    ),
    h(
      "div",
      { class: "row" },
      h("button", { onClick: () => importDialog(reload) }, "Import a key"),
      h("button", { class: "primary", onClick: () => generateDialog(reload) }, "Create a key"),
    ),
  );

  const container = h("div", null, head);
  if (view.failure) {
    container.appendChild(notice("danger", "Not read", view.failure.message));
  }

  if (availability && availability.fromSession > 0) {
    container.appendChild(sessionKeysCard(availability, reload));
  }

  if (view.keys.length === 0) {
    container.appendChild(emptyState(reload));
    return container;
  }

  for (const key of view.keys) {
    container.appendChild(card(key, view.deleteWarning, reload));
  }

  container.appendChild(
    h(
      "div",
      { class: "muted small", style: "margin-top:16px" },
      "A key lives in this machine's keychain. config.yaml holds only its name, its kind, its public half where it has one, and a handle — so the file stays portable between machines and the secrets deliberately do not.",
    ),
  );
  return container;
}

/**
 * Keys typed on a screen during this run.
 *
 * They are held in memory so that unlocking a snapshot in the library and then
 * restoring it does not ask for the same key twice. They are not stored, and
 * saying so here is the point of the card: quitting forgets them, and the way
 * to keep one is to create it above.
 */
function sessionKeysCard(availability: KeyAvailability, reload: () => void): HTMLElement {
  return h(
    "div",
    { class: "card" },
    h(
      "div",
      { class: "card-head" },
      h(
        "div",
        { class: "row" },
        h("span", { class: "card-title" }, "Used in this session"),
        badge(String(availability.fromSession), "info"),
      ),
      h(
        "button",
        {
          onClick: async () => {
            await KeysAPI.forgetSessionKeys();
            reload();
          },
        },
        "Forget them",
      ),
    ),
    h(
      "div",
      { class: "card-body muted small" },
      `${availability.fromSession} key(s) you entered while opening or restoring a snapshot are held in memory for the rest of this run, so you are not asked for them again. They are not stored anywhere: quitting PortCloak forgets them.`,
    ),
  );
}

function emptyState(reload: () => void): HTMLElement {
  return h(
    "div",
    { class: "card" },
    h(
      "div",
      { class: "card-body" },
      h("div", { class: "card-title" }, "No keys yet"),
      h(
        "p",
        null,
        "A key is how a snapshot gets sealed and opened again. Create one and PortCloak keeps it in this machine's keychain: captures can seal to it by name, and a restore opens the snapshot without asking you to remember anything.",
      ),
      h(
        "p",
        { class: "muted small" },
        "An age keypair is the one to start with. Its public half is what a capture seals to, which means the machine that takes a snapshot does not have to be able to open it — capture and restore stop being the same privilege.",
      ),
      h(
        "div",
        { class: "row" },
        h("button", { class: "primary", onClick: () => generateDialog(reload) }, "Create a key"),
        h("button", { onClick: () => importDialog(reload) }, "Import one I already have"),
      ),
    ),
  );
}

function card(key: StoredKey, deleteWarning: string, reload: () => void): HTMLElement {
  const kindLabel = key.kind === "identity" ? "Age keypair" : "Passphrase";

  return h(
    "div",
    { class: "card", style: key.present ? "" : "border-color:var(--warning)" },
    h(
      "div",
      { class: "card-head" },
      h(
        "div",
        { class: "row" },
        h("span", { class: "card-title" }, key.name),
        badge(kindLabel, "info"),
        key.present ? badge("In this keychain", "ok") : badge("Not on this machine", "warn"),
      ),
      h(
        "div",
        { class: "row" },
        key.age ? h("span", { class: "muted small" }, `Created ${key.age}`) : null,
        key.present
          ? h("button", { onClick: () => revealDialog(key) }, "Show secret")
          : null,
        h(
          "button",
          { class: "danger", onClick: () => deleteDialog(key, deleteWarning, reload) },
          "Delete",
        ),
      ),
    ),
    h(
      "div",
      { class: "card-body" },
      key.note ? h("p", { style: "margin-top:0" }, key.note) : null,
      h("div", { class: "muted small" }, key.summary),
      key.publicKey
        ? h(
            "div",
            { style: "margin-top:10px" },
            h("label", null, "Public key — captures seal to this"),
            h("div", { class: "reveal-value" }, key.publicKey),
          )
        : null,
    ),
  );
}

/** The sentence a newly stored key is handed back with. */
function backupNotice(warning: string): HTMLElement {
  return notice("warn", "Keep a copy somewhere this machine is not", warning);
}

function generateDialog(reload: () => void): void {
  const draft = { name: "", note: "", passphrase: "", kind: "identity" as "identity" | "passphrase" };

  const body = h("div");
  const render = (): void => {
    clear(body);
    body.appendChild(
      h(
        "div",
        { class: "tabs" },
        h(
          "div",
          {
            class: `tab ${draft.kind === "identity" ? "active" : ""}`,
            onClick: () => {
              draft.kind = "identity";
              render();
            },
          },
          "Age keypair",
        ),
        h(
          "div",
          {
            class: `tab ${draft.kind === "passphrase" ? "active" : ""}`,
            onClick: () => {
              draft.kind = "passphrase";
              render();
            },
          },
          "Passphrase",
        ),
      ),
    );
    body.appendChild(
      field(
        "Name",
        input(draft.name, (v) => (draft.name = v), { placeholder: "ops-team" }),
        "How every other screen refers to this key.",
      ),
    );
    if (draft.kind === "passphrase") {
      body.appendChild(
        field(
          "Passphrase",
          input(draft.passphrase, (v) => (draft.passphrase = v), { type: "password" }),
          "PortCloak remembers this in the keychain and tries it when a snapshot needs opening.",
        ),
      );
    } else {
      body.appendChild(
        h(
          "p",
          { class: "muted small" },
          "PortCloak generates the keypair. The private half goes to this machine's keychain and is shown once so you can keep a copy; the public half is what a capture seals to.",
        ),
      );
    }
    body.appendChild(
      field("Note (optional)", input(draft.note, (v) => (draft.note = v)), ""),
    );
  };
  render();

  modal({
    title: "Create a key",
    body,
    confirmLabel: "Create",
    onConfirm: async () => {
      if (draft.kind === "passphrase") {
        const failure = await KeysAPI.savePassphrase(draft.name, draft.passphrase, draft.note);
        if (failure) {
          modal({ title: "Not created", body: h("div", null, failure.message) });
          return;
        }
        reload();
        return;
      }
      const gen = await KeysAPI.generate(draft.name, draft.note);
      if (gen.failure) {
        modal({ title: "Not created", body: h("div", null, gen.failure.message) });
        return;
      }
      reload();
      // Shown once, and only as a copy to keep elsewhere: PortCloak already
      // holds it, so this is not the operator's only chance to save it — it is
      // their only chance to save it somewhere the machine is not.
      modal({
        title: `The key “${gen.name}”`,
        body: h(
          "div",
          null,
          backupNotice(gen.warning),
          h("label", { style: "margin-top:12px" }, "Private key"),
          h("div", { class: "reveal-value" }, gen.privateKey),
          h("label", { style: "margin-top:12px" }, "Public key (the recipient)"),
          h("div", { class: "reveal-value" }, gen.publicKey),
        ),
        cancelLabel: "Done",
      });
    },
  });
}

function importDialog(reload: () => void): void {
  const draft = { name: "", note: "", secret: "" };
  const secret = h("textarea", {
    rows: "3",
    placeholder: "AGE-SECRET-KEY-1…",
    onInput: (e: Event) => {
      draft.secret = (e.target as HTMLTextAreaElement).value.trim();
    },
  });

  modal({
    title: "Import a key",
    body: h(
      "div",
      null,
      field(
        "Name",
        input(draft.name, (v) => (draft.name = v), { placeholder: "ops-team" }),
        "How every other screen refers to this key.",
      ),
      h("label", null, "Age private key"),
      secret,
      h(
        "div",
        { class: "field-hint" },
        "Only the private half is needed. PortCloak derives the public half from it, so there is no way to store a pair whose halves do not match.",
      ),
      field("Note (optional)", input(draft.note, (v) => (draft.note = v)), ""),
    ),
    confirmLabel: "Import",
    onConfirm: async () => {
      const failure = await KeysAPI.importIdentity(draft.name, draft.secret, draft.note);
      if (failure) {
        modal({ title: "Not imported", body: h("div", null, failure.message) });
        return;
      }
      reload();
    },
  });
}

function revealDialog(key: StoredKey): void {
  modal({
    title: `Show the secret half of “${key.name}”?`,
    body: h(
      "div",
      null,
      h(
        "p",
        null,
        key.kind === "identity"
          ? "This is the private key. Anyone holding it can open every snapshot sealed to this key."
          : "This is the passphrase. Anyone holding it can open every snapshot sealed with it.",
      ),
      h("p", { class: "muted small" }, "PortCloak records that it was shown, and never what was shown."),
    ),
    confirmLabel: "Show it",
    confirmTone: "danger-solid",
    onConfirm: async () => {
      const res = await KeysAPI.reveal(key.name);
      if (res.failure) {
        modal({ title: "Not shown", body: h("div", null, res.failure.message) });
        return;
      }
      modal({
        title: `The key “${res.name}”`,
        body: h(
          "div",
          null,
          backupNotice(res.warning),
          h("label", { style: "margin-top:12px" }, key.kind === "identity" ? "Private key" : "Passphrase"),
          h("div", { class: "reveal-value" }, res.secret),
        ),
        cancelLabel: "Done",
      });
    },
  });
}

/**
 * Deleting a key is the one action on this screen PortCloak cannot soften.
 *
 * A key is not "in use" by anything the tool can see: it is in use by every
 * snapshot ever sealed with it, and those live in storage backends that may not
 * even be configured here. So the confirmation asks for the name to be typed,
 * the way an overwrite does — the consequence is comparable.
 */
function deleteDialog(key: StoredKey, warning: string, reload: () => void): void {
  let typed = "";
  const body = h(
    "div",
    null,
    notice("danger", "This cannot be undone", warning),
    h("label", { style: "margin-top:12px" }, `Type ${key.name} to confirm`),
    input("", (v) => {
      typed = v;
      setModalConfirmDisabled(typed !== key.name);
    }),
  );

  modal({
    title: `Delete the key “${key.name}”?`,
    body,
    confirmLabel: "Delete this key",
    confirmTone: "danger-solid",
    confirmDisabled: true,
    onConfirm: async () => {
      if (typed !== key.name) return;
      const failure = await KeysAPI.remove(key.name);
      if (failure) {
        modal({ title: "Not deleted", body: h("div", null, failure.message) });
        return;
      }
      closeModal();
      reload();
    },
  });
}
