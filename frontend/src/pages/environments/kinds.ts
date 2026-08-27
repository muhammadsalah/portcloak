// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/** The four kinds of environment, and how they are named on screen. */
import type { EnvironmentKind } from "../../api";

export const kinds: { value: EnvironmentKind; label: string }[] = [
  { value: "local", label: "Local" },
  { value: "ssh", label: "SSH" },
  { value: "docker", label: "Docker" },
  { value: "kubernetes", label: "Kubernetes / OpenShift" },
];

export function kindLabel(kind: string): string {
  return kinds.find((k) => k.value === kind)?.label ?? kind;
}
