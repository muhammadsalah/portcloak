// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The handful of boxes every screen is assembled from.
 *
 * These replace the utility classes the old stylesheet carried — `.row`,
 * `.stack`, `.grow`, `.right`, `.truncate`. Naming them as components is what
 * makes a page readable top to bottom: `<Row>` says what the element is for at
 * the point it is used, where `class="row"` only says so if you go and read
 * another file.
 */
import styled, { css } from "styled-components";

import { SelectRoot } from "./Select";

/** Things side by side on one line, evenly spaced and vertically centred. */
export const Row = styled.div<{ $gap?: number; $wrap?: boolean; $align?: string }>`
  display: flex;
  gap: ${(p) => p.$gap ?? 8}px;
  align-items: ${(p) => p.$align ?? "center"};
  ${(p) => p.$wrap && "flex-wrap: wrap;"}
`;

/** Things one under another. */
export const Stack = styled.div<{ $gap?: number }>`
  display: flex;
  flex-direction: column;
  gap: ${(p) => p.$gap ?? 4}px;
`;

/** Takes the space its siblings do not. */
export const Grow = styled.div`
  flex: 1;
  min-width: 0;
`;

/** Pushed to the far end of its row. */
export const Right = styled.div`
  margin-left: auto;
`;

/**
 * A one-line summary of something that can be arbitrarily long and has nowhere
 * to break: a filesystem path, a bucket and prefix, an install root.
 *
 * Truncated rather than wrapped. These appear in the 260px list column beside a
 * form, where a wrapped path takes four lines and pushes the row's actual
 * content — its name, whether it was reachable — out of view. The full value is
 * on the element's title, and the form next to it shows it in full anyway.
 *
 * `min-width: 0` is what makes this work inside a flex or grid parent: without
 * it a flex item refuses to shrink below its content's width, and the text
 * overflows instead of being clipped.
 */
export const truncate = css`
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
`;

export const Truncate = styled.div`
  ${truncate}
`;

/** Two equal columns of form fields. */
export const FieldRow = styled.div`
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
`;

/** A narrow list beside the thing it selects into. */
export const Split = styled.div`
  display: grid;
  grid-template-columns: 260px 1fr;
  gap: 16px;
  align-items: start;
`;

/** The content, with a narrower companion column beside it. */
export const SplitWide = styled.div`
  display: grid;
  grid-template-columns: 1fr 320px;
  gap: 16px;
  align-items: start;
`;

/** Filters and search above a table. */
export const Toolbar = styled.div`
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 16px;
  flex-wrap: wrap;

  // A filter sits to its content rather than filling the row, but not so
  // tightly that the label and the value crowd each other. The list it opens
  // starts at the trigger's width, so this is what sizes the menu too.
  ${SelectRoot} {
    width: auto;
    min-width: 220px;
  }
`;

/** The search field inside a toolbar, which grows but only so far. */
export const Search = styled.div`
  position: relative;
  flex: 1;
  max-width: 420px;
`;

/** A rule between two halves of a form. */
export const Divider = styled.hr`
  border: none;
  border-top: 1px solid ${(p) => p.theme.color.border};
  margin: 18px 0;
`;
