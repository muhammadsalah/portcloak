// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/** The eight views over an opened snapshot, in the order they are read in. */

export type Tab =
  | "overview"
  | "users"
  | "clients"
  | "keys"
  | "federations"
  | "flows"
  | "deps"
  | "secrets";

export const tabs: { key: Tab; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "users", label: "Users" },
  { key: "clients", label: "Clients" },
  { key: "keys", label: "Keys" },
  { key: "federations", label: "Federations" },
  { key: "flows", label: "Auth flows" },
  { key: "deps", label: "External deps" },
  { key: "secrets", label: "Secret ledger" },
];
