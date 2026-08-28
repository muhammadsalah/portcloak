// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * What a running job looks like: the phase pipeline, the bar, and the tail of
 * whatever the export is saying.
 *
 * These live in the design system rather than on the Activity screen because
 * they are the vocabulary of "something is happening and here is how far it
 * got", and that is the one thing the app has to be able to say convincingly.
 */
import styled from "styled-components";

export const Pipeline = styled.div`
  display: flex;
  gap: 4px;
  margin: 12px 0;
  flex-wrap: wrap;
`;

/**
 * The phases of a run, down the page rather than across it.
 *
 * Across the page they wrapped: nine phases on one line, then one on the next,
 * with no relationship between a step's position and how far the run had got.
 * Down the page every step is in the same place every time, which is what makes
 * "where is it now" a glance rather than a read — and a numbered step can be
 * named out loud, which the ticks could not be.
 */
export const Stepper = styled.ol`
  list-style: none;
  margin: 0;
  padding: 0;
`;

export type StepState = "pending" | "done" | "live" | "failed";

const stepColour = (p: { $state: StepState; theme: { color: Record<string, string> } }) =>
  p.$state === "live"
    ? p.theme.color.primary
    : p.$state === "failed"
      ? p.theme.color.danger
      : p.$state === "done"
        ? p.theme.color.success
        : p.theme.color.textMuted;

export const Step = styled.li<{ $state: StepState; $last?: boolean }>`
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding-bottom: ${(p) => (p.$last ? 0 : 10)}px;

  /* The line joins a step to the one after it, and is drawn in the colour of
     the step it leaves — so the run's progress reads as a filled thread rather
     than as a column of unrelated marks. */
  &::before {
    content: "";
    position: absolute;
    left: 10px;
    top: 21px;
    bottom: 0;
    width: 2px;
    background: ${(p) =>
      p.$last
        ? "transparent"
        : p.$state === "done"
          ? p.theme.color.success
          : p.theme.color.borderSubtle};
  }
`;

/** The numbered circle: where this step sits in the run, and how it went. */
export const StepMarker = styled.span<{ $state: StepState }>`
  flex: none;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  border: 2px solid ${stepColour};
  background: ${(p) => (p.$state === "pending" ? p.theme.color.surface : stepColour(p))};
  color: ${(p) => (p.$state === "pending" ? p.theme.color.textMuted : "#fff")};
`;

export const StepLabel = styled.span<{ $state: StepState }>`
  font-size: 13px;
  line-height: 22px;
  color: ${(p) =>
    p.$state === "pending"
      ? p.theme.color.textMuted
      : p.$state === "failed"
        ? p.theme.color.danger
        : p.theme.color.text};
  font-weight: ${(p) => (p.$state === "live" || p.$state === "failed" ? 600 : 400)};
`;

export const PipelineStep = styled.div<{ $state: "pending" | "done" | "live" | "failed" }>`
  font-size: 12px;
  display: flex;
  align-items: center;
  gap: 5px;
  padding-right: 12px;
  color: ${(p) =>
    p.$state === "live"
      ? p.theme.color.primary
      : /* A phase that failed is named as such rather than simply left
           un-ticked: "did not happen" and "went wrong here" are different
           answers. */
        p.$state === "failed"
        ? p.theme.color.danger
        : p.$state === "done"
          ? p.theme.color.text
          : p.theme.color.textMuted};
  font-weight: ${(p) => (p.$state === "live" || p.$state === "failed" ? 500 : 400)};
`;

export const ProgressTrack = styled.div`
  height: 4px;
  background: ${(p) => p.theme.color.borderSubtle};
  border-radius: 2px;
  overflow: hidden;
  margin: 8px 0;
`;

export const ProgressBar = styled.div<{ $percent: number; $warn?: boolean }>`
  height: 100%;
  width: ${(p) => p.$percent}%;
  background: ${(p) => (p.$warn ? p.theme.color.warning : p.theme.color.primary)};
  transition: width 0.2s;
`;

/** The tail of a job's streamed output. Bounded, scrolled, and never wrapped away. */
export const Log = styled.div`
  background: ${(p) => p.theme.color.logBg};
  color: ${(p) => p.theme.color.logText};
  border-radius: ${(p) => p.theme.radius.base};
  padding: 12px 14px;
  font-family: ${(p) => p.theme.font.mono};
  font-size: 12px;
  line-height: 1.6;
  max-height: 220px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-word;
`;

/** A line PortCloak wrote into the log itself, rather than one the export said. */
export const LogCommand = styled.div`
  color: ${(p) => p.theme.color.logCmd};
`;
