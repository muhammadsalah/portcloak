// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The user directory's paging and facet state.
 *
 * Every filter is a field on the query the engine answers, which is the whole
 * design: the counts beside the facets and the rows in the table describe the
 * same set because there is only one set. The failures worth catching are the
 * ones that quietly break that — a facet that narrows without resetting the
 * offset, so page 4 of an unfiltered list becomes an empty page 4 of a filtered
 * one; a next button still live on the last page; a filter chip that clears a
 * different filter than the one it names.
 *
 * The engine is mocked because it is the engine's job to answer the query, and
 * this file's job is to prove which query it was asked. The mock therefore
 * records every query it is handed, and the assertions are made against that
 * record rather than against the rows it returned.
 */
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { UserRow, UsersQuery, UsersResult } from "@/api";
import { renderApp } from "@/test/render";
import { UsersTab } from "./UsersTab";

const asked: UsersQuery[] = [];
let total = 60;

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    InspectAPI: {
      ...actual.InspectAPI,
      users: (query: UsersQuery) => {
        asked.push({ ...query });
        return Promise.resolve(answer(query));
      },
    },
  };
});

/** The last query the component sent. */
function latest(): UsersQuery {
  return asked[asked.length - 1];
}

function row(i: number): UserRow {
  return {
    id: `user-${i}`,
    username: `user${i}`,
    email: `user${i}@example.test`,
    enabled: i % 2 === 0,
    emailVerified: true,
    origin: "local",
    hasPassword: true,
    otpCount: 0,
    webauthnCount: 0,
    recoveryCodes: false,
    secondFactor: "none",
    groups: [],
  };
}

function answer(query: UsersQuery): UsersResult {
  const matching = query.enabled === "true" ? 12 : total;
  const rows = Array.from(
    { length: Math.max(0, Math.min(query.limit, matching - query.offset)) },
    (_, i) => row(query.offset + i),
  );
  return {
    page: { rows, total: matching, offset: query.offset, limit: query.limit },
    facets: {
      status: [
        { value: "true", label: "Enabled", count: 12 },
        { value: "false", label: "Disabled", count: 48 },
      ],
      origin: [{ value: "local", label: "Local", count: 60 }],
      secondFactor: [],
      realmRoles: [{ value: "admin", label: "admin", count: 2 }],
      clientRoles: [],
      groups: [{ value: "staff", label: "staff", count: 9 }],
      requiredActions: [],
    },
    note: "Read from the snapshot's index.",
  };
}

async function open() {
  renderApp(<UsersTab snapshotId="snap-1" indexNote="Indexed on first open." />);
  await screen.findByText(/matching/);
}

/** The active-filter chips, each without its × button. */
function chips(): string[] {
  return screen
    .queryAllByRole("button", { name: "×" })
    .map((button) => button.parentElement?.textContent?.replace("×", "") ?? "");
}

beforeEach(() => {
  asked.length = 0;
  total = 60;
});

describe("the first query", () => {
  it("asks for the first page of the named snapshot, unfiltered", async () => {
    await open();

    expect(latest()).toMatchObject({
      snapshotId: "snap-1",
      query: "",
      enabled: "",
      origin: "",
      group: "",
      offset: 0,
      limit: 25,
      sort: "username",
      descending: false,
    });
  });

  it("counts from one, not from zero, in the range it reports", async () => {
    await open();
    expect(screen.getByText("1–25 of 60 matching")).toBeInTheDocument();
  });
});

describe("paging", () => {
  it("cannot go back from the first page", async () => {
    await open();
    expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
  });

  it("advances a page at a time, and reports the range it landed on", async () => {
    await open();
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));

    await waitFor(() => expect(latest().offset).toBe(25));
    expect(await screen.findByText("26–50 of 60 matching")).toBeInTheDocument();
  });

  it("does not offer a page beyond the last one", async () => {
    await open();
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    await screen.findByText("26–50 of 60 matching");
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));

    // 51–60 is the last ten of sixty; there is nothing after it.
    expect(await screen.findByText("51–60 of 60 matching")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled());
  });

  it("comes back to where it was", async () => {
    await open();
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    await screen.findByText("26–50 of 60 matching");
    await userEvent.click(screen.getByRole("button", { name: "Previous page" }));

    await waitFor(() => expect(latest().offset).toBe(0));
    expect(await screen.findByText("1–25 of 60 matching")).toBeInTheDocument();
  });

  it("reports an empty range as 0, not as 1–0", async () => {
    total = 0;
    await open();
    expect(screen.getByText("0–0 of 0 matching")).toBeInTheDocument();
  });
});

