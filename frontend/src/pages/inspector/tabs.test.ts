// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The eight views over an opened snapshot, in the order they are read in.
 *
 * The order is the reading order, not an arbitrary list: overview first because
 * it says what the snapshot is, secret ledger last because it is the one view
 * that reveals rather than describes. The route carries a tab as a bare string,
 * so this list is also what an inbound `tab` is checked against.
 */
import { describe, expect, it } from "vitest";

import { tabs, type Tab } from "./tabs";

describe("the tabs", () => {
  it("are the eight views, in reading order", () => {
    expect(tabs.map((tab) => tab.key)).toEqual([
      "overview",
      "users",
      "clients",
      "keys",
      "federations",
      "flows",
      "deps",
      "secrets",
    ]);
  });

  it("end on the secret ledger, which is the only one that reveals", () => {
    expect(tabs[tabs.length - 1]).toEqual({ key: "secrets", label: "Secret ledger" });
  });

  it("give every view a label that is not its key", () => {
    // "Auth flows", not "flows"; "External deps", not "deps".
    for (const tab of tabs) {
      expect(tab.label).toBeTruthy();
      expect(tab.label).not.toBe(tab.key);
    }
  });

  it("name each key exactly once", () => {
    const keys: Tab[] = tabs.map((tab) => tab.key);
    expect(new Set(keys).size).toBe(keys.length);
  });
});
