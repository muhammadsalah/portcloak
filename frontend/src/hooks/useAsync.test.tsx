// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Every screen in the app loads through this hook, so its bugs are not one
 * screen's bugs. The two that matter are both about time: an answer arriving
 * after the operator has navigated away must not be written into a component
 * that is gone, and a `load` closure rebuilt on every render must not cause the
 * engine to be asked again on every keystroke elsewhere on the screen.
 */
import { describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";

import { useAsync } from "./useAsync";

/** A promise this test resolves by hand, so the pending state can be observed. */
function deferred<T>() {
  let settle!: (value: T) => void;
  let fail!: (reason: unknown) => void;
  const promise = new Promise<T>((resolve, reject) => {
    settle = resolve;
    fail = reject;
  });
  return { promise, settle, fail };
}

describe("the three states", () => {
  it("starts loading", () => {
    const { result } = renderHook(() => useAsync(() => deferred<string>().promise, []));
    expect(result.current.state).toEqual({ status: "loading" });
  });

  it("becomes ready with what the engine answered", async () => {
    const { result } = renderHook(() => useAsync(() => Promise.resolve(["acme"]), []));
    await waitFor(() => expect(result.current.state.status).toBe("ready"));
    expect(result.current.state).toEqual({ status: "ready", value: ["acme"] });
  });

  it("becomes failed, carrying the error rather than a message about it", async () => {
    // ViewBoundary rethrows this during render and prints the component stack,
    // which only works if the error itself survives.
    const boom = new Error("the engine is not listening");
    const { result } = renderHook(() => useAsync(() => Promise.reject(boom), []));

    await waitFor(() => expect(result.current.state.status).toBe("failed"));
    expect(result.current.state).toEqual({ status: "failed", error: boom });
  });
});

describe("reload", () => {
  it("keeps what is on screen until the new answer arrives", async () => {
    const pending = deferred<string>();
    let answer = () => Promise.resolve("first");

    const { result } = renderHook(() => useAsync(() => answer(), []));
    await waitFor(() => expect(result.current.state).toEqual({ status: "ready", value: "first" }));

    answer = () => pending.promise;
    let reloading!: Promise<void>;
    act(() => {
      reloading = result.current.reload();
    });

    // Still showing the truth as of a moment ago, rather than a spinner.
    expect(result.current.state).toEqual({ status: "ready", value: "first" });

    await act(async () => {
      pending.settle("second");
      await reloading;
    });
    expect(result.current.state).toEqual({ status: "ready", value: "second" });
  });

  it("reports a failure on a reload the same way as on a first load", async () => {
    let answer = () => Promise.resolve("first");
    const { result } = renderHook(() => useAsync(() => answer(), []));
    await waitFor(() => expect(result.current.state.status).toBe("ready"));

    answer = () => Promise.reject(new Error("gone"));
    await act(async () => {
      await result.current.reload();
    });
    expect(result.current.state.status).toBe("failed");
  });
});

describe("set", () => {
  it("replaces the value without a round trip", async () => {
    // For the screens that already know the new state — a probe result, a saved
    // draft — and would only be re-reading what they just wrote.
    const load = vi.fn(() => Promise.resolve("first"));
    const { result } = renderHook(() => useAsync(load, []));
    await waitFor(() => expect(result.current.state.status).toBe("ready"));

    act(() => result.current.set("written locally"));

    expect(result.current.state).toEqual({ status: "ready", value: "written locally" });
    expect(load).toHaveBeenCalledTimes(1);
  });
});

describe("what re-runs a load, and what does not", () => {
  it("does not ask again when only the closure changed", async () => {
    // Every page writes `useAsync(() => API.list(), [])`, which is a fresh
    // function on every render. Re-reading on each of them would put the engine
    // under a call per keystroke elsewhere on the screen.
    const load = vi.fn(() => Promise.resolve("value"));
    const { result, rerender } = renderHook(() => useAsync(() => load(), []));
    await waitFor(() => expect(result.current.state.status).toBe("ready"));

    rerender();
    rerender();

    expect(load).toHaveBeenCalledTimes(1);
  });

  it("asks again, and shows a spinner, when a dependency changes", async () => {
    const load = vi.fn((id: string) => Promise.resolve(`page ${id}`));
    const { result, rerender } = renderHook(({ id }) => useAsync(() => load(id), [id]), {
      initialProps: { id: "1" },
    });
    await waitFor(() => expect(result.current.state).toEqual({ status: "ready", value: "page 1" }));

    rerender({ id: "2" });

    await waitFor(() => expect(result.current.state).toEqual({ status: "ready", value: "page 2" }));
    expect(load).toHaveBeenCalledTimes(2);
  });
});

describe("an answer that arrives too late", () => {
  it("is dropped rather than written into an unmounted screen", async () => {
    // The operator navigated away while the engine was still thinking. React
    // logs a warning for a state update after unmount, and the `alive` ref is
    // what stops one.
    const pending = deferred<string>();
    const console_ = vi.spyOn(console, "error").mockImplementation(() => {});

    const { unmount } = renderHook(() => useAsync(() => pending.promise, []));
    unmount();

    await act(async () => {
      pending.settle("too late");
      await pending.promise;
    });

    expect(console_).not.toHaveBeenCalled();
    console_.mockRestore();
  });

  it("drops a late failure too", async () => {
    const pending = deferred<string>();
    const console_ = vi.spyOn(console, "error").mockImplementation(() => {});

    const { unmount } = renderHook(() => useAsync(() => pending.promise, []));
    unmount();

    await act(async () => {
      pending.fail(new Error("too late"));
      await pending.promise.catch(() => undefined);
    });

    expect(console_).not.toHaveBeenCalled();
    console_.mockRestore();
  });
});
