// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The Activity screen's reducer, which is where resilience becomes legible.
 *
 * A dropped connection has to look like a wait with a reason. That claim is
 * made entirely by this fold: the percentage, the sentence under the bar, which
 * phase tick is lit, and whether any of it is coloured as a warning. A retry
 * that renders as a silent stall is the bug the whole Activity screen exists to
 * prevent, and it is a bug in here rather than in the engine.
 *
 * The log tails are module-level state by design, so each test starts by
 * emptying them.
 */
import { beforeEach, describe, expect, it } from "vitest";

import type { ProgressEvent } from "../../api";
import { applyEvent, emptyLive, logTails, maxLogLines, structural, type Live } from "./live";

const job = "job-1";

function event(patch: Partial<ProgressEvent> & { kind: string }): ProgressEvent {
  return { jobId: job, at: "2026-03-04T09:07:05Z", ...patch };
}

/** Folds a run of events and returns the overlay they produced. */
function fold(...events: ProgressEvent[]): Live {
  const all = new Map<string, Live>();
  for (const one of events) applyEvent(all, one);
  return all.get(job) ?? emptyLive();
}

beforeEach(() => {
  logTails.clear();
});

describe("structural", () => {
  it("names the kinds that change what the engine would say, not just the text", () => {
    for (const kind of [
      "phaseStarted",
      "phaseCompleted",
      "phaseFailed",
      "jobState",
      "cloneCreated",
      "cloneDestroyed",
    ]) {
      expect(structural(kind)).toBe(true);
    }
  });

  it("does not refetch the job list for a log line or a percentage", () => {
    // A large export emits these by the thousand. Treating one as structural
    // would put the engine under a call per line.
    for (const kind of ["log", "progress", "retry", "breakerOpen"]) {
      expect(structural(kind)).toBe(false);
    }
  });
});

describe("an overlay before anything has happened", () => {
  it("is zero, quiet and empty", () => {
    const live = emptyLive();
    expect(live.percent).toBe(0);
    expect(live.warn).toBe(false);
    expect(live.note).toBe("");
    expect(live.steps.size).toBe(0);
  });

  it("is created on first sight of a job, whatever the event was", () => {
    const all = new Map<string, Live>();
    applyEvent(all, event({ kind: "somethingUnknown" }));
    expect(all.get(job)).toEqual(emptyLive());
  });
});

describe("progress", () => {
  it("turns a fraction into a percentage and a sentence", () => {
    const live = fold(event({ kind: "progress", current: 25, total: 200, item: "users.json" }));
    expect(live.percent).toBe(13);
    expect(live.note).toBe("13% · users.json");
  });

  it("never exceeds 100, however the engine counts", () => {
    // A total that turns out to be an estimate must not draw a 140% bar.
    const live = fold(event({ kind: "progress", current: 140, total: 100, item: "x" }));
    expect(live.percent).toBe(100);
  });

  it("counts without a bar when there is no total to divide by", () => {
    // Streaming an export whose size is unknown: a running count is honest, a
    // percentage would be invented.
    const live = fold(event({ kind: "progress", current: 120000, unit: "users", item: "acme" }));
    expect(live.percent).toBe(0);
    expect(live.note).toBe(`${(120000).toLocaleString()} users · acme`);
  });

  it("ignores an event carrying no number at all", () => {
    const live = fold(event({ kind: "progress", total: 100 }));
    expect(live).toEqual(emptyLive());
  });

  it("clears a warning once bytes are moving again", () => {
    // This is the recovery half of the promise: the screen said "retrying", and
    // it has to stop saying it when the retry worked.
    const live = fold(
      event({ kind: "retry", attempt: 2, retryIn: 4e9, message: "connection reset" }),
      event({ kind: "progress", current: 50, total: 100, item: "users.json" }),
    );
    expect(live.warn).toBe(false);
    expect(live.note).toBe("50% · users.json");
  });
});

describe("retry", () => {
  it("says which attempt failed and how long the wait is, in seconds", () => {
    // retryIn arrives as a Go duration — nanoseconds. Rendered raw it would
    // read "retrying in 4000000000s".
    const live = fold(
      event({ kind: "retry", attempt: 3, retryIn: 4_000_000_000, message: "connection reset" }),
    );
    expect(live.warn).toBe(true);
    expect(live.note).toBe("Attempt 3 failed — retrying in 4s. connection reset");
  });

  it("still says something when the engine offered no message", () => {
    const live = fold(event({ kind: "retry", attempt: 1, retryIn: 1e9 }));
    expect(live.note).toContain("Attempt 1 failed");
    expect(live.note).not.toContain("undefined");
  });
});

describe("breakerOpen", () => {
  it("names what is unreachable and promises nothing is lost", () => {
    const live = fold(event({ kind: "breakerOpen", item: "archive", retryIn: 60_000_000_000 }));
    expect(live.warn).toBe(true);
    expect(live.note).toBe(
      "Paused — archive has been unreachable. Retrying in 60s. Nothing is lost.",
    );
  });
});

