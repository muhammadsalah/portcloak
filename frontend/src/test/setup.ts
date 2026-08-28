// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * What every frontend test gets before it runs.
 *
 * Two of these are about the assertion vocabulary rather than about the
 * application: jest-dom's DOM matchers, and unmounting between tests so one
 * test's tree cannot be found by the next one's query.
 *
 * The third fills a hole in jsdom. It implements no layout, so it has no
 * scrollIntoView — a component that keeps a highlighted row in view throws in a
 * test and works in the app. Stubbing it here keeps that defensive check out of
 * the component, where it would read as a real possibility rather than as a
 * test environment's limitation.
 */
import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

afterEach(() => {
  cleanup();
});
