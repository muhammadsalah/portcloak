// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * What every frontend test gets before it runs.
 *
 * Only two things, and both are about the assertion vocabulary rather than
 * about the application: jest-dom's DOM matchers, and unmounting between tests
 * so one test's tree cannot be found by the next one's query.
 */
import "@testing-library/jest-dom/vitest";

import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(() => {
  cleanup();
});
