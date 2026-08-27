/**
 * The user directory inside a snapshot: facets down the left, a page of rows
 * beside them.
 *
 * Every filter is a field on the query the engine answers, so the counts in the
 * facets and the rows in the table always describe the same set.
 */
import { useState } from "react";

import { InspectAPI, type FacetValue, type UserRow, type UsersQuery } from "../../api";
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardFoot,
  CardHead,
  CardTitle,
  Chip,
  Dot,
  FacetCount,
  FacetGroup,
  FacetLabel,
  FailureNotice,
  GroupTitle,
  IconButton,
  Input,
  Muted,
  NoticeBox,
  Row,
  Search,
  Small,
  Spinner,
  Split,
  Table,
  TableScroll,
  Tr,
  useModal,
} from "../../design-system";
import { useAsync } from "../../hooks/useAsync";
import { count } from "../../utils/format";
import { exportView } from "./actions";
import { showUser } from "./UserDetail";

const pageSize = 25;

export function UsersTab({
  snapshotId,
  indexNote,
}: {
  snapshotId: string;
  indexNote: string;
}) {
  const modal = useModal();

  const [query, setQuery] = useState<UsersQuery>({
    snapshotId,
    query: "",
    enabled: "",
    origin: "",
    secondFactor: "",
    realmRole: "",
    clientRole: "",
    client: "",
    group: "",
    requiredAction: "",
    sort: "username",
    descending: false,
    offset: 0,
    limit: pageSize,
  });

  // What has been typed but not yet submitted. The engine is asked on Enter or
  // on leaving the field, not on every keystroke: the index is on disk and a
  // query per character would make a large realm feel broken.
  const [typed, setTyped] = useState("");

  const { state } = useAsync(() => InspectAPI.users(query), [query]);

  const narrow = (patch: Partial<UsersQuery>) =>
    setQuery((previous) => ({ ...previous, ...patch, offset: 0 }));

  if (state.status === "failed") throw state.error;
  if (state.status === "loading") return <Spinner>Building the inspection index…</Spinner>;

  const result = state.value;
  if (result.failure) return <FailureNotice failure={result.failure} />;

  const from = result.page.total === 0 ? 0 : result.page.offset + 1;
  const to = Math.min(result.page.offset + result.page.limit, result.page.total);

  return (
    <Split>
      <Card>
        <CardHead>
          <CardTitle>Filters</CardTitle>
        </CardHead>
        <CardBody>
          <Facets
            title="Status"
            values={result.facets.status}
            current={query.enabled}
            onPick={(value) => narrow({ enabled: value })}
          />
          <Facets
            title="Origin"
            values={result.facets.origin}
            current={query.origin}
            onPick={(value) => narrow({ origin: value })}
          />
          <Facets
            title="Second factor"
            values={result.facets.secondFactor}
            current={query.secondFactor}
            onPick={(value) => narrow({ secondFactor: value })}
          />
          <Facets
            title="Realm role"
            values={result.facets.realmRoles}
            current={query.realmRole}
            onPick={(value) => narrow({ realmRole: value })}
          />
          <Facets
            title="Group"
            values={result.facets.groups}
            current={query.group}
            onPick={(value) => narrow({ group: value })}
          />
          <Facets
            title="Required action"
            values={result.facets.requiredActions}
            current={query.requiredAction}
            onPick={(value) => narrow({ requiredAction: value })}
          />
          <NoticeBox $tone="info" style={{ marginTop: 12, fontSize: 12 }}>
            {indexNote}
          </NoticeBox>
        </CardBody>
      </Card>

      <Card>
        <CardHead>
          <Search>
            <Input
              type="text"
              placeholder="Search username, email, name or user id"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              onBlur={() => narrow({ query: typed })}
              onKeyUp={(e) => {
                if (e.key === "Enter") narrow({ query: typed });
              }}
            />
          </Search>
          <Row $wrap $gap={6}>
            <ActiveFilters query={query} narrow={narrow} />
          </Row>
        </CardHead>

        <TableScroll>
          <Table>
            <thead>
              <tr>
                <th>Username</th>
                <th>Email</th>
                <th>Status</th>
                <th>Origin</th>
                <th>Second factor</th>
                <th>Groups</th>
              </tr>
            </thead>
            <tbody>
              {result.page.rows.length === 0 ? (
                <tr>
                  <td colSpan={6}>
                    <Muted>{result.empty ?? "No users."}</Muted>
                  </td>
                </tr>
              ) : (
                result.page.rows.map((user) => (
                  <UserTableRow
                    key={user.id}
                    user={user}
                    onOpen={() => void showUser(snapshotId, user.id, modal)}
                  />
                ))
              )}
            </tbody>
          </Table>
        </TableScroll>

        <CardFoot>
          <div>
            <Small>{`${from}–${to} of ${count(result.page.total)} matching`}</Small>
            <div>
              <Muted>
                <Small>{result.note}</Small>
              </Muted>
            </div>
          </div>
          <Row>
            <Button
              disabled={result.page.offset === 0}
              onClick={() =>
                setQuery((previous) => ({
                  ...previous,
                  offset: Math.max(0, previous.offset - previous.limit),
                }))
              }
            >
              ‹
            </Button>
            <Button
              disabled={to >= result.page.total}
              onClick={() =>
                setQuery((previous) => ({ ...previous, offset: previous.offset + previous.limit }))
              }
            >
              ›
            </Button>
            <Button onClick={() => exportView(snapshotId, "users", query, modal)}>
              Export CSV
            </Button>
          </Row>
        </CardFoot>
      </Card>
    </Split>
  );
}

