/** Step one: which snapshot. */
import type { LibraryEntry } from "../../api";
import {
  Badge,
  Card,
  CardHead,
  CardTitle,
  Muted,
  Numeric,
  NumericHeader,
  Small,
  Table,
  TableScroll,
  Tr,
} from "../../design-system";
import { count, when } from "../../utils/format";

export function SnapshotStep({
  entries,
  selected,
  onSelect,
}: {
  entries: LibraryEntry[];
  selected?: LibraryEntry;
  onSelect: (entry: LibraryEntry) => void;
}) {
  return (
    <Card>
      <CardHead>
        <CardTitle>Pick a snapshot</CardTitle>
      </CardHead>
      <TableScroll>
        <Table>
          <thead>
            <tr>
              <th>Realm</th>
              <th>Captured</th>
              <NumericHeader>Users</NumericHeader>
              <th>Encryption</th>
              <th>Storage</th>
            </tr>
          </thead>
          <tbody>
            {entries.length === 0 ? (
              <tr>
                <td colSpan={5}>
                  <Muted>No snapshots to restore.</Muted>
                </td>
              </tr>
            ) : (
              entries.map((entry) => (
                <Tr
                  key={entry.snapshotId}
                  $selectable
                  $selected={selected?.snapshotId === entry.snapshotId}
                  onClick={() => onSelect(entry)}
                >
                  <td>{entry.realm}</td>
                  <td>{when(entry.createdAt)}</td>
                  <Numeric>{entry.metadataReadable ? count(entry.users) : "—"}</Numeric>
                  <td>
                    {entry.encrypted ? (
                      <Badge $tone="neutral">Encrypted</Badge>
                    ) : (
                      <Badge $tone="danger">Unencrypted</Badge>
                    )}
                  </td>
                  <td>
                    <Muted>
                      <Small>{entry.storage}</Small>
                    </Muted>
                  </td>
                </Tr>
              ))
            )}
          </tbody>
        </Table>
      </TableScroll>
    </Card>
  );
}
