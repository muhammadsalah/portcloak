/** The library is Tier 0: every snapshot, across every backend, with no key. */
import { useEffect, useMemo, useState } from "react";

import { SnapshotAPI, type LibraryEntry, type LibraryView } from "../../api";
import { useNavigate, useShell } from "../../app/ShellContext";
import {
  Badge,
  Button,
  Card,
  Link,
  Muted,
  Notice,
  PageHead,
  PageSubtitle,
  PageTitle,
  Row,
  Search,
  Select,
  Small,
  Spinner,
  Table,
  TableScroll,
  Toolbar,
  Tr,
  useModal,
  Input,
  NumericHeader,
  Numeric,
  Right,
} from "../../design-system";
import { useAsync } from "../../hooks/useAsync";
import { bytes, count, when } from "../../utils/format";
import { FirstRun } from "./FirstRun";

export function LibraryPage() {
  const { state } = useAsync(() => SnapshotAPI.library().then((view) => ({ view })).catch((error) => ({ error })), []);

  if (state.status === "failed") throw state.error;
  if (state.status === "loading") return <Spinner>Reading storage…</Spinner>;

  if ("error" in state.value) {
    return (
      <Notice
        tone="danger"
        title="The snapshot library could not be read."
        body={String(state.value.error)}
      />
    );
  }
  return <Library view={state.value.view} />;
}

function Library({ view }: { view: LibraryView }) {
  const navigate = useNavigate();
  const { setHasSnapshots } = useShell();

  useEffect(() => {
    setHasSnapshots(view.entries.length > 0);
  }, [view.entries.length, setHasSnapshots]);

  const [query, setQuery] = useState("");
  const [realm, setRealm] = useState("");
  const [storage, setStorage] = useState("");

  const rows = useMemo(
    () =>
      view.entries.filter((entry) => {
        if (realm && entry.realm !== realm) return false;
        if (storage && entry.storage !== storage) return false;
        if (query) {
          const q = query.toLowerCase();
          if (
            !entry.realm.toLowerCase().includes(q) &&
            !entry.snapshotId.toLowerCase().includes(q)
          ) {
            return false;
          }
        }
        return true;
      }),
    [view.entries, query, realm, storage],
  );

  if (view.firstRun) return <FirstRun firstRun={view.firstRun} />;

  return (
    <div>
      <PageHead>
        <div>
          <PageTitle>Snapshots</PageTitle>
          <PageSubtitle>{view.summary}</PageSubtitle>
        </div>
        <Button $variant="primary" onClick={() => navigate({ name: "capture" })}>
          Capture snapshot
        </Button>
      </PageHead>

      {view.storages
        .filter((s) => !s.reachable)
        .map((s) => (
          <Notice
            key={s.name}
            tone="warn"
            title={`${s.name} could not be read, so this list may be short.`}
            body={s.error ?? ""}
          />
        ))}

      <Toolbar>
        <Search>
          <Input
            type="text"
            placeholder="Search realm or snapshot id"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </Search>
        <Select value={realm} onChange={(e) => setRealm(e.target.value)}>
          <option value="">Realm: all</option>
          {view.realms.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </Select>
        <Select value={storage} onChange={(e) => setStorage(e.target.value)}>
          <option value="">Storage: all</option>
          {view.storages.map((s) => (
            <option key={s.name} value={s.name}>
              {s.name}
            </option>
          ))}
        </Select>
      </Toolbar>

      <Card>
        <TableScroll>
          <Table>
            <thead>
              <tr>
                <th>Realm</th>
                <th>Captured</th>
                <th>Environment</th>
                <NumericHeader>Users</NumericHeader>
                <th>Completeness</th>
                <th>Encryption</th>
                <th>Storage</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={8}>
                    <Muted>No snapshots match those filters.</Muted>
                  </td>
                </tr>
              ) : (
                rows.map((entry) => <SnapshotRow key={entry.snapshotId} entry={entry} />)
              )}
            </tbody>
          </Table>
        </TableScroll>
      </Card>
    </div>
  );
}

function SnapshotRow({ entry }: { entry: LibraryEntry }) {
  const navigate = useNavigate();
  const modal = useModal();

  const open = () =>
    navigate({
      name: "inspect",
      storage: entry.storage,
      bundleKey: entry.bundleKey,
      snapshotId: entry.snapshotId,
    });

  return (
    <Tr $selectable>
      <td>
        <Link onClick={open}>{entry.realm || "(unknown)"}</Link>
      </td>
      <td>{when(entry.createdAt)}</td>
      <td>
        <Small>
          {entry.environment
            ? `${entry.environment}${entry.executionMode ? ` · ${describeMode(entry.executionMode)}` : ""}`
            : "—"}
        </Small>
      </td>
      <Numeric>{entry.metadataReadable ? count(entry.users) : "—"}</Numeric>
      <td>
        <Completeness entry={entry} />
      </td>
      <td>
        {entry.encrypted ? (
          <Muted>
            <Small>
              🔒 Encrypted{entry.encryptionMode ? ` · ${entry.encryptionMode}` : ""}
            </Small>
          </Muted>
        ) : (
          <Badge $tone="danger">Unencrypted</Badge>
        )}
      </td>
      <td>
        <Muted>
          <Small>{`${entry.storage} · ${bytes(entry.bytes)}`}</Small>
        </Muted>
      </td>
      <td>
        <Right>
          <Row>
            <Link onClick={open}>Inspect</Link>
            <Link $tone="muted" onClick={() => confirmDelete(entry, modal, navigate)}>
              Delete
            </Link>
          </Row>
        </Right>
      </td>
    </Tr>
  );
}

function Completeness({ entry }: { entry: LibraryEntry }) {
  if (!entry.metadataReadable) return <Badge $tone="warn">Unreadable metadata</Badge>;
  if (entry.dependencyCount > 0) {
    return <Badge $tone="warn">{`${entry.dependencyCount} external deps`}</Badge>;
  }
  return (
    <Badge $tone={entry.verdict === "Complete" ? "ok" : "warn"}>
      {entry.verdict || "Complete"}
    </Badge>
  );
}

function describeMode(mode: string): string {
  return mode === "ephemeral-clone" ? "clone" : "in place";
}

function confirmDelete(
  entry: LibraryEntry,
  modal: ReturnType<typeof useModal>,
  navigate: ReturnType<typeof useNavigate>,
): void {
  modal.open({
    title: `Delete this snapshot of ${entry.realm}?`,
    body: (
      <div>
        <p>
          {`Captured ${when(entry.createdAt)} from ${entry.environment ?? "an unknown environment"}, ${count(entry.users)} users.`}
        </p>
        <p>
          <Muted>
            <Small>
              {`The bundle and both sidecars are removed from ${entry.storage}. PortCloak does not keep another copy, and this cannot be undone.`}
            </Small>
          </Muted>
        </p>
      </div>
    ),
    confirmLabel: "Delete snapshot",
    confirmTone: "danger-solid",
    onConfirm: async () => {
      const result = await SnapshotAPI.remove(entry.storage, entry.bundleKey);
      if (result.failure) {
        modal.open({
          title: "The snapshot was not deleted",
          body: <div>{result.failure.message}</div>,
        });
        return;
      }
      navigate({ name: "library" });
    },
  });
}
