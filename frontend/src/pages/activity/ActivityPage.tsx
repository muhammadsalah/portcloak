// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The Activity screen is where resilience becomes legible. A dropped connection
 * has to look like a wait with a reason, not a hang.
 *
 * That only holds if the screen keeps up with the run:
 *
 *   · the event stream writes into a live overlay, flushed on a short tick, so
 *     a streamed log line and a phase tick appear the instant they happen
 *     without a re-render per line on an export that talks a lot;
 *   · anything structural — a phase boundary, a job changing state — asks the
 *     engine for the job list again, coalesced so a burst costs one call;
 *   · a slow poll runs while anything is in flight, so a missed event is a
 *     second of staleness rather than a permanently wrong screen, and the
 *     elapsed time on a running job advances once a second.
 */
import { useCallback, useEffect, useRef, useState } from "react";

import { JobsAPI, type ActivityView, type JobView } from "../../api";
import { useProgress } from "../../app/ProgressContext";
import { Notice, PageSubtitle, PageTitle, Spinner } from "../../design-system";
import { JobCard } from "./JobCard";
import {
  applyEvent,
  applyTail,
  forgetTail,
  heardCount,
  logCursor,
  tailIds,
  structural,
  type Live,
} from "./live";

/** How long a burst of events is allowed to coalesce into one refresh. */
const coalesceMs = 400;
/**
 * How often the screen re-reads the job list while anything is in flight.
 *
 * One second, because the elapsed time on a running card comes from this read.
 * At two it advanced in jumps of one second and two, which reads as a clock
 * that is losing time rather than one counting up.
 */
const pollMs = 1000;
/** How often the live overlay is painted, however fast the events arrive. */
const flushMs = 100;

export function ActivityPage() {
  const [view, setView] = useState<ActivityView | null>(null);
  const [error, setError] = useState<unknown>(null);

  // What the event stream has said since the last time the engine was asked.
  // Held in a ref and painted on a tick: a large export produces log lines
  // faster than it is worth re-rendering the screen for.
  const live = useRef(new Map<string, Live>());
  /** Jobs whose output has already been asked for at least once. */
  const asked = useRef(new Set<string>());
  const [, repaint] = useState(0);
  const dirty = useRef(false);

  // A refresh re-reads the output, rather than trusting that this screen was
  // listening when it was said. It was not, whenever the app was restarted
  // mid-run, the window was reloaded, or the operator opened the screen after
  // the export had been talking for a minute — and the events cannot be
  // replayed, because the exec stream they came from is long closed. The engine
  // recorded them; this is where that record is read back.
  //
  // Only jobs that could still be saying something are asked about. A finished
  // job's tail cannot change, so re-fetching it every second would be a
  // kilobyte a second of nothing new.
  const reloadLogs = useCallback(async (jobs: JobView[]) => {
    const talking = jobs.filter((job) => {
      if (job.state === "running" || job.state === "queued") return true;
      // A job that has stopped is asked about once. Without that, one whose
      // output the engine no longer holds — an old job, or one from before this
      // build — would be asked about on every poll for as long as it is listed.
      if (asked.current.has(job.id)) return false;
      asked.current.add(job.id);
      return true;
    });
    const pages = await Promise.all(
      talking.map((job) => {
        // Recorded before the request goes out: these are the streamed lines
        // this answer will account for. Anything that arrives while it is in
        // flight belongs to the next one.
        const heardBefore = heardCount(job.id);
        return (
          JobsAPI.log(job.id, logCursor(job.id))
            .then((log) => ({ id: job.id, log, heardBefore }))
            // One job's output failing to load must not take the job list with
            // it: the list is the part that says whether anything is wrong.
            .catch(() => undefined)
        );
      }),
    );
    let changed = false;
    for (const page of pages) {
      if (page && applyTail(page.id, page.log, page.heardBefore)) changed = true;
    }
    if (changed) dirty.current = true;
  }, []);

  const reload = useCallback(async () => {
    try {
      const next = await JobsAPI.list();
      setView(next);
      await reloadLogs(next.jobs);
    } catch (failure) {
      // The first read failing is worth saying; a later one is not worth
      // replacing a screen that is still showing the truth as of a moment ago.
      // The poll will try again.
      setView((current) => {
        if (!current) setError(failure);
        return current;
      });
    }
  }, [reloadLogs]);

  useEffect(() => {
    void reload();
  }, [reload]);

  // Paint whatever the events have written, at most ten times a second.
  useEffect(() => {
    const flush = window.setInterval(() => {
      if (!dirty.current) return;
      dirty.current = false;
      repaint((n) => n + 1);
    }, flushMs);
    return () => window.clearInterval(flush);
  }, []);

  // A phase boundary or a state change alters the pipeline, the badge, the
  // buttons and the ledger — all of which come from the engine rather than from
  // the event. Ask for them rather than half-deriving them here.
  const pending = useRef<number | undefined>(undefined);
  const scheduleReload = useCallback(() => {
    if (pending.current !== undefined) return;
    pending.current = window.setTimeout(() => {
      pending.current = undefined;
      void reload();
    }, coalesceMs);
  }, [reload]);

  useEffect(
    () => () => {
      if (pending.current !== undefined) window.clearTimeout(pending.current);
    },
    [],
  );

  useProgress((event) => {
    applyEvent(live.current, event);
    dirty.current = true;
    if (structural(event.kind)) scheduleReload();
  });

  const running = view?.jobs.some((job) => job.state === "running" || job.state === "queued");

  useEffect(() => {
    if (!running) return;
    const poll = window.setInterval(() => void reload(), pollMs);
    return () => window.clearInterval(poll);
  }, [running, reload]);

  // A job that is no longer listed — discarded, purged — takes its buffered
  // output with it.
  useEffect(() => {
    if (!view) return;
    const listed = new Set(view.jobs.map((job) => job.id));
    // Tails are dropped from their own keys, not from the overlay's: a job
    // whose output was fetched but which never streamed an event has one
    // without the other.
    for (const id of tailIds()) {
      if (!listed.has(id)) forgetTail(id);
    }
    for (const id of live.current.keys()) {
      if (!listed.has(id)) live.current.delete(id);
    }
    for (const id of asked.current) {
      if (!listed.has(id)) asked.current.delete(id);
    }
  }, [view]);

  if (error) {
    return <Notice tone="danger" title="The job list could not be read." body={String(error)} />;
  }
  if (!view) return <Spinner>Reading jobs…</Spinner>;

  return (
    <div>
      <PageTitle>Activity</PageTitle>
      <PageSubtitle>{view.summary}</PageSubtitle>

      {view.jobs.length === 0 ? (
        <Notice
          tone="info"
          title="Nothing has run yet"
          body="Captures and restores appear here while they run, and stay afterwards with what they did."
        />
      ) : (
        view.jobs.map((job) => (
          <JobCard
            key={job.id}
            job={job}
            live={live.current.get(job.id)}
            reload={() => void reload()}
          />
        ))
      )}
    </div>
  );
}
