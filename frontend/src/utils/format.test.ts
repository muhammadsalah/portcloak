// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The formatters are the only place a number becomes a sentence, and three
 * screens share each one. A regression here is not a wrong pixel — it is a
 * table cell and a tooltip disagreeing about the same timestamp, which is
 * exactly the disagreement an operator reconciling an incident cannot afford.
 *
 * Every date below is built from a local-time literal — `"…T09:07:05"`, with no
 * trailing Z — because `when` and `time` render in the reader's zone by design.
 * A UTC instant would make these assertions pass in London and fail in Cairo.
 */
import { describe, expect, it } from "vitest";

import { bytes, count, stamp, time, titleCase, when } from "./format";

describe("bytes", () => {
  it("says 0 B for nothing, rather than an empty cell", () => {
    expect(bytes(0)).toBe("0 B");
    expect(bytes(undefined)).toBe("0 B");
  });

  it("keeps whole bytes whole", () => {
    // Below a kibibyte there is no unit to climb into, and "512.0 B" would be
    // precision the number does not have.
    expect(bytes(1)).toBe("1 B");
    expect(bytes(512)).toBe("512 B");
    expect(bytes(1023)).toBe("1023 B");
  });

  it("climbs a unit at exactly 1024, and only then", () => {
    expect(bytes(1024)).toBe("1.0 KiB");
    expect(bytes(1024 * 1024)).toBe("1.0 MiB");
    expect(bytes(1024 ** 3)).toBe("1.0 GiB");
    expect(bytes(1024 ** 4)).toBe("1.0 TiB");
  });

  it("stops at tebibytes rather than inventing a unit", () => {
    // A snapshot that large is a bug report, not a pebibyte — and a formatter
    // that ran off the end of its unit list would render "undefined".
    expect(bytes(1024 ** 5)).toBe("1024.0 TiB");
  });

  it("rounds to one decimal", () => {
    expect(bytes(1536)).toBe("1.5 KiB");
    expect(bytes(1024 * 1024 * 2.25)).toBe("2.3 MiB");
  });
});

describe("count", () => {
  it("groups a realm-sized number", () => {
    expect(count(120000)).toBe((120000).toLocaleString());
    expect(count(120000)).not.toBe("120000");
  });

  it("treats a missing count as zero, not as blank", () => {
    expect(count(undefined)).toBe("0");
    expect(count(0)).toBe("0");
  });
});

describe("when", () => {
  it("writes a timestamp the way it is read, not as an ISO string", () => {
    expect(when("2026-03-04T09:07:05")).toBe("2026-03-04 09:07");
  });

  it("pads every field, so a column of them lines up", () => {
    expect(when("2026-01-02T03:04:05")).toBe("2026-01-02 03:04");
  });

  it("renders an em dash for nothing and for nonsense", () => {
    // A row with no timestamp and a row with a corrupt one both have to render
    // something; neither may render "Invalid Date" or "NaN-NaN-NaN".
    expect(when(undefined)).toBe("—");
    expect(when("")).toBe("—");
    expect(when("not a date")).toBe("—");
  });
});

describe("time", () => {
  it("drops the date, keeping the clock", () => {
    expect(time("2026-03-04T09:07:05")).toBe("09:07");
  });

  it("renders nothing at all for nothing, because it sits inline", () => {
    expect(time(undefined)).toBe("");
    expect(time("not a date")).toBe("");
  });
});

describe("stamp", () => {
  const iso = "2026-03-04T09:07:05";

  it("carries the seconds that `when` drops", () => {
    // Two audit rows can share a minute — a capture and the deletion that
    // followed it — and the log is evidence, so they have to be orderable.
    expect(stamp(iso)).toContain("09:07:05");
    expect(when(iso)).not.toContain(":05");
  });

  it("names the zone it is in", () => {
    // Without this a record read on a laptop that has since crossed a border
    // cannot be lined up against a Keycloak server log.
    const zone = new Intl.DateTimeFormat(undefined, { timeZoneName: "short" })
      .formatToParts(new Date(iso))
      .find((part) => part.type === "timeZoneName")?.value;

    expect(zone).toBeTruthy();
    expect(stamp(iso)).toContain(zone!);
  });

  it("carries the full date, whatever the reader's locale orders first", () => {
    const rendered = stamp(iso);
    expect(rendered).toContain("2026");
    expect(rendered).toContain("04");
  });

  it("renders an em dash rather than a failure", () => {
    expect(stamp(undefined)).toBe("—");
    expect(stamp("not a date")).toBe("—");
  });
});

describe("titleCase", () => {
  it("capitalises a job kind arriving lowercase from the engine", () => {
    expect(titleCase("capture")).toBe("Capture");
    expect(titleCase("running")).toBe("Running");
  });

  it("leaves the rest of the word alone", () => {
    // "breakerOpen" must not become "Breakeropen".
    expect(titleCase("breakerOpen")).toBe("BreakerOpen");
  });

  it("survives an empty string", () => {
    expect(titleCase("")).toBe("");
  });
});
