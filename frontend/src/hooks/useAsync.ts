/**
 * Loading something from the engine, with the three states that implies.
 *
 * Every screen in the app does the same thing: put a spinner up, ask the
 * engine, replace the spinner with the answer. This is that, once, so no screen
 * has to remember to handle the case where the answer arrives after the
 * operator has already navigated away.
 *
 * A load that fails is rethrown during render on purpose — see ViewBoundary.
 */
import { useCallback, useEffect, useRef, useState, type DependencyList } from "react";

export type Async<T> =
  | { status: "loading" }
  | { status: "ready"; value: T }
  | { status: "failed"; error: unknown };

export interface AsyncResult<T> {
  state: Async<T>;
  /** Asks again, keeping whatever is on screen until the answer arrives. */
  reload: () => Promise<void>;
  /**
   * Replaces the loaded value without a round trip.
   *
   * For the screens that already know the new state — a probe result, a saved
   * draft — and would only be re-reading what they just wrote.
   */
  set: (value: T) => void;
}

export function useAsync<T>(load: () => Promise<T>, deps: DependencyList): AsyncResult<T> {
  const [state, setState] = useState<Async<T>>({ status: "loading" });

  // The load closure changes on every render; the effect must not. Holding it
  // in a ref is what lets a page write `useAsync(() => API.list(), [])` without
  // the list being re-read on every keystroke elsewhere on the screen.
  const current = useRef(load);
  current.current = load;

  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const run = useCallback(async () => {
    try {
      const value = await current.current();
      if (alive.current) setState({ status: "ready", value });
    } catch (error) {
      if (alive.current) setState({ status: "failed", error });
    }
  }, []);

  useEffect(() => {
    setState({ status: "loading" });
    void run();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  const set = useCallback((value: T) => setState({ status: "ready", value }), []);

  return { state, reload: run, set };
}
