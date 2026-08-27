/**
 * A very small DOM helper set.
 *
 * The UI is forms, tables and progress. A framework would buy little here and
 * cost binary size and build complexity, so these thirty lines stand in for one.
 */

type Child = Node | string | number | null | undefined | false;

export function h<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs?: Record<string, unknown> | null,
  ...children: Child[]
): HTMLElementTagNameMap[K] {
  const el = document.createElement(tag);
  if (attrs) {
    for (const [key, value] of Object.entries(attrs)) {
      if (value === null || value === undefined || value === false) continue;
      if (key === "class") {
        el.className = String(value);
      } else if (key === "dataset") {
        Object.assign(el.dataset, value as Record<string, string>);
      } else if (key.startsWith("on") && typeof value === "function") {
        el.addEventListener(key.slice(2).toLowerCase(), value as EventListener);
      } else if (key === "value" && el instanceof HTMLInputElement) {
        el.value = String(value);
      } else if (key === "checked" || key === "disabled" || key === "selected") {
        (el as unknown as Record<string, unknown>)[key] = Boolean(value);
      } else {
        el.setAttribute(key, String(value));
      }
    }
  }
  append(el, children);
  return el;
}

export function append(parent: Node, children: Child[]): void {
  for (const child of children) {
    if (child === null || child === undefined || child === false) continue;
    parent.appendChild(
      typeof child === "string" || typeof child === "number"
        ? document.createTextNode(String(child))
        : child,
    );
  }
}

export function clear(el: Element): void {
  while (el.firstChild) el.removeChild(el.firstChild);
}

export function frag(...children: Child[]): DocumentFragment {
  const f = document.createDocumentFragment();
  append(f, children);
  return f;
}

/* ── Formatting ────────────────────────────────────────────────────────── */

