// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/** What the capture wizard is accumulating, shared by its five steps. */
import type { ProbeResult } from "@/api";

export type Step = "source" | "realms" | "options" | "storage" | "review";

export interface CaptureDraft {
  environment: string;
  /** What the engine said about discovering realms, in the operator's words. */
  realmsNote: string;
  discoveredRealms: string[];
  realmsDiscovered: boolean;
  realms: string[];
  manualRealm: string;
  storage: string;
  usersMode: string;
  usersPerFile: number;
  /** Let the export's transactions run without a time limit. See OptionsStep. */
  noTransactionTimeout: boolean;
  verify: boolean;
  detectDependencies: boolean;
  encrypt: boolean;
  encryptionMode: "passphrase" | "recipients";
  passphrase: string;
  recipients: string[];
  /** Remember this capture's passphrase in the keychain, under this name. */
  rememberPassphraseAs: string;
  acknowledgedUnencrypted: boolean;
}

export type UpdateDraft = (patch: Partial<CaptureDraft>) => void;

/**
 * What the export writes per user file, and per transaction.
 *
 * kc.sh exports one page of users inside one transaction, so this is not only
 * a file-size setting: a realm whose users come from a federation provider is
 * re-read through that provider one user at a time inside that transaction, and
 * a page too large for the server's transaction limit kills the export. The
 * engine holds the same bounds — this is the wizard being helpful, not the
 * enforcement (see kc.ClampUsersPerFile).
 */
export const usersPerFileMin = 10;
export const usersPerFileMax = 1000;
export const usersPerFileDefault = 1000;

/** Brings a typed page size inside the range. Empty, or nonsense, is the default. */
export function clampUsersPerFile(n: number): number {
  if (!Number.isFinite(n) || n <= 0) return usersPerFileDefault;
  return Math.min(usersPerFileMax, Math.max(usersPerFileMin, Math.round(n)));
}

export function kindLabel(kind: string): string {
  switch (kind) {
    case "local":
      return "Local";
    case "ssh":
      return "SSH";
    case "docker":
      return "Docker";
    case "kubernetes":
      return "Kubernetes / OpenShift";
    default:
      return kind;
  }
}

/**
 * Whether the current step has been answered, and what is missing if not.
 *
 * It lives here rather than in CapturePage because it is the whole of the
 * wizard's rule set — the reason Next is grey and the sentence beside it saying
 * why — and it is a pure function of the draft. Keeping it beside the draft is
 * what lets `advanceable.test.ts` state every one of those rules without
 * mounting five steps and an engine behind them.
 */
export function advanceable(
  step: Step,
  draft: CaptureDraft,
  probe: ProbeResult | undefined,
): { ok: boolean; reason?: string } {
  switch (step) {
    case "source":
      if (!draft.environment) return { ok: false, reason: "Choose an environment." };
      if (!probe) {
        return {
          ok: false,
          reason: "Run Test first, so a failure is a sentence now rather than a surprise later.",
        };
      }
      if (!probe.ok) {
        return { ok: false, reason: "The probe found something that would stop a capture." };
      }
      return { ok: true };

    case "realms":
      return draft.realms.length > 0
        ? { ok: true }
        : { ok: false, reason: "Select at least one realm." };

    case "options":
      if (draft.encrypt && draft.encryptionMode === "passphrase" && !draft.passphrase) {
        return { ok: false, reason: "Enter a passphrase, or switch to recipients." };
      }
      if (draft.encrypt && draft.encryptionMode === "recipients" && draft.recipients.length === 0) {
        return { ok: false, reason: "Add at least one age recipient." };
      }
      if (!draft.encrypt && !draft.acknowledgedUnencrypted) {
        return { ok: false, reason: "Confirm that this snapshot may be written unencrypted." };
      }
      return { ok: true };

    case "storage":
      return draft.storage
        ? { ok: true }
        : { ok: false, reason: "Choose where the snapshot should go." };

    default:
      return { ok: true };
  }
}