describe("facets", () => {
  it("narrows on the field the facet names", async () => {
    await open();
    await userEvent.click(screen.getByRole("checkbox", { name: /Enabled/ }));

    await waitFor(() => expect(latest().enabled).toBe("true"));
  });

  it("returns to the first page when the set changes underneath it", async () => {
    // Otherwise narrowing from page 3 of sixty users to a twelve-user facet
    // lands the operator on an empty page they did not ask for.
    await open();
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    await waitFor(() => expect(latest().offset).toBe(25));

    await userEvent.click(screen.getByRole("checkbox", { name: /Enabled/ }));

    await waitFor(() => expect(latest()).toMatchObject({ enabled: "true", offset: 0 }));
  });

  it("clears the facet when the value already applied is picked again", async () => {
    await open();
    const enabled = screen.getByRole("checkbox", { name: /Enabled/ });

    await userEvent.click(enabled);
    await waitFor(() => expect(latest().enabled).toBe("true"));

    await userEvent.click(screen.getByRole("checkbox", { name: /Enabled/ }));
    await waitFor(() => expect(latest().enabled).toBe(""));
  });

  it("leaves the other fields of the query alone", async () => {
    await open();
    await userEvent.click(screen.getByRole("checkbox", { name: /staff/ }));

    await waitFor(() => expect(latest().group).toBe("staff"));
    expect(latest()).toMatchObject({ snapshotId: "snap-1", enabled: "", origin: "", limit: 25 });
  });

  it("does not render a group the snapshot has no values for", async () => {
    await open();
    // secondFactor came back empty, so its heading must not appear in the
    // filter rail — an empty facet group reads as "no users have a second
    // factor", which is a different claim from "this was not indexed".
    //
    // Both names are also table columns, so the count is the assertion: Status
    // appears as a heading and as a column, Second factor only as a column.
    expect(screen.getAllByText("Status")).toHaveLength(2);
    expect(screen.getAllByText("Second factor")).toHaveLength(1);
    expect(screen.getByText("Second factor").tagName).toBe("TH");
  });
});

describe("the active filter chips", () => {
  it("appear for each filter in force, naming it in the operator's words", async () => {
    await open();
    expect(chips()).toEqual([]);

    await userEvent.click(screen.getByRole("checkbox", { name: /Enabled/ }));

    // "true" is the query's value; "Enabled" is what it means, and the chip is
    // the only place the operator is told which of the two is in force.
    await waitFor(() => expect(chips()).toEqual(["Enabled"]));
  });

  it("clear only the filter they name", async () => {
    await open();
    await userEvent.click(screen.getByRole("checkbox", { name: /Enabled/ }));
    await waitFor(() => expect(latest().enabled).toBe("true"));
    await userEvent.click(screen.getByRole("checkbox", { name: /staff/ }));
    await waitFor(() => expect(latest().group).toBe("staff"));

    const chips = screen.getAllByRole("button", { name: "×" });
    await userEvent.click(chips[0]);

    await waitFor(() => expect(latest()).toMatchObject({ enabled: "", group: "staff" }));
  });
});

describe("search", () => {
  it("is not sent on every keystroke", async () => {
    // The index is on disk. A query per character makes a large realm feel
    // broken, which is the reason this field submits rather than filters.
    await open();
    const before = asked.length;

    await userEvent.type(screen.getByPlaceholderText(/Search username/), "ali");

    expect(asked).toHaveLength(before);
  });

  it("is sent on Enter, and resets to the first page", async () => {
    await open();
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    await waitFor(() => expect(latest().offset).toBe(25));

    await userEvent.type(screen.getByPlaceholderText(/Search username/), "alice{Enter}");

    await waitFor(() => expect(latest()).toMatchObject({ query: "alice", offset: 0 }));
  });

  it("is sent on leaving the field, for the operator who clicks away", async () => {
    await open();
    await userEvent.type(screen.getByPlaceholderText(/Search username/), "alice");
    await userEvent.tab();

    await waitFor(() => expect(latest().query).toBe("alice"));
  });
});
