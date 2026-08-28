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
 *     second of staleness rather than a permanently wrong screen.
 */
import { useCallback, useEffect, useRef, useState } from "react";

import { JobsAPI, type ActivityView } from "../../api";
import { useProgress } from "../../app/ProgressContext";
import { Notice, PageSubtitle, PageTitle, Spinner } from "../../design-system";
import { JobCard } from "./JobCard";
import { applyEvent, logTails, structural, type Live } from "./live";

/** How long a burst of events is allowed to coalesce into one refresh. */
const coalesceMs = 400;
/** How often the screen re-reads the job list while anything is in flight. */
const pollMs = 2000;
/** How often the live overlay is painted, however fast the events arrive. */
const flushMs = 100;

export function ActivityPage() {
  const [view, setView] = useState<ActivityView | null>(null);
  const [error, setError] = useState<unknown>(null);

  // What the event stream has said since the last time the engine was asked.
  // Held in a ref and painted on a tick: a large export produces log lines
  // faster than it is worth re-rendering the screen for.
  const live = useRef(new Map<string, Live>());
  const [, repaint] = useState(0);
  const dirty = useRef(false);

  const reload = useCallback(async () => {
    try {
      setView(await JobsAPI.list());
    } catch (failure) {
      // The first read failing is worth saying; a later one is not worth
      // replacing a screen that is still showing the truth as of a moment ago.
      // The poll will try again.
      setView((current) => {
        if (!current) setError(failure);
        return current;
      });
    }
  }, []);

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
    for (const id of logTails.keys()) {
      if (!listed.has(id)) logTails.delete(id);
    }
    for (const id of live.current.keys()) {
      if (!listed.has(id)) live.current.delete(id);
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
