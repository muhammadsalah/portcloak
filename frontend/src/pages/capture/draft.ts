// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/** What the capture wizard is accumulating, shared by its five steps. */

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
