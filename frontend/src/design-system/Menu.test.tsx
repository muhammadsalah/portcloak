// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Where the row menu is drawn, which is the part that broke.
 *
 * Every table in the app sits inside a box that scrolls sideways, and CSS
 * computes `overflow-y` to `auto` alongside an explicit `overflow-x: auto`. A
 * list positioned inside that box is therefore clipped at its edge whatever it
 * is stacked above — the menu on the last row of a table opened, and was cut
 * in half by the card it was in.
 *
 * So what is asserted here is that the list is not a descendant of the box it
 * would be clipped by, and that moving it out did not cost the two things that
 * depend on it being nearby: choosing an item, and clicking away to dismiss.
 */
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { renderApp } from "../test/render";
import { Menu } from "./Menu";
import { TableScroll } from "./Table";

/** The component as a table uses it: inside the box that does the clipping. */
function Harness({ onOpen = () => {} }: { onOpen?: () => void } = {}) {
  return (
    <TableScroll data-testid="scroll">
      <Menu items={[{ label: "Open", onSelect: onOpen }]} />
    </TableScroll>
  );
}

describe("Menu", () => {
  it("draws the list outside the box that would clip it", async () => {
    renderApp(<Harness />);
    await userEvent.click(screen.getByRole("button", { name: "More actions" }));

    const list = screen.getByRole("menu");
    expect(screen.getByTestId("scroll")).not.toContainElement(list);
    expect(list.parentElement).toBe(document.body);
  });

  it("still runs the item that was chosen", async () => {
    const onOpen = vi.fn();
    renderApp(<Harness onOpen={onOpen} />);
    await userEvent.click(screen.getByRole("button", { name: "More actions" }));
    await userEvent.click(screen.getByRole("menuitem", { name: "Open" }));

    expect(onOpen).toHaveBeenCalledOnce();
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("still closes when the click lands somewhere else", async () => {
    renderApp(<Harness />);
    await userEvent.click(screen.getByRole("button", { name: "More actions" }));
    expect(screen.getByRole("menu")).toBeInTheDocument();

    await userEvent.click(document.body);
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });
});
