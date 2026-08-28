// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The probe panel reports the facts a capture depends on, not a green tick.
 *
 * It is shared by the Environments editor and the capture wizard's first step
 * so that an operator does not have to learn two answers to the same question.
 * The distinctions it draws are the point: a passing check is stated plainly, a
 * skipped one is not a passing one, and a warning is not a failure. Collapsing
 * any of those into a tick is how a capture that was never going to work gets
 * started.
 */
import { describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";

import type { TargetFacts } from "../api";
import { renderApp } from "../test/render";
import { ProbePanel } from "./ProbePanel";

function facts(checks: TargetFacts["checks"] = []): TargetFacts {
  return {
    kind: "ssh",
    reachable: true,
    hasTar: true,
    mode: "read-only",
    cloneCapable: true,
    adminReachable: true,
    ports: { http: 8080, https: 8443, management: 9000 },
    probedAt: "2026-03-04T09:07:05Z",
    checks,
    readOnlyNote: "Nothing on the serving instance is written to.",
  };
}

describe("the headline", () => {
  it("promises the serving instance is not touched when the probe passed", () => {
    renderApp(<ProbePanel facts={facts()} ok />);
    expect(
      screen.getByText("Probe passed — capture will not touch the serving instance"),
    ).toBeInTheDocument();
  });

  it("says a blocker was found, rather than showing a red tick", () => {
    renderApp(<ProbePanel facts={facts()} ok={false} />);
    expect(screen.getByText("The probe found a blocking problem")).toBeInTheDocument();
  });
});

describe("the checks", () => {
  it("lists each one with what it found", () => {
    renderApp(
      <ProbePanel
        facts={facts([
          { name: "Keycloak version", value: "26.0.7", status: "pass", blocking: false },
          { name: "Free space", value: "12.4 GiB", status: "pass", blocking: false },
        ])}
        ok
      />,
    );

    expect(screen.getByText("Keycloak version")).toBeInTheDocument();
    expect(screen.getByText("26.0.7")).toBeInTheDocument();
    expect(screen.getByText("Free space")).toBeInTheDocument();
  });

  it("states a passing check plainly, and badges the other three", () => {
    // A row of green badges is a wall of colour nobody reads. The badge is
    // reserved for the rows that need an answer.
    renderApp(
      <ProbePanel
        facts={facts([
          { name: "Reachable", value: "yes", status: "pass", blocking: false },
          { name: "Clone support", value: "unavailable", status: "warn", blocking: false },
          { name: "Admin API", value: "refused", status: "fail", blocking: true },
          { name: "Theme scan", value: "not run", status: "skipped", blocking: false },
        ])}
        ok={false}
      />,
    );

    // Both render as a span, so the badge is identified by the component that
    // produced it — which is legible here only because the styled-components
    // displayName transform is on. See vitest.config.ts.
    expect(screen.getByText("yes").className).not.toMatch(/Badge/);
    for (const value of ["unavailable", "refused", "not run"]) {
      expect(screen.getByText(value).className).toMatch(/Badge/);
    }
  });

  it("shows the advice attached to a check, which is what makes it actionable", () => {
    renderApp(
      <ProbePanel
        facts={facts([
          {
            name: "Admin API",
            value: "refused",
            status: "fail",
            blocking: true,
            advice: "Add a service account with the realm-admin role.",
          },
        ])}
        ok={false}
      />,
    );

    expect(
      screen.getByText("Add a service account with the realm-admin role."),
    ).toBeInTheDocument();
  });

  it("renders a check with no advice without inventing one", () => {
    renderApp(
      <ProbePanel
        facts={facts([{ name: "Reachable", value: "yes", status: "pass", blocking: false }])}
        ok
      />,
    );
    expect(screen.getByText("Reachable")).toBeInTheDocument();
  });
});

describe("the read-only note", () => {
  it("is shown whether the probe passed or not", () => {
    const note = "Nothing on the serving instance is written to.";

    const passing = renderApp(<ProbePanel facts={facts()} ok />);
    expect(screen.getByText(note)).toBeInTheDocument();
    passing.unmount();

    renderApp(<ProbePanel facts={facts()} ok={false} />);
    expect(screen.getByText(note)).toBeInTheDocument();
  });
});
