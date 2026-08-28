// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The restore wizard's five steps, and the rule for leaving each one.
 *
 * The order is the order of the guarantees, so the rules are not
 * interchangeable: the key is demanded at the destination step because the step
 * after it downloads the bundle, and the typed realm name is demanded at the
 * strategy step because the step after that writes. Stating them here, apart
 * from the screen that renders them, is what lets each be asserted on its own
 * rather than by clicking four steps of a wizard to reach the fifth.
 */
import type { LibraryEntry, Plan } from "../../api";
import type { SnapshotKey } from "../../components/SnapshotKeyFields";

export type Step = "snapshot" | "destination" | "preconditions" | "strategy" | "apply";

export const steps: { key: Step; label: string }[] = [
  { key: "snapshot", label: "Snapshot" },
  { key: "destination", label: "Destination" },
  { key: "preconditions", label: "Preconditions" },
  { key: "strategy", label: "Strategy & dry run" },
  { key: "apply", label: "Apply" },
];

/** Whether the current step has been answered, and what is missing if not. */
export function advanceable({
  step,
  snapshot,
  key,
  storedKeys,
  environment,
  plan,
  confirmRealm,
}: {
  step: Step;
  snapshot: LibraryEntry | undefined;
  key: SnapshotKey;
  storedKeys: number;
  environment: string;
  plan: Plan | undefined;
  confirmRealm: string;
}): { ok: boolean; reason?: string } {
  switch (step) {
    case "snapshot":
      return snapshot ? { ok: true } : { ok: false, reason: "Choose a snapshot." };

    case "destination":
      if (!environment) return { ok: false, reason: "Choose a destination environment." };
      if (
        snapshot?.encrypted &&
        key.passphrase === "" &&
        key.identities.length === 0 &&
        storedKeys === 0
      ) {
        // Asked for here rather than discovered at Apply: without it the next
        // step downloads the bundle only to fail on it. But it is only a gate
        // when there is nothing stored to try — a key PortCloak already holds
        // is a key the operator has already decided to trust it with, and
        // demanding it again is the prompt that teaches people to turn
        // encryption off.
        return { ok: false, reason: "Enter the key this snapshot was sealed with." };
      }
      return { ok: true };

    case "preconditions":
      // Informative only. Next stays enabled even when every dependency is
      // missing — the operator manages these environments.
      return { ok: !plan?.blocked, reason: plan?.blockedNote };

    case "strategy":
      if (plan?.confirmationRequired && confirmRealm !== snapshot?.realm) {
        return { ok: false, reason: "Type the realm name to confirm an overwrite." };
      }
      return { ok: true };

    default:
      return { ok: true };
  }
}
