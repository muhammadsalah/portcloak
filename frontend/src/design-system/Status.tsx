// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The things that say how something is: badges, dots, notices, spinners.
 *
 * All five tones come from one place so that "warn" means the same amber
 * everywhere it appears, and a screen cannot invent a sixth.
 */
import type { ReactNode } from "react";
import styled, { css } from "styled-components";

import type { NoticeTone, Tone } from "./theme";

/* ── Badge ─────────────────────────────────────────────────────────────── */

const badgeTones = {
  ok: css`
    background: ${(p) => p.theme.color.successBg};
    color: ${(p) => p.theme.color.success};
    border-color: ${(p) => p.theme.color.successBorder};
  `,
  warn: css`
    background: ${(p) => p.theme.color.warningBg};
    color: ${(p) => p.theme.color.warningBadgeText};
    border-color: ${(p) => p.theme.color.warningBorder};
  `,
  danger: css`
    background: ${(p) => p.theme.color.dangerBg};
    color: ${(p) => p.theme.color.danger};
    border-color: ${(p) => p.theme.color.dangerBorder};
  `,
  neutral: css`
    background: ${(p) => p.theme.color.page};
    color: ${(p) => p.theme.color.textSecondary};
    border-color: ${(p) => p.theme.color.border};
  `,
  info: css`
    background: ${(p) => p.theme.color.infoBg};
    color: ${(p) => p.theme.color.infoBadgeText};
    border-color: ${(p) => p.theme.color.infoBorder};
  `,
} satisfies Record<Tone, ReturnType<typeof css>>;

export const Badge = styled.span<{ $tone: Tone }>`
  display: inline-block;
  padding: 1px 9px;
  border-radius: ${(p) => p.theme.radius.pill};
  font-size: 12px;
  line-height: 18px;
  white-space: nowrap;
  border: 1px solid;
  ${(p) => badgeTones[p.$tone]}
`;

/* ── Dot ───────────────────────────────────────────────────────────────── */

const dotColours = {
  ok: (p: { theme: { color: { success: string } } }) => p.theme.color.success,
  warn: (p: { theme: { color: { warning: string } } }) => p.theme.color.warning,
  danger: (p: { theme: { color: { danger: string } } }) => p.theme.color.danger,
  neutral: (p: { theme: { color: { textMuted: string } } }) => p.theme.color.textMuted,
  info: (p: { theme: { color: { primary: string } } }) => p.theme.color.primary,
};

/** One outcome, reduced to a colour, at the head of a row. */
export const Dot = styled.span<{ $tone: Tone }>`
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 6px;
  flex: none;
  background: ${(p) => dotColours[p.$tone](p)};
`;

/* ── Notice ────────────────────────────────────────────────────────────── */

const noticeTones = {
  ok: css`
    background: ${(p) => p.theme.color.successBg};
    border-color: ${(p) => p.theme.color.successBorder};
    color: ${(p) => p.theme.color.successText};
  `,
  info: css`
    background: ${(p) => p.theme.color.infoBg};
    border-color: ${(p) => p.theme.color.infoBorder};
    color: ${(p) => p.theme.color.infoText};
  `,
  warn: css`
    background: ${(p) => p.theme.color.warningBg};
    border-color: ${(p) => p.theme.color.warningBorder};
    color: ${(p) => p.theme.color.warningText};
  `,
  danger: css`
    background: ${(p) => p.theme.color.dangerBg};
    border-color: ${(p) => p.theme.color.dangerBorder};
    color: ${(p) => p.theme.color.dangerText};
  `,
} satisfies Record<NoticeTone, ReturnType<typeof css>>;

export const NoticeBox = styled.div<{ $tone: NoticeTone }>`
  border-radius: ${(p) => p.theme.radius.base};
  padding: 12px 16px;
  margin-bottom: 16px;
  border: 1px solid;
  font-size: 13px;
  ${(p) => noticeTones[p.$tone]}
`;

export const NoticeTitle = styled.div`
  font-weight: 600;
  margin-bottom: 2px;
`;

