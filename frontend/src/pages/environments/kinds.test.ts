// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * How the four kinds of environment are named.
 *
 * The capture wizard has its own copy of this in `capture/draft.ts`, because a
 * wizard step should not import from another page. That duplication is only
 * safe while the two agree, so both are asserted against the same words.
 */
import { describe, expect, it } from "vitest";

import { kindLabel as captureKindLabel } from "@/pages/snapshots/capture/draft";
import { kindLabel, kinds } from "./kinds";

describe("the kinds", () => {
  it("are the four the engine supports, in the order the editor offers them", () => {
    expect(kinds.map((kind) => kind.value)).toEqual(["local", "ssh", "docker", "kubernetes"]);
  });

  it("name Kubernetes and OpenShift together, because one adapter serves both", () => {
    expect(kindLabel("kubernetes")).toBe("Kubernetes / OpenShift");
  });

  it("show an unknown kind rather than hiding it", () => {
    expect(kindLabel("podman")).toBe("podman");
  });
});

describe("the capture wizard's copy", () => {
  it("agrees with this one, kind for kind", () => {
    for (const kind of kinds) {
      expect(captureKindLabel(kind.value)).toBe(kindLabel(kind.value));
    }
  });
});
