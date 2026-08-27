// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The button, in the five weights the app uses it at.
 *
 * `variant` is a prop rather than a class because it is the one thing about a
 * button that carries meaning: `danger-solid` is reserved for the confirm
 * button of something that cannot be undone, and having it in the type is what
 * keeps that reservation visible.
 */
import styled, { css } from "styled-components";

export type ButtonVariant = "secondary" | "primary" | "plain" | "danger" | "danger-solid";

const variants = {
  secondary: css`
    background: ${(p) => p.theme.color.surface};
    color: ${(p) => p.theme.color.primary};
    border-color: ${(p) => p.theme.color.primary};

    &:hover:not(:disabled) {
      background: ${(p) => p.theme.color.primarySoft};
    }
  `,
  primary: css`
    background: ${(p) => p.theme.color.primary};
    color: #fff;
    border-color: ${(p) => p.theme.color.primary};

    &:hover:not(:disabled) {
      background: ${(p) => p.theme.color.primaryHover};
    }
  `,
  plain: css`
    background: transparent;
    color: ${(p) => p.theme.color.primary};
    border-color: transparent;

    &:hover:not(:disabled) {
      background: ${(p) => p.theme.color.primarySoft};
    }
  `,
  danger: css`
    background: ${(p) => p.theme.color.surface};
    color: ${(p) => p.theme.color.danger};
    border-color: ${(p) => p.theme.color.danger};

    &:hover:not(:disabled) {
      background: ${(p) => p.theme.color.dangerBg};
    }
  `,
  "danger-solid": css`
    background: ${(p) => p.theme.color.danger};
    color: #fff;
    border-color: ${(p) => p.theme.color.danger};

    &:hover:not(:disabled) {
      background: ${(p) => p.theme.color.danger};
    }
  `,
} satisfies Record<ButtonVariant, ReturnType<typeof css>>;

export const Button = styled.button<{ $variant?: ButtonVariant }>`
  font-family: ${(p) => p.theme.font.body};
  font-size: 14px;
  border-radius: ${(p) => p.theme.radius.base};
  padding: 6px 16px;
  border: 1px solid transparent;
  cursor: pointer;
  line-height: 1.5;
  white-space: nowrap;

  ${(p) => variants[p.$variant ?? "secondary"]}

  &:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
`;

/**
 * The × on a chip. Not a Button: it lives inside running text at the chip's own
 * colour, and any of the variants above would give it a box of its own.
 */
export const IconButton = styled.button`
  border: none;
  background: none;
  padding: 0;
  color: inherit;
  font-size: 13px;
  line-height: 1;
  cursor: pointer;
`;
