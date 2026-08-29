// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Paging for a table that is longer than a screen.
 *
 * Two arrows and a range told an operator where they were and let them move one
 * step in either direction, which is enough for the second page and useless for
 * the eleventh: reaching it meant pressing the same button ten times, and
 * nothing on screen said how many presses were left.
 *
 * So the pages are numbered and addressable, and the page size is the reader's.
 * A realm with a quarter of a million users is read differently from one with
 * two hundred, and the size that suits one is the wrong size for the other.
 */
import styled from "styled-components";

import { Select } from "./Select";

/** How many rows a page may hold. The first is the default. */
export const pageSizes = [25, 50, 100, 200] as const;

export function Pagination({
  total,
  offset,
  limit,
  onChange,
  disabled = false,
}: {
  total: number;
  offset: number;
  limit: number;
  /** Called with the new offset and limit together, because changing the size moves the offset. */
  onChange: (next: { offset: number; limit: number }) => void;
  disabled?: boolean;
}) {
  const pages = Math.max(1, Math.ceil(total / limit));
  const current = Math.min(pages, Math.floor(offset / limit) + 1);
  const go = (page: number) => onChange({ offset: (page - 1) * limit, limit });

  return (
    <Bar>
      <Group>
        <Label htmlFor="page-size">Rows</Label>
        <Select
          id="page-size"
          aria-label="Rows per page"
          value={String(limit)}
          disabled={disabled}
          options={pageSizes.map((n) => ({ value: String(n), label: String(n) }))}
          // The first row on screen stays on screen: resizing keeps the reader
          // where they were rather than sending them back to the top of a page
          // that is now a different page.
          onChange={(value) => {
            const next = Number(value);
            onChange({ offset: Math.floor(offset / next) * next, limit: next });
          }}
          style={{ width: 88 }}
        />
      </Group>

      <Group as="nav" aria-label="Pages">
        <Step
          type="button"
          onClick={() => go(current - 1)}
          disabled={disabled || current <= 1}
          aria-label="Previous page"
        >
          ‹
        </Step>
        {pageNumbers(current, pages).map((page, i) =>
          page === null ? (
            <Gap key={`gap-${i}`}>…</Gap>
          ) : (
            <Step
              key={page}
              type="button"
              $current={page === current}
              aria-current={page === current ? "page" : undefined}
              aria-label={`Page ${page}`}
              disabled={disabled}
              onClick={() => go(page)}
            >
              {page}
            </Step>
          ),
        )}
        <Step
          type="button"
          onClick={() => go(current + 1)}
          disabled={disabled || current >= pages}
          aria-label="Next page"
        >
          ›
        </Step>
      </Group>
    </Bar>
  );
}

/**
 * The pages worth drawing: the first, the last, the current and its neighbours,
 * with a null wherever a run was left out.
 *
 * Exported for the test. A realm with ten thousand users is four hundred pages,
 * and four hundred buttons is not navigation.
 */
export function pageNumbers(current: number, pages: number): (number | null)[] {
  if (pages <= 7) return Array.from({ length: pages }, (_, i) => i + 1);

  const near = [current - 1, current, current + 1].filter((n) => n > 1 && n < pages);
  const shown = [1, ...near, pages];

  const out: (number | null)[] = [];
  let previous = 0;
  for (const page of shown) {
    // A single missing page is drawn rather than elided: "1 … 3" hides one
    // number behind a mark that is wider than the number.
    if (page - previous === 2) out.push(page - 1);
    else if (page - previous > 2) out.push(null);
    out.push(page);
    previous = page;
  }
  return out;
}

const Bar = styled.div`
  display: flex;
  align-items: center;
  gap: 16px;
`;

const Group = styled.div`
  display: flex;
  align-items: center;
  gap: 4px;
`;

const Label = styled.label`
  font-size: 12px;
  color: ${(p) => p.theme.color.textSecondary};
  margin-right: 4px;
`;

const Step = styled.button<{ $current?: boolean }>`
  min-width: 32px;
  height: 32px;
  padding: 0 8px;
  border-radius: 6px;
  font-size: 13px;
  cursor: pointer;
  border: 1px solid ${(p) => (p.$current ? p.theme.color.primary : p.theme.color.border)};
  background: ${(p) => (p.$current ? p.theme.color.primary : p.theme.color.surface)};
  color: ${(p) => (p.$current ? p.theme.color.surface : p.theme.color.primary)};
  font-weight: ${(p) => (p.$current ? 600 : 400)};

  &:hover:not(:disabled) {
    background: ${(p) => (p.$current ? p.theme.color.primary : p.theme.color.surfaceSubtle)};
  }

  &:disabled {
    color: ${(p) => p.theme.color.textMuted};
    cursor: default;
  }

  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.primary};
    outline-offset: 1px;
  }
`;

const Gap = styled.span`
  min-width: 20px;
  text-align: center;
  color: ${(p) => p.theme.color.textMuted};
  font-size: 13px;
`;
