// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The restore wizard's rules.
 *
 * A restore writes into a live identity system, so two of these are the last
 * thing standing in front of an irreversible action: the key is demanded before
 * the bundle is downloaded, and an overwrite is not armed until the realm's
 * name has been typed out. The third is the one that is deliberately *not* a
 * gate — missing dependencies inform, they do not block, because the operator
 * manages those environments and PortCloak does not.
 */
import { describe, expect, it } from "vitest";

import type { LibraryEntry, Plan } from "../../api";
import type { SnapshotKey } from "../../components/SnapshotKeyFields";
import { advanceable, steps } from "./wizard";

function entry(patch: Partial<LibraryEntry> = {}): LibraryEntry {
  return {
    snapshotId: "snap-1",
    realm: "acme",
    createdAt: "2026-03-04T09:07:05Z",
    storage: "archive",
    bundleKey: "2026/03/acme.pck.age",
    bytes: 4096,
    users: 12,
    clients: 3,
    verdict: "complete",
    encrypted: true,
    secretCount: 4,
    dependencyCount: 0,
    tokenContinuity: true,
    metadataReadable: true,
    ...patch,
  };
}

function plan(patch: Partial<Plan> = {}): Plan {
  return {
    // The rules read `blocked`, `blockedNote` and `confirmationRequired` and
    // nothing else; the report and the dry run are what the *steps* render.
    preconditions: {} as Plan["preconditions"],
    dryRun: {} as Plan["dryRun"],
    blocked: false,
    confirmationRequired: false,
    ...patch,
  };
}

const noKey: SnapshotKey = { passphrase: "", identities: [] };

function ask(patch: Partial<Parameters<typeof advanceable>[0]> = {}) {
  return advanceable({
    step: "snapshot",
    snapshot: entry(),
    key: noKey,
    storedKeys: 0,
    environment: "staging",
    plan: plan(),
    confirmRealm: "",
    ...patch,
  });
}

describe("the wizard's shape", () => {
  it("is five steps, in the order of the guarantees", () => {
    // Opening the snapshot precedes contacting a destination, and the dry run
    // precedes writing. Reordering this list reorders those promises.
    expect(steps.map((step) => step.key)).toEqual([
      "snapshot",
      "destination",
      "preconditions",
      "strategy",
      "apply",
    ]);
  });
});

describe("the snapshot step", () => {
  it("needs a snapshot", () => {
    const verdict = ask({ snapshot: undefined });
    expect(verdict.ok).toBe(false);
    expect(verdict.reason).toBe("Choose a snapshot.");
  });

  it("advances once one is selected", () => {
    expect(ask()).toEqual({ ok: true });
  });
});

describe("the destination step", () => {
  it("needs a destination environment", () => {
    const verdict = ask({ step: "destination", environment: "" });
    expect(verdict.ok).toBe(false);
    expect(verdict.reason).toBe("Choose a destination environment.");
  });

  it("demands a key for an encrypted snapshot when nothing is stored", () => {
    // Asked for here rather than discovered at Apply: the next step downloads
    // the bundle, and would otherwise do it only to fail on it.
    const verdict = ask({ step: "destination", snapshot: entry({ encrypted: true }) });
    expect(verdict.ok).toBe(false);
    expect(verdict.reason).toBe("Enter the key this snapshot was sealed with.");
  });

  it("accepts a passphrase, and accepts an age identity instead", () => {
    expect(
      ask({
        step: "destination",
        key: { passphrase: "correct horse battery staple", identities: [] },
      }).ok,
    ).toBe(true);
    expect(
      ask({
        step: "destination",
        key: { passphrase: "", identities: ["AGE-SECRET-KEY-1EXAMPLE"] },
      }).ok,
    ).toBe(true);
  });

  it("does not ask again when PortCloak already holds a candidate key", () => {
    // Demanding a key the operator has already entrusted to PortCloak is the
    // prompt that teaches people to turn encryption off.
    expect(ask({ step: "destination", storedKeys: 1 }).ok).toBe(true);
  });

  it("asks for nothing when the snapshot is not encrypted", () => {
    expect(ask({ step: "destination", snapshot: entry({ encrypted: false }) }).ok).toBe(true);
  });
});

describe("the preconditions step", () => {
  it("informs rather than blocks — a missing dependency is the operator's call", () => {
    expect(ask({ step: "preconditions", plan: plan({ blocked: false }) }).ok).toBe(true);
  });

  it("blocks, with the engine's own words, when the plan says it must", () => {
    const verdict = ask({
      step: "preconditions",
      plan: plan({ blocked: true, blockedNote: "The destination is running a newer Keycloak." }),
    });
    expect(verdict.ok).toBe(false);
    expect(verdict.reason).toBe("The destination is running a newer Keycloak.");
  });

  it("does not block when there is no plan at all", () => {
    // Reachable only if the dry run were skipped: RestorePage computes the plan
    // on the way into this step. The rule is nonetheless "block on a plan that
    // says blocked", not "block on the absence of one" — an absent plan carries
    // no reason to show, and a Next disabled with nothing beside it is the
    // failure this wizard is written to avoid.
    expect(ask({ step: "preconditions", plan: undefined })).toEqual({
      ok: true,
      reason: undefined,
    });
  });
});

describe("the strategy step", () => {
  it("arms an overwrite only once the realm name has been typed out", () => {
    const overwrite = plan({ confirmationRequired: true });

    expect(ask({ step: "strategy", plan: overwrite, confirmRealm: "" }).ok).toBe(false);
    expect(ask({ step: "strategy", plan: overwrite, confirmRealm: "acm" }).ok).toBe(false);
    expect(ask({ step: "strategy", plan: overwrite, confirmRealm: "ACME" }).ok).toBe(false);
    expect(ask({ step: "strategy", plan: overwrite, confirmRealm: "acme" }).ok).toBe(true);
  });

  it("says what the empty confirmation field is for", () => {
    const verdict = ask({
      step: "strategy",
      plan: plan({ confirmationRequired: true }),
      confirmRealm: "",
    });
    expect(verdict.reason).toBe("Type the realm name to confirm an overwrite.");
  });

  it("asks for no confirmation when the strategy does not destroy anything", () => {
    expect(
      ask({ step: "strategy", plan: plan({ confirmationRequired: false }), confirmRealm: "" }).ok,
    ).toBe(true);
  });
});

describe("the apply step", () => {
  it("gates nothing — the confirmation was the gate", () => {
    expect(ask({ step: "apply", snapshot: undefined, environment: "" })).toEqual({ ok: true });
  });
});
