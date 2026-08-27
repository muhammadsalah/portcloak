/**
 * How PortCloak asks for a snapshot's decryption key.
 *
 * Two screens need it — reading inside a snapshot, and restoring one — and
 * they ask in different containers: the inspector in a modal on its way in, the
 * restore wizard as a field on the step that says decryption happens first. The
 * container differs; the question must not. A snapshot is opened by a
 * passphrase or by an age identity, and an operator who learns the shape of
 * that question on one screen should recognise it on the other.
 */
import { h } from "../dom";

/** A key as the engine takes it: one passphrase, or one or more identities. */
export interface SnapshotKey {
  passphrase: string;
  identities: string[];
}

export function noKey(): SnapshotKey {
  return { passphrase: "", identities: [] };
}

export function hasKey(key: SnapshotKey): boolean {
  return key.passphrase !== "" || key.identities.length > 0;
}

/**
 * The passphrase and identity inputs, writing straight into `key`.
 *
 * `onSettled` runs when an input loses focus rather than on every keystroke:
 * the callers redraw, and redrawing per character would take the cursor out of
 * the field being typed into.
 */
export function keyFields(key: SnapshotKey, onSettled?: () => void): HTMLElement {
  const passphrase = h("input", {
    type: "password",
    placeholder: "The passphrase this snapshot was sealed with",
    onInput: (e: Event) => {
      key.passphrase = (e.target as HTMLInputElement).value;
    },
    onChange: () => onSettled?.(),
  });
  passphrase.value = key.passphrase;

  const identity = h("textarea", {
    rows: "3",
    placeholder: "AGE-SECRET-KEY-1…",
    onInput: (e: Event) => {
      const v = (e.target as HTMLTextAreaElement).value.trim();
      key.identities = v ? [v] : [];
    },
    onChange: () => onSettled?.(),
  });
  // Assigned rather than passed as an attribute: a textarea takes its value
  // from its content, so a redraw would otherwise blank what was typed.
  identity.value = key.identities[0] ?? "";

  return h(
    "div",
    null,
    h("label", null, "Passphrase"),
    passphrase,
    h("label", { style: "margin-top:12px" }, "…or an age private key"),
    identity,
  );
}