export function bytes(n: number | undefined): string {
  if (!n) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`;
}

export function count(n: number | undefined): string {
  return (n ?? 0).toLocaleString();
}

/** Renders a timestamp the way an operator reads one, not as an ISO string. */
export function when(iso: string | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function time(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/* ── Common pieces ─────────────────────────────────────────────────────── */

export function badge(text: string, tone: "ok" | "warn" | "danger" | "neutral" | "info"): HTMLElement {
  return h("span", { class: `badge ${tone}` }, text);
}

export function notice(
  tone: "ok" | "info" | "warn" | "danger",
  title: string,
  body?: string,
): HTMLElement {
  return h(
    "div",
    { class: `notice ${tone}` },
    h("div", { class: "notice-title" }, title),
    body ? lines(body) : null,
  );
}

/**
 * A message that carries newlines is several statements, not one paragraph.
 *
 * The engine reports a rejected edit as one problem per line, because a
 * configuration can be wrong in more than one place at a time and fixing them
 * one launch at a time is the thing the validator exists to avoid. Rendered
 * into a single text node those newlines collapse into spaces and the problems
 * run together into a sentence that reads like one long complaint.
 */
function lines(text: string): Node {
  const parts = text.split("\n").filter((l) => l.trim() !== "");
  if (parts.length <= 1) return h("div", null, text);
  return frag(...parts.map((l) => h("div", null, l)));
}

export function field(
  label: string,
  control: HTMLElement,
  hint?: string,
): HTMLElement {
  return h("div", { class: "field" }, h("label", null, label), control, hint ? h("div", { class: "field-hint" }, hint) : null);
}

export function input(
  value: string | undefined,
  onInput: (v: string) => void,
  opts: { type?: string; placeholder?: string } = {},
): HTMLInputElement {
  return h("input", {
    type: opts.type ?? "text",
    value: value ?? "",
    placeholder: opts.placeholder ?? "",
    onInput: (e: Event) => onInput((e.target as HTMLInputElement).value),
  });
}

export function select(
  options: { value: string; label: string }[],
  current: string,
  onChange: (v: string) => void,
): HTMLSelectElement {
  const el = h("select", {
    onChange: (e: Event) => onChange((e.target as HTMLSelectElement).value),
  });
  for (const o of options) {
    el.appendChild(h("option", { value: o.value, selected: o.value === current }, o.label));
  }
  return el;
}

export function toggle(on: boolean, onChange: (v: boolean) => void): HTMLElement {
  return h("div", {
    class: `toggle ${on ? "on" : ""}`,
    role: "switch",
    "aria-checked": String(on),
    onClick: () => onChange(!on),
  });
}

export function checkbox(
  checked: boolean,
  label: string,
  hint: string,
  onChange: (v: boolean) => void,
): HTMLElement {
  return h(
    "div",
    { class: "checkbox" },
    h("input", {
      type: "checkbox",
      checked,
      onChange: (e: Event) => onChange((e.target as HTMLInputElement).checked),
    }),
    h(
      "div",
      { class: "checkbox-body" },
      h("div", { class: "checkbox-label" }, label),
      hint ? h("div", { class: "checkbox-hint" }, hint) : null,
    ),
  );
}

export function spinner(text?: string): HTMLElement {
  return h("div", { class: "row muted small" }, h("span", { class: "spinner" }), text ?? "Working…");
}

/** Renders an engine failure with its hint, which is the actionable half. */
export function failureNotice(f: { message: string; hint?: string; retryable?: boolean }): HTMLElement {
  return h(
    "div",
    { class: `notice ${f.retryable ? "warn" : "danger"}` },
    h("div", { class: "notice-title" }, f.message),
    f.hint ? lines(f.hint) : null,
  );
}

/* ── Modal ─────────────────────────────────────────────────────────────── */

export interface ModalOptions {
  title: string;
  body: Node;
  confirmLabel?: string;
  confirmTone?: "primary" | "danger-solid";
  cancelLabel?: string;
  onConfirm?: () => void | Promise<void>;
  /**
   * Runs when the modal is dismissed without confirming — the cancel button,
   * the backdrop, or being replaced by another modal.
   *
   * A confirmation that asks "are you sure you want to turn this off" needs
   * this: declining leaves the setting mid-way otherwise, off in the state but
   * unconfirmed, which is neither of the two answers the question offered.
   */
  onCancel?: () => void;
  confirmDisabled?: boolean;
}

let openModal: HTMLElement | null = null;

export function modal(opts: ModalOptions): void {
  closeModal();

  // Dismissal is per-element rather than "whatever is open now", because
  // confirming is allowed to open the next modal — showing a key that was just
  // generated, reporting a failure — and a global close would take that one
  // down again the moment it appeared.
  let cancelled = opts.onCancel;
  const dismiss = (): void => {
    const run = cancelled;
    cancelled = undefined;
    backdrop.remove();
    if (openModal === backdrop) openModal = null;
    run?.();
  };

  const confirm = opts.onConfirm
    ? h(
        "button",
        {
          class: opts.confirmTone ?? "primary",
          disabled: opts.confirmDisabled,
          onClick: async () => {
            // Confirming is not dismissing, whatever the handler goes on to do.
            cancelled = undefined;
            await opts.onConfirm?.();
            dismiss();
          },
        },
        opts.confirmLabel ?? "Confirm",
      )
    : null;

  const backdrop = h(
    "div",
    {
      class: "modal-backdrop",
      onClick: (e: MouseEvent) => {
        if (e.target === backdrop) dismiss();
      },
    },
    h(
      "div",
      { class: "modal" },
      h("div", { class: "modal-head" }, opts.title),
      h("div", { class: "modal-body" }, opts.body),
      h(
        "div",
        { class: "modal-foot" },
        h("button", { class: "plain", onClick: () => dismiss() }, opts.cancelLabel ?? "Cancel"),
        confirm,
      ),
    ),
  );
  document.body.appendChild(backdrop);
  openModal = backdrop;
  modalDismiss = dismiss;
}

/** Dismisses whatever modal is currently open, running its cancel handler. */
let modalDismiss: (() => void) | null = null;

export function closeModal(): void {
  const dismiss = modalDismiss;
  modalDismiss = null;
  dismiss?.();
}

/** Sets a modal's confirm button state after the body has changed. */
export function setModalConfirmDisabled(disabled: boolean): void {
  const btn = openModal?.querySelector<HTMLButtonElement>(".modal-foot button:last-child");
  if (btn) btn.disabled = disabled;
}
