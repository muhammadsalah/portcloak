// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/** The library is Tier 0: every snapshot, across every backend, with no key. */
import { useMemo, useState } from "react";

import { InspectAPI, SnapshotAPI, type LibraryEntry, type LibraryView } from "../../api";
import { useNavigate } from "../../app/ShellContext";
import { Icon } from "../../components/Icon";
import {
  Badge,
  Button,
  Card,
  Encryption,
  Input,
  Link,
  Menu,
  Muted,
  Notice,
  Numeric,
  NumericHeader,
  PageHead,
  PageSubtitle,
  PageTitle,
  Right,
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
} from "../../design-system";
import { useAsync } from "../../hooks/useAsync";
import { bytes, count, when } from "../../utils/format";
import { FirstRun } from "./FirstRun";

export function LibraryPage() {
  const { state, reload } = useAsync(
    () =>
      SnapshotAPI.library()
        .then((view) => ({ view }))
        .catch((error) => ({ error })),
    [],
  );

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
  return <Library view={state.value.view} reload={reload} />;
}

function Library({ view, reload }: { view: LibraryView; reload: () => void }) {
  const navigate = useNavigate();

  // What a snapshot names and what still exists are two different things: a
  // snapshot records where it was captured from and written to, and either can
  // have been renamed or removed since. Offering a link to something that is
  // gone is worse than not offering one.
  //
  // Undefined rather than empty when the engine did not send the list: "I was
  // not told" and "there are none" are different answers, and collapsing them
  // is what made every row claim its environment had been removed. A screen may
  // decline to offer a link it cannot justify; it may not tell an operator
  // their environments are gone on the strength of a field it never received.
  const configuredEnvironments = useMemo(
    () => (view.environments ? new Set(view.environments) : undefined),
    [view.environments],
  );
  // An open snapshot is one with decrypted realm material on this machine. The
  // list is the engine's, not a guess from which screens were visited: a
  // session survives navigating away, and closing the window is not what ends
  // it.
  const openSnapshots = useMemo(() => new Set(view.open ?? []), [view.open]);

  const configuredStorages = useMemo(
    () => (view.storages ? new Set(view.storages.map((storage) => storage.name)) : undefined),
    [view.storages],
  );

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
        <Select
          aria-label="Filter by realm"
          value={realm}
          onChange={setRealm}
          options={[
            { value: "", label: "Realm: all" },
            ...view.realms.map((r) => ({ value: r, label: r })),
          ]}
        />
        <Select
          aria-label="Filter by storage"
          value={storage}
          onChange={setStorage}
          options={[
            { value: "", label: "Storage: all" },
            ...view.storages.map((s) => ({ value: s.name, label: s.name })),
          ]}
        />
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
                rows.map((entry) => (
                  <SnapshotRow
                    key={entry.snapshotId}
                    entry={entry}
                    environments={configuredEnvironments}
                    storages={configuredStorages}
                    open={openSnapshots.has(entry.snapshotId)}
                    reload={reload}
                  />
                ))
              )}
            </tbody>
          </Table>
        </TableScroll>
      </Card>
    </div>
  );
}

