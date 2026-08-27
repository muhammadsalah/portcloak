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
