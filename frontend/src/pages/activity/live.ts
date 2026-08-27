/**
 * What the event stream knows that the job list does not yet.
 *
 * The log tails are module-level on purpose: they survive a repaint, they
 * survive the job finishing — so the last thing the export said is still on
 * screen at the moment it matters most — and they survive navigating away and
 * coming back, which is the first thing an operator does when a job looks
 * stuck.
 */

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
