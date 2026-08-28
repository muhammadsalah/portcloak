// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * What the event stream knows that the job list does not yet.
 *
 * The log tails are module-level on purpose: they survive a repaint, they
 * survive the job finishing — so the last thing the export said is still on
 * screen at the moment it matters most — and they survive navigating away and
 * coming back, which is the first thing an operator does when a job looks
 * stuck.
 *
 * The fold from an event to that overlay lives here too. It is the whole of
 * what the Activity screen derives rather than asks the engine for — a
 * percentage, the sentence under the bar, which phase tick is lit — and it is
 * ordinary data in, ordinary data out. Keeping it out of the component is what
 * lets a test drive a retry, a breaker opening and a phase failing through it
 * in sequence, which is the shape of the run it actually has to describe.
 */
import type { ProgressEvent } from "../../api";

export interface LogLine {
  text: string;
  /** Written by PortCloak rather than said by the export, and coloured for it. */
  fromPortCloak: boolean;
}

export const logTails = new Map<string, LogLine[]>();

/** The overlay for one job: progress, the line under it, and phase ticks. */
export interface Live {
  percent: number;
  warn: boolean;
  note: string;
  steps: Map<string, "live" | "done" | "failed">;
}

export function emptyLive(): Live {
  return { percent: 0, warn: false, note: "", steps: new Map() };
}

/** Kinds that change what the engine would say about a job, not just its text. */
export function structural(kind: string): boolean {
  return (
    kind === "phaseStarted" ||
    kind === "phaseCompleted" ||
    kind === "phaseFailed" ||
    kind === "jobState" ||
    kind === "cloneCreated" ||
    kind === "cloneDestroyed"
  );
}

/** Folds one event into the overlay for the job it belongs to. */
export function applyEvent(all: Map<string, Live>, event: ProgressEvent): void {
  const current = all.get(event.jobId) ?? emptyLive();
  all.set(event.jobId, current);

  switch (event.kind) {
    case "log":
      if (event.message) remember(event.jobId, event.message);
      break;

    case "progress":
      if (event.total && event.total > 0 && event.current !== undefined) {
        current.percent = Math.min(100, Math.round((event.current / event.total) * 100));
        current.warn = false;
        current.note = `${current.percent}% · ${event.item ?? ""}`;
      } else if (event.current !== undefined) {
        current.note = `${event.current.toLocaleString()} ${event.unit ?? ""} · ${event.item ?? ""}`;
      }
      break;

    case "retry":
      current.warn = true;
      current.note = `Attempt ${event.attempt} failed — retrying in ${Math.round((event.retryIn ?? 0) / 1e9)}s. ${event.message ?? ""}`;
      break;

    case "breakerOpen":
      current.warn = true;
      current.note = `Paused — ${event.item} has been unreachable. Retrying in ${Math.round((event.retryIn ?? 0) / 1e9)}s. Nothing is lost.`;
      break;

    case "phaseStarted":
      current.note = event.label ?? event.phase ?? "";
      // Written here rather than waiting for the refresh: the tick is the one
      // piece of feedback that has to feel immediate.
      if (event.phase) {
        for (const [phase, state] of current.steps) {
          if (state === "live") current.steps.delete(phase);
        }
        current.steps.set(event.phase, "live");
      }
      break;

    case "phaseCompleted":
      if (event.phase) current.steps.set(event.phase, "done");
      break;

    case "phaseFailed":
      if (event.phase) current.steps.set(event.phase, "failed");
      current.warn = true;
      if (event.message) current.note = event.message;
      break;

    case "cloneCreated":
      remember(event.jobId, `Ephemeral clone ${event.item} is running.`, true);
      break;

    case "cloneDestroyed":
      remember(event.jobId, `Ephemeral clone ${event.item} destroyed.`, true);
      break;
  }
}

/** How many lines of a job's output are kept. It is a log tail, not a log file. */
export const maxLogLines = 500;

function remember(jobId: string, line: string, fromPortCloak = false): void {
  const lines = logTails.get(jobId) ?? [];
  lines.push({ text: line, fromPortCloak });
  if (lines.length > maxLogLines) lines.splice(0, lines.length - maxLogLines);
  logTails.set(jobId, lines);
}
