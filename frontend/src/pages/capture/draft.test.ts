// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The capture wizard's rules, one assertion each.
 *
 * `advanceable` is the only thing standing between an operator and a snapshot
 * that cannot be opened later, or one written to disk in the clear when they
 * thought otherwise. Two of the rules below exist because their absence is a
 * data-loss bug rather than an inconvenience: a passphrase that was never
 * entered seals nothing, and an unencrypted bundle carries unmasked client
 * secrets and private signing keys.
 *
 * The reason strings are asserted as well as the verdicts. A disabled Next with
 * no sentence beside it is the failure mode this whole function is written to
 * avoid.
 */
import { describe, expect, it } from "vitest";

import type { ProbeResult } from "../../api";
import { advanceable, kindLabel, type CaptureDraft } from "./draft";

function draft(patch: Partial<CaptureDraft> = {}): CaptureDraft {
  return {
    environment: "production",
    realmsNote: "",
    discoveredRealms: ["acme"],
    realmsDiscovered: true,
    realms: ["acme"],
    manualRealm: "",
    storage: "archive",
    usersMode: "different_files",
    usersPerFile: 1000,
    verify: true,
    detectDependencies: true,
    encrypt: true,
    encryptionMode: "passphrase",
    passphrase: "correct horse battery staple",
    recipients: [],
    rememberPassphraseAs: "",
    acknowledgedUnencrypted: false,
    ...patch,
  };
}

const passed: ProbeResult = { ok: true, facts: facts() };
const failed: ProbeResult = { ok: false, facts: facts() };

function facts(): ProbeResult["facts"] {
  return {
    kind: "local",
    reachable: true,
    hasTar: true,
    mode: "read-only",
    cloneCapable: false,
    adminReachable: true,
    ports: { http: 8080, https: 8443, management: 9000 },
    probedAt: "2026-03-04T09:07:05Z",
    checks: [],
    readOnlyNote: "",
  };
}

describe("the source step", () => {
  it("will not advance without an environment", () => {
    const verdict = advanceable("source", draft({ environment: "" }), passed);
    expect(verdict.ok).toBe(false);
    expect(verdict.reason).toBe("Choose an environment.");
  });

  it("will not advance on an unprobed environment", () => {
    // Deliberate: the probe is what turns "the capture failed after twenty
    // minutes" into a sentence read before it started.
    const verdict = advanceable("source", draft(), undefined);
    expect(verdict.ok).toBe(false);
    expect(verdict.reason).toContain("Run Test first");
  });

  it("will not advance on a probe that found a blocker", () => {
    const verdict = advanceable("source", draft(), failed);
    expect(verdict.ok).toBe(false);
    expect(verdict.reason).toBe("The probe found something that would stop a capture.");
  });

  it("advances on an environment whose probe passed", () => {
    expect(advanceable("source", draft(), passed)).toEqual({ ok: true });
  });
});

describe("the realms step", () => {
  it("needs at least one realm", () => {
    const verdict = advanceable("realms", draft({ realms: [] }), passed);
    expect(verdict.ok).toBe(false);
    expect(verdict.reason).toBe("Select at least one realm.");
  });

  it("advances on one realm, and on several", () => {
    expect(advanceable("realms", draft({ realms: ["acme"] }), passed).ok).toBe(true);
    expect(advanceable("realms", draft({ realms: ["acme", "staging"] }), passed).ok).toBe(true);
  });
});

describe("the options step", () => {
  it("will not seal a snapshot with an empty passphrase", () => {
    const verdict = advanceable("options", draft({ passphrase: "" }), passed);
    expect(verdict.ok).toBe(false);
    expect(verdict.reason).toBe("Enter a passphrase, or switch to recipients.");
  });

  it("will not seal a snapshot to nobody", () => {
    const verdict = advanceable(
      "options",
      draft({ encryptionMode: "recipients", recipients: [] }),
      passed,
    );
    expect(verdict.ok).toBe(false);
    expect(verdict.reason).toBe("Add at least one age recipient.");
  });

  it("accepts recipients without a passphrase, and a passphrase without recipients", () => {
    // The two modes are alternatives, not both halves of one requirement.
    expect(
      advanceable(
        "options",
        draft({ encryptionMode: "recipients", passphrase: "", recipients: ["age1abc"] }),
        passed,
      ).ok,
    ).toBe(true);
    expect(advanceable("options", draft(), passed).ok).toBe(true);
  });

  it("demands an acknowledgement before writing an unencrypted snapshot", () => {
    // An unencrypted bundle holds unmasked client secrets and private signing
    // keys. Turning encryption off has to be a decision, not a default.
    const verdict = advanceable(
      "options",
      draft({ encrypt: false, acknowledgedUnencrypted: false }),
      passed,
    );
    expect(verdict.ok).toBe(false);
    expect(verdict.reason).toBe("Confirm that this snapshot may be written unencrypted.");
  });

  it("advances once the unencrypted snapshot has been acknowledged", () => {
    expect(
      advanceable("options", draft({ encrypt: false, acknowledgedUnencrypted: true }), passed).ok,
    ).toBe(true);
  });

  it("does not ask for a key when nothing is being encrypted", () => {
    // The passphrase and recipient rules must not fire with encryption off, or
    // the acknowledgement could never be reached.
    expect(
      advanceable(
        "options",
        draft({
          encrypt: false,
          acknowledgedUnencrypted: true,
          passphrase: "",
          recipients: [],
        }),
        passed,
      ).ok,
    ).toBe(true);
  });
});

describe("the storage step", () => {
  it("needs somewhere for the snapshot to go", () => {
    const verdict = advanceable("storage", draft({ storage: "" }), passed);
    expect(verdict.ok).toBe(false);
    expect(verdict.reason).toBe("Choose where the snapshot should go.");
  });

  it("advances once a sink is chosen", () => {
    expect(advanceable("storage", draft(), passed)).toEqual({ ok: true });
  });
});

describe("the review step", () => {
  it("gates nothing — everything it shows was decided upstream", () => {
    expect(advanceable("review", draft({ environment: "", realms: [] }), undefined)).toEqual({
      ok: true,
    });
  });
});

describe("kindLabel", () => {
  it("names each environment kind the way the rest of the app does", () => {
    expect(kindLabel("local")).toBe("Local");
    expect(kindLabel("ssh")).toBe("SSH");
    expect(kindLabel("docker")).toBe("Docker");
    expect(kindLabel("kubernetes")).toBe("Kubernetes / OpenShift");
  });

  it("shows an unknown kind rather than hiding it", () => {
    // A config written by a newer version should read oddly, not read blank.
    expect(kindLabel("podman")).toBe("podman");
  });
});