/**
 * A message that carries newlines is several statements, not one paragraph.
 *
 * The engine reports a rejected edit as one problem per line, because a
 * configuration can be wrong in more than one place at a time and fixing them
 * one launch at a time is the thing the validator exists to avoid. Rendered
 * into a single text node those newlines collapse into spaces and the problems
 * run together into a sentence that reads like one long complaint.
 */
export function Lines({ text }: { text: string }) {
  const parts = text.split("\n").filter((line) => line.trim() !== "");
  if (parts.length <= 1) return <div>{text}</div>;
  return (
    <>
      {parts.map((line, i) => (
        <div key={i}>{line}</div>
      ))}
    </>
  );
}

export function Notice({
  tone,
  title,
  body,
  children,
}: {
  tone: NoticeTone;
  title: ReactNode;
  body?: string;
  children?: ReactNode;
}) {
  return (
    <NoticeBox $tone={tone}>
      <NoticeTitle>{title}</NoticeTitle>
      {body ? <Lines text={body} /> : null}
      {children}
    </NoticeBox>
  );
}

/** Renders an engine failure with its hint, which is the actionable half. */
export function FailureNotice({
  failure,
}: {
  failure: { message: string; hint?: string; retryable?: boolean };
}) {
  return (
    <NoticeBox $tone={failure.retryable ? "warn" : "danger"}>
      <NoticeTitle>{failure.message}</NoticeTitle>
      {failure.hint ? <Lines text={failure.hint} /> : null}
    </NoticeBox>
  );
}

/* ── Spinner ───────────────────────────────────────────────────────────── */

const SpinnerRing = styled.span`
  width: 14px;
  height: 14px;
  border: 2px solid ${(p) => p.theme.color.border};
  border-top-color: ${(p) => p.theme.color.primary};
  border-radius: 50%;
  display: inline-block;
  animation: portcloak-spin 0.7s linear infinite;
`;

const SpinnerRow = styled.div`
  display: flex;
  gap: 8px;
  align-items: center;
  color: ${(p) => p.theme.color.textSecondary};
  font-size: 12px;
`;

export function Spinner({ children }: { children?: ReactNode }) {
  return (
    <SpinnerRow>
      <SpinnerRing />
      {children ?? "Working…"}
    </SpinnerRow>
  );
}

/* ── Chip ──────────────────────────────────────────────────────────────── */

/** A filter that is currently applied, with the way to remove it attached. */
export const Chip = styled.span`
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: ${(p) => p.theme.color.infoBg};
  border: 1px solid ${(p) => p.theme.color.infoBorder};
  border-radius: ${(p) => p.theme.radius.pill};
  padding: 1px 8px;
  font-size: 12px;
  color: ${(p) => p.theme.color.infoBadgeText};
`;

/* ── Step marks ────────────────────────────────────────────────────────── */

/** The numbered circle beside a wizard step, and beside a first-run card. */
export const StepMark = styled.span<{ $state?: "done" | "active" | "pending" }>`
  width: 20px;
  height: 20px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  border: 1px solid ${(p) => p.theme.color.borderInput};
  color: ${(p) => p.theme.color.textSecondary};
  flex: none;

  ${(p) =>
    p.$state === "done" &&
    css`
      background: ${p.theme.color.success};
      border-color: ${p.theme.color.success};
      color: #fff;
    `}
  ${(p) =>
    p.$state === "active" &&
    css`
      background: ${p.theme.color.primary};
      border-color: ${p.theme.color.primary};
      color: #fff;
    `}
`;

/** The step number on a first-run card: filled when it is this one's turn. */
export const StepNumber = styled.span<{ $pending?: boolean }>`
  width: 22px;
  height: 22px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  margin-right: 8px;
  background: ${(p) => (p.$pending ? p.theme.color.page : p.theme.color.primary)};
  color: ${(p) => (p.$pending ? p.theme.color.textSecondary : "#fff")};
  border: 1px solid ${(p) => (p.$pending ? p.theme.color.border : "transparent")};
`;
