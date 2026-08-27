// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The modal, and the one place in the app that owns it.
 *
 * A modal is opened from a click handler, often deep inside a page, and one
 * modal is routinely opened from another's confirm handler — showing a key that
 * was just generated, reporting a failure. Passing that down as props would
 * mean every page threading a `setModal` through every panel it renders, so it
 * is a context instead: `useModal().open({…})` from anywhere.
 *
 * Only one is ever on screen. Opening a second dismisses the first, exactly as
 * the imperative version did, and for the same reason: two stacked scrims is a
 * state nobody designed and nobody can get out of.
 */
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import styled from "styled-components";

import { Button, type ButtonVariant } from "./Button";

export interface ModalOptions {
  title: ReactNode;
  body: ReactNode;
  confirmLabel?: string;
  confirmTone?: Extract<ButtonVariant, "primary" | "danger-solid">;
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

interface ModalEntry {
  id: number;
  options: ModalOptions;
  /** Confirming is not dismissing, whatever the handler goes on to do. */
  confirmed: boolean;
}

interface ModalApi {
  open: (options: ModalOptions) => void;
  close: () => void;
}

/** What a modal's own body can do to the frame around it. */
interface ModalControls {
  /**
   * Arms or disarms the confirm button after the body has changed.
   *
   * A confirmation that asks for a name to be typed starts disabled and becomes
   * available as the field matches. The button lives in the footer, which the
   * body does not render, so this is how it reaches it.
   */
  setConfirmDisabled: (disabled: boolean) => void;
  /**
   * Replaces what confirming does.
   *
   * A modal whose body is a form — creating a key, importing one — cannot hand
   * its handler in at open time, because the values the handler needs do not
   * exist until they have been typed. The body registers a fresh handler as its
   * own state changes instead, and the footer button calls whatever is current.
   */
  setConfirm: (handler: () => void | Promise<void>) => void;
  /** Dismisses this modal from inside its own body. */
  close: () => void;
}

const ModalContext = createContext<ModalApi | null>(null);
const ModalControlsContext = createContext<ModalControls | null>(null);

export function useModal(): ModalApi {
  const api = useContext(ModalContext);
  if (!api) throw new Error("useModal was called outside ModalProvider");
  return api;
}

export function useModalControls(): ModalControls {
  const controls = useContext(ModalControlsContext);
  if (!controls) throw new Error("useModalControls was called outside a modal body");
  return controls;
}

export function ModalProvider({ children }: { children: ReactNode }) {
  const [entry, setEntry] = useState<ModalEntry | null>(null);
  const [confirmDisabled, setConfirmDisabled] = useState(false);
  // Set by a form body through `setConfirm`; it takes precedence over whatever
  // the modal was opened with, and is dropped when the modal is. Held as state
  // rather than a ref because it also decides whether the button is there at
  // all — a body can add a confirm to a modal that opened without one.
  const [bodyConfirm, setBodyConfirm] = useState<{ run: () => void | Promise<void> } | null>(null);
  // The state above is what renders; this is what the handlers read, because a
  // confirm handler that opens the next modal runs before React has committed
  // anything and would otherwise be looking at the modal it just replaced.
  const entryRef = useRef<ModalEntry | null>(null);
  const nextId = useRef(0);

  const dismiss = useCallback(() => {
    const closing = entryRef.current;
    if (!closing) return;
    entryRef.current = null;
    setEntry(null);
    setBodyConfirm(null);
    if (!closing.confirmed) closing.options.onCancel?.();
  }, []);

  const open = useCallback(
    (options: ModalOptions) => {
      dismiss();
      const opened: ModalEntry = { id: nextId.current++, options, confirmed: false };
      entryRef.current = opened;
      setEntry(opened);
      setConfirmDisabled(Boolean(options.confirmDisabled));
    },
    [dismiss],
  );

  const api = useMemo<ModalApi>(() => ({ open, close: dismiss }), [open, dismiss]);

  const controls = useMemo<ModalControls>(
    () => ({
      setConfirmDisabled,
      setConfirm: (handler) => setBodyConfirm({ run: handler }),
      close: dismiss,
    }),
    [dismiss],
  );

  const confirm = async () => {
    const confirming = entryRef.current;
    if (!confirming) return;
    confirming.confirmed = true;
    await (bodyConfirm ? bodyConfirm.run() : confirming.options.onConfirm?.());
    // Only if it is still ours. Confirming is allowed to open the next modal,
    // and dismissing then would take that one down the moment it appeared.
    if (entryRef.current === confirming) dismiss();
  };

  return (
    <ModalContext.Provider value={api}>
      {children}
      {entry
        ? createPortal(
            <Backdrop
              onClick={(e) => {
                if (e.target === e.currentTarget) dismiss();
              }}
            >
              <Panel>
                <Head>{entry.options.title}</Head>
                <Body>
                  <ModalControlsContext.Provider value={controls}>
                    {entry.options.body}
                  </ModalControlsContext.Provider>
                </Body>
                <Foot>
                  <Button $variant="plain" onClick={dismiss}>
                    {entry.options.cancelLabel ?? "Cancel"}
                  </Button>
                  {entry.options.onConfirm || bodyConfirm ? (
                    <Button
                      $variant={entry.options.confirmTone ?? "primary"}
                      disabled={confirmDisabled}
                      onClick={() => void confirm()}
                    >
                      {entry.options.confirmLabel ?? "Confirm"}
                    </Button>
                  ) : null}
                </Foot>
              </Panel>
            </Backdrop>,
            document.body,
          )
        : null}
    </ModalContext.Provider>
  );
}

const Backdrop = styled.div`
  position: fixed;
  inset: 0;
  background: ${(p) => p.theme.color.scrim};
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: ${(p) => p.theme.z.modal};
  padding: 24px;
`;

const Panel = styled.div`
  background: ${(p) => p.theme.color.surface};
  border-radius: ${(p) => p.theme.radius.base};
  width: 100%;
  max-width: 560px;
  max-height: 90vh;
  overflow-y: auto;
  box-shadow: 0 8px 32px rgba(3, 3, 3, 0.3);
`;

const Head = styled.div`
  padding: 16px 20px;
  border-bottom: 1px solid ${(p) => p.theme.color.border};
  font-family: ${(p) => p.theme.font.heading};
  font-size: 17px;
`;

const Body = styled.div`
  padding: 20px;
`;

const Foot = styled.div`
  padding: 12px 20px;
  border-top: 1px solid ${(p) => p.theme.color.border};
  background: ${(p) => p.theme.color.surfaceSubtle};
  display: flex;
  justify-content: flex-end;
  gap: 8px;
`;
