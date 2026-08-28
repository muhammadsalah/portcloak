// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The boundary exists because of a real failure: on a first launch the engine
 * answered an unconfigured PortCloak with null lists, the first `.length` threw
 * during render, and nothing replaced the spinner. The application looked hung
 * rather than broken, which is the worse of the two.
 *
 * These tests hold the guarantee that made that visible — a screen that fails
 * says so, offers the one useful action, and does not follow the operator to
 * the next screen.
 *
 * React writes a caught error to console.error whether or not a boundary
 * handled it, so each test that throws silences it; a test suite whose passing
 * output is full of red stack traces trains people to ignore it.
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { renderApp } from "../test/render";
import { ViewBoundary } from "./ViewBoundary";

const quiet = vi.spyOn(console, "error").mockImplementation(() => {});

afterEach(() => {
  quiet.mockClear();
});

function Boom({
  throws,
  message = "realms is not iterable",
}: {
  throws: boolean;
  message?: string;
}) {
  if (throws) throw new Error(message);
  return <p>The screen drew fine.</p>;
}

describe("a screen that draws", () => {
  it("is rendered untouched", () => {
    renderApp(
      <ViewBoundary resetKey="library#1">
        <Boom throws={false} />
      </ViewBoundary>,
    );
    expect(screen.getByText("The screen drew fine.")).toBeInTheDocument();
  });
});

describe("a screen that throws", () => {
  it("says so, in a sentence, instead of leaving a spinner up", () => {
    renderApp(
      <ViewBoundary resetKey="library#1">
        <Boom throws />
      </ViewBoundary>,
    );

    expect(screen.getByText("This screen could not be drawn.")).toBeInTheDocument();
    expect(screen.getByText("realms is not iterable")).toBeInTheDocument();
  });

  it("puts the component stack in the console, not on the screen", () => {
    renderApp(
      <ViewBoundary resetKey="library#1">
        <Boom throws />
      </ViewBoundary>,
    );

    expect(quiet).toHaveBeenCalledWith(
      "A screen could not be drawn.",
      expect.any(Error),
      expect.anything(),
    );
  });

  it("renders something thrown that was never an Error", () => {
    function ThrowsAString(): never {
      throw "the engine returned null";
    }

    renderApp(
      <ViewBoundary resetKey="library#1">
        <ThrowsAString />
      </ViewBoundary>,
    );
    expect(screen.getByText("the engine returned null")).toBeInTheDocument();
  });

  it("offers the only useful action", async () => {
    function Flaky({ fail }: { fail: { now: boolean } }) {
      if (fail.now) throw new Error("first attempt");
      return <p>Second attempt drew.</p>;
    }
    const fail = { now: true };

    renderApp(
      <ViewBoundary resetKey="library#1">
        <Flaky fail={fail} />
      </ViewBoundary>,
    );
    expect(screen.getByText("This screen could not be drawn.")).toBeInTheDocument();

    fail.now = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));

    expect(screen.getByText("Second attempt drew.")).toBeInTheDocument();
  });
});

describe("navigating away from a failure", () => {
  it("clears it, rather than keeping the content column on the notice", () => {
    // Without this the whole column stays on the error for the rest of the
    // session, and every screen the operator visits afterwards looks broken.
    const { rerender } = renderApp(
      <ViewBoundary resetKey="library#1">
        <Boom throws />
      </ViewBoundary>,
    );
    expect(screen.getByText("This screen could not be drawn.")).toBeInTheDocument();

    rerender(
      <ViewBoundary resetKey="settings#2">
        <Boom throws={false} />
      </ViewBoundary>,
    );

    expect(screen.queryByText("This screen could not be drawn.")).not.toBeInTheDocument();
    expect(screen.getByText("The screen drew fine.")).toBeInTheDocument();
  });

  it("keeps the failure on screen while the route has not changed", () => {
    const { rerender } = renderApp(
      <ViewBoundary resetKey="library#1">
        <Boom throws />
      </ViewBoundary>,
    );

    rerender(
      <ViewBoundary resetKey="library#1">
        <Boom throws={false} />
      </ViewBoundary>,
    );

    expect(screen.getByText("This screen could not be drawn.")).toBeInTheDocument();
  });
});
