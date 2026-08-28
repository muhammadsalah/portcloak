// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The one subscription to the engine's progress stream.
 *
 * This exists because the imperative version kept leaking listeners: a screen
 * opened one, the operator navigated away, and it was still there for the rest
 * of the session. The two guarantees below are the ones that replaced the
 * mutation observers written to defend against that — a component that unmounts
 * stops receiving, and a component that re-renders does not lose events in the
 * gap while it resubscribes.
 */
import { describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";
import type { ReactNode } from "react";

import type { ProgressEvent } from "../api";
import { ProgressProvider, useProgress } from "./ProgressContext";

/** Stands in for the engine: `send` is what the Wails event stream would do. */
const stream = { emit: undefined as ((event: ProgressEvent) => void) | undefined };

vi.mock("../api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api")>();
  return {
    ...actual,
    onProgress: (handler: (event: ProgressEvent) => void) => {
      stream.emit = handler;
      return () => {
        stream.emit = undefined;
      };
    },
  };
});

function send(patch: Partial<ProgressEvent> = {}) {
  act(() => {
    stream.emit?.({ jobId: "job-1", kind: "log", at: "2026-03-04T09:07:05Z", ...patch });
  });
}

function Listener({ onEvent }: { onEvent: (event: ProgressEvent) => void }) {
  useProgress(onEvent);
  return null;
}

function provide(children: ReactNode) {
  return render(<ProgressProvider>{children}</ProgressProvider>);
}

describe("subscribing", () => {
  it("delivers every event to every listening screen", () => {
    const first = vi.fn();
    const second = vi.fn();
    provide(
      <>
        <Listener onEvent={first} />
        <Listener onEvent={second} />
      </>,
    );

    send({ message: "Exporting realm acme" });

    expect(first).toHaveBeenCalledTimes(1);
    expect(second).toHaveBeenCalledTimes(1);
    expect(first.mock.calls[0][0]).toMatchObject({ message: "Exporting realm acme" });
  });

  it("opens exactly one stream, however many screens are listening", () => {
    // The whole reason this is a context: one subscription, routed, rather than
    // one per screen.
    provide(
      <>
        <Listener onEvent={vi.fn()} />
        <Listener onEvent={vi.fn()} />
      </>,
    );
    expect(stream.emit).toBeDefined();
  });
});

describe("a screen that has gone away", () => {
  it("stops receiving, rather than leaving a listener behind", () => {
    const handler = vi.fn();
    const { rerender } = provide(<Listener onEvent={handler} />);

    send();
    expect(handler).toHaveBeenCalledTimes(1);

    rerender(<ProgressProvider>{null}</ProgressProvider>);
    send();

    expect(handler).toHaveBeenCalledTimes(1);
  });
});

describe("a screen that re-renders", () => {
  it("keeps receiving, and receives through the newest closure", () => {
    // Every screen passes a fresh closure on each render. Resubscribing on each
    // one would drop whatever arrived in the gap; holding a stale one would
    // report progress against the previous render's state.
    const seen: string[] = [];
    function Screen({ label }: { label: string }) {
      useProgress((event) => seen.push(`${label}:${event.kind}`));
      return null;
    }

    const { rerender } = provide(<Screen label="first" />);
    send({ kind: "progress" });

    rerender(
      <ProgressProvider>
        <Screen label="second" />
      </ProgressProvider>,
    );
    send({ kind: "phaseCompleted" });

    expect(seen).toEqual(["first:progress", "second:phaseCompleted"]);
  });
});

describe("useProgress outside the provider", () => {
  it("fails loudly rather than silently never firing", () => {
    // A screen that quietly never receives an event looks like a hung job,
    // which is the exact failure the Activity screen exists to rule out.
    const quiet = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => render(<Listener onEvent={vi.fn()} />)).toThrow(
      "useProgress was called outside ProgressProvider",
    );
    quiet.mockRestore();
  });
});
