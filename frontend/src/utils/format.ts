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

/** Renders a timestamp the way an operator reads one, not as an ISO string. */
export function when(iso: string | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function time(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/**
 * A full timestamp, to the second, naming the zone it is in.
 *
 * The audit log is evidence, and the other two formatters are not good enough
 * for that. `when` drops the zone, so a record read on a laptop that has since
 * crossed a border cannot be lined up against a Keycloak server log without
 * someone guessing the offset. `time` drops the date and the seconds, so a row
 * cannot be ordered against its neighbours inside the same minute — and a
 * capture and the deletion that followed it can share a minute.
 *
 * The zone is the reader's own, not UTC: an operator reconciling what happened
 * is working in local time, and naming the zone is what keeps that unambiguous
 * without forcing a conversion in their head.
 */
export function stamp(iso: string | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return new Intl.DateTimeFormat(undefined, {
    day: "2-digit",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
    timeZoneName: "short",
  }).format(d);
}

/** "capture" → "Capture". Used on job kinds and states, which arrive lowercase. */
export function titleCase(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