function SnapshotRow({
  entry,
  environments,
  storages,
  open: isOpen,
  reload,
}: {
  entry: LibraryEntry;
  /** Undefined where the engine did not say, which is not the same as empty. */
  environments?: Set<string>;
  storages?: Set<string>;
  /** This snapshot has an inspection session, and decrypted files on disk. */
  open: boolean;
  reload: () => void;
}) {
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
    <Tr>
      {/* Not a link: Inspect is on this row and is the same destination. Two
          ways to reach one screen from one row is one of them going unnoticed. */}
      <td>
        <Row $gap={8}>
          {/* The realm is how a snapshot is opened. It was a link, then it was
              not because Inspect sat in the same row saying the same thing, and
              now Inspect has gone into the menu — so the name carries it. */}
          <Link onClick={open} title="Open this snapshot">
            {entry.realm || "(unknown)"}
          </Link>
          {isOpen ? (
            <Badge $tone="info" title="Decrypted working files are on this machine.">
              Open
            </Badge>
          ) : null}
        </Row>
      </td>
      <td>{when(entry.createdAt)}</td>
      <td>
        <Gone
          name={entry.environment}
          exists={environments && environments.has(entry.environment ?? "")}
          missing="This environment is no longer configured."
          // How the export ran is provenance, not identity. Appended to the
          // name it read as part of what the environment is called, and every
          // row said the same thing. It is on hover, and in full on the
          // snapshot's own screen.
          title={entry.executionMode ? `Captured ${describeMode(entry.executionMode)}` : undefined}
          onOpen={() => navigate({ name: "environments", select: entry.environment })}
        />
      </td>
      <Numeric>{entry.metadataReadable ? count(entry.users) : "—"}</Numeric>
      <td>
        <Completeness entry={entry} />
      </td>
      <td>
        <Encryption encrypted={entry.encrypted} />
      </td>
      <td>
        <Gone
          name={entry.storage}
          exists={storages && storages.has(entry.storage)}
          missing="This storage is no longer configured."
          note={bytes(entry.bytes)}
          onOpen={() => navigate({ name: "storage", select: entry.storage })}
        />
      </td>
      <td>
        <Right>
          <Row $gap={8}>
            {/*
              Restoring is what a snapshot is for, so it is the one action that
              looks like an action — anchoring the right edge of every row, the
              way the page's own primary sits at its top right.

              It is always a restore *of a particular snapshot*, which is why it
              starts here rather than from a rail item that could only have
              asked which one. The wizard opens on the destination when it
              arrives already knowing.
            */}
            <Button
              $variant="primary"
              onClick={() => navigate({ name: "restore", snapshotId: entry.snapshotId })}
            >
              Restore
            </Button>
            <Menu
              label={`More actions for ${entry.realm || "this snapshot"}`}
              items={[
                {
                  label: isOpen ? "Open" : "Inspect",
                  icon: <Icon name="library" />,
                  onSelect: open,
                },
                // Closing appears only when there is something to close: an
                // inspection session, holding decrypted realm material on this
                // machine until it ends.
                ...(isOpen
                  ? [
                      {
                        label: "Close snapshot",
                        icon: <Icon name="close" />,
                        onSelect: async () => {
                          await InspectAPI.close(entry.snapshotId);
                          reload();
                        },
                      },
                    ]
                  : []),
                {
                  label: "Delete",
                  icon: <Icon name="trash" />,
                  tone: "danger" as const,
                  onSelect: () => confirmDelete(entry, modal, navigate),
                },
              ]}
            />
          </Row>
        </Right>
      </td>
    </Tr>
  );
}

/**
 * A name that is a link while the thing it names still exists.
 *
 * A snapshot is a record of something that happened, not a foreign key: the
 * environment it was captured from and the storage it was written to can both
 * be renamed or removed afterwards, and the snapshot stays exactly as true as
 * it was. So the name is always shown — losing it would lose the record — and
 * only the link comes and goes.
 */
function Gone({
  name,
  exists,
  missing,
  title,
  note,
  onOpen,
}: {
  name?: string;
  /** True: still configured. False: gone. Undefined: not known either way. */
  exists?: boolean;
  missing: string;
  title?: string;
  note?: string;
  onOpen: () => void;
}) {
  if (!name) return <Small>—</Small>;
  return (
    <div>
      {exists === true ? (
        <Link title={title} onClick={onOpen}>
          {name}
        </Link>
      ) : exists === false ? (
        <Muted title={missing}>
          <Small>{`${name} (removed)`}</Small>
        </Muted>
      ) : (
        // Not known: the name, and no claim about it in either direction.
        <Small title={title}>{name}</Small>
      )}
      {note ? (
        <Muted>
          <Small>{` · ${note}`}</Small>
        </Muted>
      ) : null}
    </div>
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
  return mode === "ephemeral-clone" ? "via a clone" : "in place";
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
