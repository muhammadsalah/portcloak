// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * What the event stream knows that the job list does not yet.
 *
 * The log tails are module-level on purpose: they survive a repaint, they
 * survive the job finishing — so the last thing the export said is still on
 * screen at the moment it matters most — and they survive navigating away and
 * coming back, which is the first thing an operator does when a job looks
 * stuck. What they do not survive is the window being reloaded or the app being
 * restarted mid-run, which is why the engine keeps its own copy and this one is
 * reconciled against it.
 *
 * The fold from an event to that overlay lives here too. It is the whole of
 * what the Activity screen derives rather than asks the engine for — a
 * percentage, the sentence under the bar, which phase tick is lit — and it is
 * ordinary data in, ordinary data out. Keeping it out of the component is what
 * lets a test drive a retry, a breaker opening and a phase failing through it
 * in sequence, which is the shape of the run it actually has to describe.
 */
import type { LogLine, ProgressEvent } from "@/api";

export type { LogLine };

/**
 * A job's output, in two parts.
 *
 * `confirmed` is what the engine has handed over and is the authority: it
 * recorded the same events, it is the only copy that survives a reload or a
 * second window, and it is where a line the stream dropped comes back from.
 * `heard` is what the stream has said since — kept separate rather than
 * appended, because those same lines arrive again in the next fetch and a
 * single list would show them twice.
 *
 * What the screen draws is the two of them end to end.
 */
interface Tail {
  confirmed: LogLine[];
  heard: LogLine[];
  /** How many lines of this run the engine has handed over, ever. */
  cursor: number;
  /**
   * The two halves joined, remembered until one of them changes.
   *
   * The screen repaints ten times a second while a job is talking, and every
   * repaint asks every card for its tail. Building a fresh ten-thousand-element
   * array each time — and handing React a new identity, so it reconciles the
   * whole list again — is most of the cost of drawing this screen. The cache is
   * what lets an unchanged tail be recognised as unchanged.
   */
  joined?: LogLine[];
}

const tails = new Map<string, Tail>();

function tailFor(jobId: string): Tail {
  const existing = tails.get(jobId);
  if (existing) return existing;
  const fresh: Tail = { confirmed: [], heard: [], cursor: 0 };
  tails.set(jobId, fresh);
  return fresh;
}

/**
 * What the job card draws: the engine's record, then anything since.
 *
 * The same array comes back until the tail actually changes, so a repaint that
 * changed nothing costs a map lookup rather than a ten-thousand-line copy and a
 * full reconciliation.
 */
export function logTail(jobId: string): LogLine[] {
  const tail = tails.get(jobId);
  if (!tail) return empty;
  if (!tail.joined) {
    const all = tail.confirmed.concat(tail.heard);
    tail.joined = all.length > maxLogLines ? all.slice(-maxLogLines) : all;
  }
  return tail.joined;
}

/** One shared empty array, so a job with no output keeps one identity. */
const empty: LogLine[] = [];

/** Whether anything is held for a job, so the card knows to draw the panel. */
export function hasLogTail(jobId: string): boolean {
  const tail = tails.get(jobId);
  return tail !== undefined && tail.confirmed.length + tail.heard.length > 0;
}

/** The cursor to ask the engine from — how much of this run it has handed over. */
export function logCursor(jobId: string): number {
  return tails.get(jobId)?.cursor ?? 0;
}

/** Every job something is held for, so a caller can drop the ones that are gone. */
export function tailIds(): string[] {
  return [...tails.keys()];
}

/** Drops everything held for a job that is no longer listed. */
export function forgetTail(jobId: string): void {
  tails.delete(jobId);
}

/** For tests: start from nothing. */
export function clearTails(): void {
  tails.clear();
}

/**
 * Apply what the engine handed over.
 *
 * `heardBefore` is how many streamed lines were held when the request went out.
 * Those are the ones this answer covers, so they are the ones dropped; anything
 * that arrived while the request was in flight stays, and is covered by the
 * next one. Dropping the lot instead would blink the newest line off the screen
 * for a second every second.
 */
export function applyTail(
  jobId: string,
  page: { lines: LogLine[]; next: number; reset: boolean },
  heardBefore: number,
): boolean {
  const tail = tailFor(jobId);
  if (page.lines.length === 0 && !page.reset) {
    // Nothing new to add, but the cursor still moves: the engine has confirmed
    // everything up to here.
    tail.cursor = Math.max(tail.cursor, page.next);
    return false;
  }
  tail.confirmed = page.reset ? page.lines.slice() : tail.confirmed.concat(page.lines);
  if (tail.confirmed.length > maxLogLines) {
    tail.confirmed = tail.confirmed.slice(-maxLogLines);
  }
  tail.heard = tail.heard.slice(heardBefore);
  tail.cursor = page.next;
  tail.joined = undefined;
  return true;
}

/** How many streamed lines are waiting to be confirmed, at this moment. */
export function heardCount(jobId: string): number {
  return tails.get(jobId)?.heard.length ?? 0;
}

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
      current.note = `Attempt ${event.attempt} failed. Retrying in ${Math.round((event.retryIn ?? 0) / 1e9)}s. ${event.message ?? ""}`;
      break;

    case "breakerOpen":
      current.warn = true;
      current.note = `Paused. ${event.item} has been unreachable. Retrying in ${Math.round((event.retryIn ?? 0) / 1e9)}s. Nothing is lost.`;
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

/**
 * How many lines of a job's output are kept, matching the engine's own cap.
 *
 * It is a log tail, not a log file — but ten thousand lines holds a whole export
 * of a large realm, which is what an operator scrolls back through when a job
 * looks stuck.
 */
export const maxLogLines = 10_000;

function remember(jobId: string, line: string, fromPortCloak = false): void {
  const tail = tailFor(jobId);
  tail.heard.push({ text: line, fromPortCloak });
  if (tail.heard.length > maxLogLines) {
    tail.heard.splice(0, tail.heard.length - maxLogLines);
  }
  tail.joined = undefined;
}
