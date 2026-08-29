// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Every screen the app can be on, as a discriminated union.
 *
 * There is no URL and no history: PortCloak is a desktop window, and a route is
 * simply which page the shell is rendering plus what that page needs to know.
 * Keeping it a union is what makes `navigate({ name: "inspect" })` fail to
 * compile without the three identifiers the inspector cannot open a snapshot
 * without.
 */

export type Route =
  | { name: "capture" }
  | { name: "snapshots" }
  | { name: "restore"; snapshotId?: string }
  | { name: "activity" }
  | { name: "environments"; select?: string; add?: boolean }
  | { name: "storage"; select?: string; add?: boolean }
  | { name: "browse"; storage: string }
  | { name: "keys" }
  | { name: "audit" }
  | { name: "settings" }
  | {
      name: "inspect";
      storage: string;
      bundleKey: string;
      snapshotId: string;
      tab?: string;
    };

export type RouteName = Route["name"];

/**
 * Which navigation item a route lights up.
 *
 * Inspecting a snapshot is reached from the snapshot list, restoring one from
 * the same list, and browsing a storage from the storage editor, so none of
 * them has an item of its own — they highlight the one they came from rather
 * than leaving the rail with nothing selected.
 */
export function routeKey(route: Route): string {
  switch (route.name) {
    case "inspect":
    case "restore":
      return "snapshots";
    case "browse":
      return "storage";
    default:
      return route.name;
  }
}