/** One group of facet values. Picking the current one again clears it. */
function Facets({
  title,
  values,
  current,
  onPick,
}: {
  title: string;
  values: FacetValue[];
  current: string;
  onPick: (value: string) => void;
}) {
  if (!values || values.length === 0) return null;

  return (
    <FacetGroup>
      <GroupTitle>{title}</GroupTitle>
      {values.slice(0, 12).map((value) => (
        <FacetLabel key={value.value}>
          <input
            type="checkbox"
            checked={current === value.value}
            onChange={() => onPick(current === value.value ? "" : value.value)}
          />
          <span>{value.label}</span>
          <FacetCount>{count(value.count)}</FacetCount>
        </FacetLabel>
      ))}
    </FacetGroup>
  );
}

/** The filters currently narrowing the list, each with the way to remove it. */
function ActiveFilters({
  query,
  narrow,
}: {
  query: UsersQuery;
  narrow: (patch: Partial<UsersQuery>) => void;
}) {
  const chips: { label: string; clear: Partial<UsersQuery> }[] = [];
  if (query.enabled) {
    chips.push({
      label: query.enabled === "true" ? "Enabled" : "Disabled",
      clear: { enabled: "" },
    });
  }
  if (query.origin) chips.push({ label: query.origin, clear: { origin: "" } });
  if (query.secondFactor) {
    chips.push({ label: query.secondFactor, clear: { secondFactor: "" } });
  }
  if (query.realmRole) chips.push({ label: query.realmRole, clear: { realmRole: "" } });
  if (query.group) chips.push({ label: query.group, clear: { group: "" } });

  return (
    <>
      {chips.map((chip) => (
        <Chip key={chip.label}>
          {chip.label}
          <IconButton onClick={() => narrow(chip.clear)}>×</IconButton>
        </Chip>
      ))}
    </>
  );
}

function UserTableRow({ user, onOpen }: { user: UserRow; onOpen: () => void }) {
  return (
    <Tr $selectable>
      <td>
        <a style={{ color: "var(--primary)", cursor: "pointer" }} onClick={onOpen}>
          {user.username}
        </a>
      </td>
      <td>{user.email || "—"}</td>
      <td>
        <Dot $tone={user.enabled ? "ok" : "danger"} />
        {user.enabled ? "Enabled" : "Disabled"}
      </td>
      <td>{user.origin}</td>
      <td>
        <Row $gap={4}>
          {user.otpCount > 0 ? (
            <Badge $tone="info">{user.otpCount > 1 ? `OTP ×${user.otpCount}` : "OTP"}</Badge>
          ) : null}
          {user.webauthnCount > 0 ? (
            <Badge $tone="info">
              {user.webauthnCount > 1 ? `Passkey ×${user.webauthnCount}` : "Passkey"}
            </Badge>
          ) : null}
          {user.otpCount === 0 && user.webauthnCount === 0 ? <Muted>—</Muted> : null}
        </Row>
      </td>
      <td>
        <Muted>
          <Small>{(user.groups ?? []).join(", ") || "—"}</Small>
        </Muted>
      </td>
    </Tr>
  );
}