describe("phases", () => {
  it("lights the current tick, preferring the engine's label to its phase id", () => {
    const live = fold(event({ kind: "phaseStarted", phase: "export", label: "Exporting realm" }));
    expect(live.steps.get("export")).toBe("live");
    expect(live.note).toBe("Exporting realm");
  });

  it("falls back to the phase id when there is no label", () => {
    const live = fold(event({ kind: "phaseStarted", phase: "export" }));
    expect(live.note).toBe("export");
  });

  it("leaves at most one tick live at a time", () => {
    // A phase that starts without its predecessor having reported completion
    // must not leave two spinners on the pipeline.
    const live = fold(
      event({ kind: "phaseStarted", phase: "export" }),
      event({ kind: "phaseStarted", phase: "upload" }),
    );
    expect(live.steps.has("export")).toBe(false);
    expect(live.steps.get("upload")).toBe("live");
  });

  it("keeps a completed tick when the next phase starts", () => {
    const live = fold(
      event({ kind: "phaseStarted", phase: "export" }),
      event({ kind: "phaseCompleted", phase: "export" }),
      event({ kind: "phaseStarted", phase: "upload" }),
    );
    expect(live.steps.get("export")).toBe("done");
    expect(live.steps.get("upload")).toBe("live");
  });

  it("marks a failed phase, warns, and shows the reason it failed", () => {
    const live = fold(
      event({ kind: "phaseStarted", phase: "upload" }),
      event({ kind: "phaseFailed", phase: "upload", message: "the bucket refused the part" }),
    );
    expect(live.steps.get("upload")).toBe("failed");
    expect(live.warn).toBe(true);
    expect(live.note).toBe("the bucket refused the part");
  });

  it("keeps a failed tick failed rather than clearing it at the next phase", () => {
    const live = fold(
      event({ kind: "phaseFailed", phase: "upload", message: "refused" }),
      event({ kind: "phaseStarted", phase: "cleanup" }),
    );
    expect(live.steps.get("upload")).toBe("failed");
  });
});

describe("the log tail", () => {
  it("keeps what the export said, attributed to the export", () => {
    fold(event({ kind: "log", message: "Exporting realm acme" }));
    expect(logTails.get(job)).toEqual([{ text: "Exporting realm acme", fromPortCloak: false }]);
  });

  it("marks PortCloak's own lines as its own, so they read differently", () => {
    fold(
      event({ kind: "cloneCreated", item: "portcloak-clone-1" }),
      event({ kind: "cloneDestroyed", item: "portcloak-clone-1" }),
    );
    expect(logTails.get(job)).toEqual([
      { text: "Ephemeral clone portcloak-clone-1 is running.", fromPortCloak: true },
      { text: "Ephemeral clone portcloak-clone-1 destroyed.", fromPortCloak: true },
    ]);
  });

  it("drops an empty log line rather than pushing a blank row", () => {
    fold(event({ kind: "log", message: "" }));
    expect(logTails.has(job)).toBe(false);
  });

  it("is a tail, not a file — it keeps the last lines and discards the first", () => {
    // A 120,000-user export talks enough to exhaust the renderer otherwise.
    const all = new Map<string, Live>();
    for (let i = 0; i < maxLogLines + 50; i++) {
      applyEvent(all, event({ kind: "log", message: `line ${i}` }));
    }

    const lines = logTails.get(job)!;
    expect(lines).toHaveLength(maxLogLines);
    expect(lines[0].text).toBe("line 50");
    expect(lines[lines.length - 1].text).toBe(`line ${maxLogLines + 49}`);
  });

  it("keeps each job's output apart", () => {
    const all = new Map<string, Live>();
    applyEvent(all, { jobId: "capture", kind: "log", message: "one", at: "" });
    applyEvent(all, { jobId: "restore", kind: "log", message: "two", at: "" });

    expect(logTails.get("capture")).toHaveLength(1);
    expect(logTails.get("restore")).toHaveLength(1);
    expect(all.size).toBe(2);
  });
});

describe("an event missing a field the engine usually sends", () => {
  // Every fallback below is one the operator would otherwise read as
  // "undefined" or "NaN" in the sentence under the progress bar. They are
  // reachable: the engine omits `item` on a phase that is not per-object, and
  // omits `retryIn` when a retry is immediate.
  it("renders a percentage with nothing to name", () => {
    expect(fold(event({ kind: "progress", current: 1, total: 4 })).note).toBe("25% · ");
  });

  it("renders a running count with no unit and nothing to name", () => {
    expect(fold(event({ kind: "progress", current: 7 })).note).toBe("7  · ");
  });

  it("renders an immediate retry as 0s rather than as NaN", () => {
    expect(fold(event({ kind: "retry", attempt: 1 })).note).toBe(
      "Attempt 1 failed — retrying in 0s. ",
    );
  });

  it("renders an immediate breaker retry as 0s", () => {
    expect(fold(event({ kind: "breakerOpen", item: "archive" })).note).toBe(
      "Paused — archive has been unreachable. Retrying in 0s. Nothing is lost.",
    );
  });

  it("says nothing rather than 'undefined' for a phase with neither label nor id", () => {
    const live = fold(event({ kind: "phaseStarted" }));
    expect(live.note).toBe("");
    expect(live.steps.size).toBe(0);
  });

  it("ticks nothing for a completion that does not say which phase completed", () => {
    expect(fold(event({ kind: "phaseCompleted" })).steps.size).toBe(0);
  });

  it("still warns on a failure that names neither a phase nor a reason", () => {
    // The tick cannot be drawn, but the run did fail, and the screen has to
    // stop looking like it is still working.
    const live = fold(event({ kind: "phaseFailed" }));
    expect(live.warn).toBe(true);
    expect(live.steps.size).toBe(0);
    expect(live.note).toBe("");
  });
});

describe("a whole run", () => {
  it("reads as a drop, a wait, a recovery and a finish", () => {
    const live = fold(
      event({ kind: "phaseStarted", phase: "upload", label: "Uploading bundle" }),
      event({ kind: "progress", current: 40, total: 100, item: "acme.pck.age" }),
      event({ kind: "retry", attempt: 1, retryIn: 2e9, message: "connection reset" }),
      event({ kind: "progress", current: 100, total: 100, item: "acme.pck.age" }),
      event({ kind: "phaseCompleted", phase: "upload" }),
    );

    expect(live.percent).toBe(100);
    expect(live.warn).toBe(false);
    expect(live.steps.get("upload")).toBe("done");
  });
});
