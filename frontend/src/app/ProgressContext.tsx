/**
 * The one subscription to the engine's progress stream.
 *
 * Progress events all arrive on one channel. Screens subscribe through here
 * rather than each opening a listener of its own, so navigating away cannot
 * leave one behind — which is exactly the bug the imperative version kept
 * having to defend against with mutation observers.
 */
import { createContext, useContext, useEffect, useMemo, useRef, type ReactNode } from "react";

import { onProgress, type ProgressEvent } from "../api";

type Listener = (event: ProgressEvent) => void;

const ProgressContext = createContext<((listener: Listener) => () => void) | null>(null);

export function ProgressProvider({ children }: { children: ReactNode }) {
  const listeners = useRef(new Set<Listener>());

  useEffect(() => onProgress((event) => {
    for (const listener of listeners.current) listener(event);
  }), []);

  const subscribe = useMemo(
    () => (listener: Listener) => {
      listeners.current.add(listener);
      return () => {
        listeners.current.delete(listener);
      };
    },
    [],
  );

  return <ProgressContext.Provider value={subscribe}>{children}</ProgressContext.Provider>;
}

/**
 * Runs `handler` for every progress event, for as long as the calling component
 * is mounted.
 *
 * The handler is held in a ref rather than being a dependency, so a screen can
 * pass a fresh closure on every render — which every screen does — without
 * resubscribing and losing events in the gap.
 */
export function useProgress(handler: Listener): void {
  const subscribe = useContext(ProgressContext);
  if (!subscribe) throw new Error("useProgress was called outside ProgressProvider");

  const current = useRef(handler);
  current.current = handler;

  useEffect(() => subscribe((event) => current.current(event)), [subscribe]);
}
