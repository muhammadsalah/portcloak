// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * How PortCloak writes numbers, sizes and times.
 *
 * Pure functions, no DOM: a page calls one of these and puts the string where
 * it wants it. They are here rather than in a component because the same
 * timestamp is written into a table cell, a modal and a tooltip, and it has to
 * come out identical in all three.
 */

export function bytes(n: number | undefined): string {
  if (!n) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`;
}

export function count(n: number | undefined): string {
  return (n ?? 0).toLocaleString();
}

/**
 * A timestamp, written out in full and naming the zone it is in.
 *
 * Every date in the interface goes through this or through [stamp], and both
 * write the month as a word and end with the zone. That is not decoration. A
 * capture is a record of a moment on someone else's server, and the two
 * shortcuts a compact format takes are exactly the two that make a record
 * ambiguous later: `03/04` is two different days depending on who is reading,
 * and a time with no zone cannot be lined up against a Keycloak log without
 * someone guessing the offset — including the guess that the laptop has not
 * moved since.
 *
 * The zone is the reader's own rather than UTC. An operator reconciling what
 * happened is working in local time, and naming the zone is what keeps that
 * unambiguous without forcing a conversion in their head.
 *
 * The ordering of the parts is the reader's locale's, not ours: Intl is given
 * the fields rather than a pattern, so a reader who writes the day first gets
 * the day first. The clock is 12-hour with AM or PM named, which is what the
 * desktop this runs on writes elsewhere.
 */
export function when(iso: string | undefined): string {
  return format(iso, false) ?? "—";
}

/**
 * The same timestamp, to the second.
 *
 * The audit log is evidence, and minutes are not enough for it: a capture and
 * the deletion that followed it can share a minute, and two rows that cannot be
 * ordered against each other are two rows that cannot be read as a sequence.
 */
export function stamp(iso: string | undefined): string {
  return format(iso, true) ?? "—";
}

function format(iso: string | undefined, seconds: boolean): string | undefined {
  if (!iso) return undefined;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return undefined;
  return new Intl.DateTimeFormat(undefined, {
    // Two digits, not one: these sit in table columns, and the old compact
    // format padded every field for exactly that reason. A column of dates
    // that lines up is read down; one that does not is read row by row.
    day: "2-digit",
    month: "long",
    year: "numeric",
    // The hour stays padded even with a meridiem beside it. These sit in table
    // columns, and "03:18 AM" lines up under "11:18 AM" where "3:18 AM" does
    // not — a column of dates that lines up is read down, one that does not is
    // read row by row.
    hour: "2-digit",
    minute: "2-digit",
    ...(seconds ? { second: "2-digit" as const } : {}),
    hour12: true,
    // No timeZone: the zone is the one this desktop is in, and naming it is
    // what keeps a local reading unambiguous without a conversion in the head.
    timeZoneName: "short",
  }).format(d);
}

/** "capture" → "Capture". Used on job kinds and states, which arrive lowercase. */
export function titleCase(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
