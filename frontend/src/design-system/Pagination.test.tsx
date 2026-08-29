// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Which page numbers are drawn, and what pressing one does.
 *
 * A realm with a quarter of a million users is ten thousand pages at the
 * smallest size, so the interesting part is what is left out: the row has to
 * stay the same width whether there are three pages or ten thousand, while
 * still reaching the first, the last and the neighbours of wherever the reader
 * is.
 */
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { renderApp } from "@/test/render";
import { Pagination, pageNumbers } from "./Pagination";

describe("pageNumbers", () => {
  it("draws every page while they still fit", () => {
    expect(pageNumbers(1, 7)).toEqual([1, 2, 3, 4, 5, 6, 7]);
  });

  it("keeps the first, the last and the current one's neighbours", () => {
    expect(pageNumbers(50, 400)).toEqual([1, null, 49, 50, 51, null, 400]);
  });

  it("draws a lone missing page rather than eliding it", () => {
    expect(pageNumbers(4, 20)).toEqual([1, 2, 3, 4, 5, null, 20]);
  });

  // The row is bounded, not constant: it is narrower at the ends, where a
  // neighbour falls off. What matters is that it never grows with the total,
  // which is the whole reason for eliding at all.
  it("never grows with the number of pages", () => {
    for (const pages of [8, 40, 400, 10_000]) {
      for (const current of [1, 2, Math.floor(pages / 2), pages - 1, pages]) {
        expect(pageNumbers(current, pages).length).toBeLessThanOrEqual(7);
      }
    }
  });
});

describe("Pagination", () => {
  const props = { total: 253, offset: 75, limit: 25 };

  it("marks the page the reader is on", () => {
    renderApp(<Pagination {...props} onChange={() => {}} />);
    expect(screen.getByRole("button", { name: "Page 4" })).toHaveAttribute("aria-current", "page");
  });

  it("moves to the page that was pressed", async () => {
    const onChange = vi.fn();
    renderApp(<Pagination {...props} onChange={onChange} />);
    await userEvent.click(screen.getByRole("button", { name: "Page 5" }));
    expect(onChange).toHaveBeenCalledWith({ offset: 100, limit: 25 });
  });

  it("cannot step back from the first page or on from the last", () => {
    const { unmount } = renderApp(
      <Pagination total={253} offset={0} limit={25} onChange={() => {}} />,
    );
    expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
    unmount();

    renderApp(<Pagination total={253} offset={250} limit={25} onChange={() => {}} />);
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
  });

  // Resizing that sent the reader back to row one would lose their place in a
  // list they are halfway through reading.
  it("keeps the reader's place when the page size changes", async () => {
    const onChange = vi.fn();
    renderApp(<Pagination total={253} offset={75} limit={25} onChange={onChange} />);
    await userEvent.click(screen.getByRole("combobox", { name: "Rows per page" }));
    await userEvent.click(screen.getByRole("option", { name: "50" }));
    expect(onChange).toHaveBeenCalledWith({ offset: 50, limit: 50 });
  });
});
