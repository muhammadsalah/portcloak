// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The navigation rail is the only orientation this window offers — there is no
 * URL and no back button. A route that lights up nothing leaves an operator
 * three clicks deep with no indication of where they are, which is why the two
 * routes reached from inside another screen borrow its highlight rather than
 * having one of their own.
 */
import { describe, expect, it } from "vitest";

import { routeKey, type Route } from "./routes";

describe("routeKey", () => {
  it("is the route's own name for every screen the rail lists", () => {
    const listed: Route[] = [
      { name: "capture" },
      { name: "library" },
      { name: "activity" },
      { name: "environments" },
      { name: "storage" },
      { name: "keys" },
      { name: "audit" },
      { name: "settings" },
    ];

    for (const route of listed) {
      expect(routeKey(route)).toBe(route.name);
    }
  });

  it("lights up Snapshots while one is being restored", () => {
    // A restore starts from a row on the snapshot list and has no rail item of
    // its own. A rail with nothing selected reads as being lost.
    expect(routeKey({ name: "restore" })).toBe("library");
    expect(routeKey({ name: "restore", snapshotId: "snap-1" })).toBe("library");
  });

  it("lights up Library while a snapshot is being inspected", () => {
    // The inspector is reached from the snapshot list and has no rail item.
    expect(
      routeKey({
        name: "inspect",
        storage: "archive",
        bundleKey: "2026/03/acme.pck",
        snapshotId: "snap-1",
      }),
    ).toBe("library");
  });

  it("lights up Storage while a storage is being browsed", () => {
    expect(routeKey({ name: "browse", storage: "archive" })).toBe("storage");
  });

  it("ignores the parameters a route carries", () => {
    // Selecting a storage and browsing one are different routes; the rail
    // cannot start distinguishing them by their payload.
    expect(routeKey({ name: "storage", select: "archive" })).toBe("storage");
    expect(
      routeKey({
        name: "inspect",
        storage: "archive",
        bundleKey: "k",
        snapshotId: "s",
        tab: "secrets",
      }),
    ).toBe("library");
  });
});
