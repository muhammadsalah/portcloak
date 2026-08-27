// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Every size and colour of text the app uses, named for what it says rather
 * than how it looks.
 */
import styled, { css } from "styled-components";

/** The name of the screen. One per page, at the top. */
export const PageTitle = styled.h1`
  font-size: 24px;
  margin-bottom: 4px;
`;

/** The sentence under the title that says what the screen is for. */
export const PageSubtitle = styled.div<{ $mono?: boolean }>`
  color: ${(p) => p.theme.color.textSecondary};
  font-size: 13px;
  margin-bottom: 20px;
  ${(p) => p.$mono && css`font-family: ${p.theme.font.mono};`}
`;

/** Title on the left, the screen's primary action on the right. */
export const PageHead = styled.div`
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
`;

/** Secondary text: present, but not what the eye should land on. */
export const Muted = styled.span`
  color: ${(p) => p.theme.color.textSecondary};
`;

export const Small = styled.span`
  font-size: 12px;
`;

/** Secondary and small together, which is most supporting text in the app. */
export const Hint = styled.div`
  font-size: 12px;
  color: ${(p) => p.theme.color.textSecondary};
`;

/** Machine text — a path, a key, a digest — set so it can be read character by character. */
export const Mono = styled.span`
  font-family: ${(p) => p.theme.font.mono};
  font-size: 12px;
`;

/** A heading over a group of filters or a short list. */
export const GroupTitle = styled.div`
  font-size: 11px;
  letter-spacing: 0.5px;
  text-transform: uppercase;
  color: ${(p) => p.theme.color.textSecondary};
  margin-bottom: 6px;
`;

/** The heading inside a wizard step or a panel body. */
export const SectionTitle = styled.h2`
  font-size: 17px;
  margin-bottom: 12px;
`;

/** A label that names a value beside it, without being a form label. */
export const Strong = styled.span`
  font-weight: 500;
`;

/**
 * A filesystem path shown as the thing itself: monospaced, boxed, and free to
 * wrap anywhere, because a home folder or a container reference has no spaces
 * to break at and would otherwise run past the card.
 */
export const PathBox = styled.div`
  font-family: ${(p) => p.theme.font.mono};
  font-size: 12px;
  background: ${(p) => p.theme.color.surfaceSubtle};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.base};
  padding: 8px 10px;
  overflow-wrap: anywhere;
`;

/** A secret, a public key, a digest — shown in full, breaking anywhere. */
export const RevealValue = styled.div`
  font-family: ${(p) => p.theme.font.mono};
  font-size: 13px;
  background: ${(p) => p.theme.color.surfaceSubtle};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.base};
  padding: 10px 12px;
  word-break: break-all;
`;

/** A short list of bullet points inside a panel or a modal. */
export const BulletList = styled.ul`
  font-size: 12px;
  margin: 6px 0 0;
  padding-left: 18px;
`;

/** Anything that behaves as a link: navigation, an inline action, a disclosure. */
export const Link = styled.a<{ $tone?: "default" | "muted" | "danger" }>`
  color: ${(p) =>
    p.$tone === "danger"
      ? p.theme.color.danger
      : p.$tone === "muted"
        ? p.theme.color.textSecondary
        : p.theme.color.primary};
  text-decoration: none;
  cursor: pointer;

  &:hover {
    text-decoration: underline;
  }
`;
