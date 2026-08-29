// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/** The four kinds of storage, and how they are named on screen. */
import type { StorageKind } from "@/api";

export const kinds: { value: StorageKind; label: string }[] = [
  { value: "disk", label: "Disk" },
  { value: "ssh", label: "SSH" },
  { value: "s3", label: "S3" },
  { value: "azure", label: "Azure Blob" },
];

export function kindLabel(kind: string): string {
  return kinds.find((k) => k.value === kind)?.label ?? kind;
}

/** What the secret for a given kind actually is, in the operator's words. */
export function credentialLabel(kind: StorageKind): string {
  switch (kind) {
    case "s3":
      return "Access key and secret (key:secret)";
    case "azure":
      return "Connection string, account key or SAS";
    default:
      return "Credential";
  }
}
