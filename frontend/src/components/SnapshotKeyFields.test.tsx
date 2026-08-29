// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * How PortCloak asks for a snapshot's decryption key.
 *
 * Two screens ask it — the inspector on its way in, the restore wizard on the
 * step that says decryption happens first — and the restore wizard gates on
 * `hasKey`. A passphrase that reaches the field but not the value, or an
 * identity that arrives with the trailing newline a paste brings, is a snapshot
 * that will not open at the point of no return.
 */
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { renderApp } from "@/test/render";
import { hasKey, noKey, SnapshotKeyFields, type SnapshotKey } from "./SnapshotKeyFields";

describe("noKey and hasKey", () => {
  it("start empty", () => {
    expect(noKey()).toEqual({ passphrase: "", identities: [] });
    expect(hasKey(noKey())).toBe(false);
  });

  it("count a passphrase, and count an identity, as a key", () => {
    expect(hasKey({ passphrase: "correct horse", identities: [] })).toBe(true);
    expect(hasKey({ passphrase: "", identities: ["AGE-SECRET-KEY-1EXAMPLE"] })).toBe(true);
  });
});

/** Renders the fields over a value the test can read back. */
function ask(initial: SnapshotKey = noKey()) {
  const onChange = vi.fn<(key: SnapshotKey) => void>();
  renderApp(<SnapshotKeyFields value={initial} onChange={onChange} />);
  return onChange;
}

describe("the passphrase field", () => {
  it("is masked", () => {
    // It is a secret being typed in front of whoever is in the room.
    ask();
    expect(screen.getByPlaceholderText(/sealed with/)).toHaveAttribute("type", "password");
  });

  it("reports what was typed, keeping any identity already entered", async () => {
    const onChange = ask({ passphrase: "", identities: ["AGE-SECRET-KEY-1EXAMPLE"] });

    await userEvent.type(screen.getByPlaceholderText(/sealed with/), "a");

    expect(onChange).toHaveBeenCalledWith({
      passphrase: "a",
      identities: ["AGE-SECRET-KEY-1EXAMPLE"],
    });
  });
});

describe("the identity field", () => {
  it("shows the identity already held, rather than starting blank", () => {
    ask({ passphrase: "", identities: ["AGE-SECRET-KEY-1EXAMPLE"] });
    expect(screen.getByPlaceholderText("AGE-SECRET-KEY-1…")).toHaveValue("AGE-SECRET-KEY-1EXAMPLE");
  });

  it("trims what was pasted", async () => {
    // A key copied out of a file arrives with a trailing newline, and age does
    // not accept it.
    const onChange = ask();

    await userEvent.click(screen.getByPlaceholderText("AGE-SECRET-KEY-1…"));
    await userEvent.paste("  AGE-SECRET-KEY-1EXAMPLE\n");

    expect(onChange).toHaveBeenCalledWith({
      passphrase: "",
      identities: ["AGE-SECRET-KEY-1EXAMPLE"],
    });
  });

  it("clears back to no identity when emptied, rather than holding one blank string", async () => {
    // `hasKey` counts identities by length, so an empty string left in the list
    // would report a key that cannot open anything.
    const onChange = ask({ passphrase: "", identities: ["AGE-SECRET-KEY-1EXAMPLE"] });

    await userEvent.clear(screen.getByPlaceholderText("AGE-SECRET-KEY-1…"));

    expect(onChange).toHaveBeenCalledWith({ passphrase: "", identities: [] });
    expect(hasKey({ passphrase: "", identities: [] })).toBe(false);
  });

  it("keeps a passphrase already entered", async () => {
    const onChange = ask({ passphrase: "correct horse", identities: [] });

    await userEvent.click(screen.getByPlaceholderText("AGE-SECRET-KEY-1…"));
    await userEvent.paste("AGE-SECRET-KEY-1EXAMPLE");

    expect(onChange).toHaveBeenCalledWith({
      passphrase: "correct horse",
      identities: ["AGE-SECRET-KEY-1EXAMPLE"],
    });
  });
});

describe("both screens ask the same question", () => {
  it("offers a passphrase and an age identity, in that order", () => {
    // An operator who learns the shape of this on the inspector should
    // recognise it on the restore wizard.
    ask();
    expect(screen.getByText("Passphrase")).toBeInTheDocument();
    expect(screen.getByText("…or an age private key")).toBeInTheDocument();
  });
});
