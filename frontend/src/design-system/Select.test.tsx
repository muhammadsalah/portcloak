// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The dropdown's behaviour, which is the part a native `<select>` used to
 * provide and now has to be maintained.
 *
 * The styling is why this component exists, but the styling is not what breaks.
 * What breaks is a keyboard path nobody tried, a list that stays open over the
 * rest of the screen, or focus left somewhere the operator cannot get out of —
 * so those are what is asserted here, one per case, in the order an operator
 * would meet them.
 */
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { renderApp } from "@/test/render";
import { Select } from "./Select";

const realms = [
  { value: "", label: "Realm: all" },
  { value: "corp-a", label: "corp-a" },
  { value: "corp-b", label: "corp-b" },
];

/** The component as a page uses it: the value lives outside and comes back in. */
function Harness({ onChange }: { onChange?: (value: string) => void } = {}) {
  const [value, setValue] = useState("");
  return (
    <Select
      aria-label="Realm"
      value={value}
      options={realms}
      onChange={(next) => {
        setValue(next);
        onChange?.(next);
      }}
    />
  );
}

function trigger() {
  return screen.getByRole("combobox", { name: "Realm" });
}

describe("Select", () => {
  it("shows the selected option and no list until it is opened", () => {
    renderApp(<Harness />);
    expect(trigger()).toHaveTextContent("Realm: all");
    expect(screen.queryByRole("listbox")).toBeNull();
    expect(trigger()).toHaveAttribute("aria-expanded", "false");
  });

  it("opens on click and reports the selection back as a value", async () => {
    const user = userEvent.setup();
    const changed = vi.fn();
    renderApp(<Harness onChange={changed} />);

    await user.click(trigger());
    expect(screen.getByRole("listbox")).toBeInTheDocument();

    await user.click(screen.getByRole("option", { name: "corp-b" }));
    expect(changed).toHaveBeenCalledWith("corp-b");
    expect(trigger()).toHaveTextContent("corp-b");
    // The list closes on choosing, or it sits over whatever the choice revealed.
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("marks the selected option in the accessibility tree, not only in colour", async () => {
    const user = userEvent.setup();
    renderApp(<Harness />);

    await user.click(trigger());
    await user.click(screen.getByRole("option", { name: "corp-a" }));
    await user.click(trigger());

    expect(screen.getByRole("option", { name: "corp-a" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("option", { name: "corp-b" })).toHaveAttribute(
      "aria-selected",
      "false",
    );
  });

  it("is operable from the keyboard alone", async () => {
    const user = userEvent.setup();
    const changed = vi.fn();
    renderApp(<Harness onChange={changed} />);

    await user.tab();
    expect(trigger()).toHaveFocus();

    await user.keyboard("{ArrowDown}");
    expect(screen.getByRole("listbox")).toBeInTheDocument();

    await user.keyboard("{ArrowDown}{Enter}");
    expect(changed).toHaveBeenCalledWith("corp-a");
  });

  it("opens on the value it already holds, so opening and confirming changes nothing", async () => {
    const user = userEvent.setup();
    const changed = vi.fn();
    renderApp(<Harness onChange={changed} />);

    await user.click(trigger());
    await user.click(screen.getByRole("option", { name: "corp-b" }));
    changed.mockClear();

    await user.keyboard("{Enter}{Enter}");
    expect(changed).toHaveBeenCalledWith("corp-b");
    expect(trigger()).toHaveTextContent("corp-b");
  });

  it("jumps to an option by typing its first letters", async () => {
    const user = userEvent.setup();
    renderApp(<Harness />);

    await user.click(trigger());
    await user.keyboard("corp-b");
    await user.keyboard("{Enter}");

    expect(trigger()).toHaveTextContent("corp-b");
  });

  it("closes on Escape and leaves focus on the trigger", async () => {
    const user = userEvent.setup();
    renderApp(<Harness />);

    await user.click(trigger());
    await user.keyboard("{Escape}");

    expect(screen.queryByRole("listbox")).toBeNull();
    // Focus left in the list that no longer exists is a keyboard dead end.
    expect(trigger()).toHaveFocus();
  });

  it("closes when something else on the page is clicked", async () => {
    const user = userEvent.setup();
    renderApp(
      <div>
        <Harness />
        <button type="button">Elsewhere</button>
      </div>,
    );

    await user.click(trigger());
    await user.click(screen.getByRole("button", { name: "Elsewhere" }));

    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("does not open when disabled", async () => {
    const user = userEvent.setup();
    renderApp(<Select aria-label="Realm" value="" options={realms} onChange={vi.fn()} disabled />);

    await user.click(trigger());
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("shows the placeholder when the value matches no option", () => {
    renderApp(
      <Select
        aria-label="Realm"
        value="gone"
        options={realms}
        onChange={vi.fn()}
        placeholder="Pick a realm"
      />,
    );
    expect(trigger()).toHaveTextContent("Pick a realm");
  });
});
