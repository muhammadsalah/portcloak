/**
 * What a storage backend really holds.
 *
 * Two tables: the snapshots PortCloak recognises, and everything else that is
 * under the same prefix. The second one is shown rather than hidden — a
 * mistyped prefix looks exactly like an empty one.
 */
import { SnapshotAPI } from "../../api";
import { useNavigate } from "../../app/ShellContext";
import {
  Badge,
  Card,
  CardHead,
  CardTitle,
  Link,
  Mono,
  Muted,
  Notice,
  Numeric,
  NumericHeader,
  Small,
  Spinner,
  Table,
  TableScroll,
  Tr,
} from "../../design-system";
import { useAsync } from "../../hooks/useAsync";
import { bytes, count, when } from "../../utils/format";

export function StorageBrowser({ storage }: { storage: string }) {
  const navigate = useNavigate();
  const { state } = useAsync(() => SnapshotAPI.browse(storage), [storage]);

  if (state.status === "failed") throw state.error;
  if (state.status === "loading") return <Spinner>Reading storage…</Spinner>;

  const result = state.value;

  return (
    <div>
      <Notice
        tone={result.status.reachable ? "info" : "danger"}
        title={`${result.storage} · ${result.status.kind}`}
        body={result.note}
      />

      <Card>
        <CardHead>
          <CardTitle>Snapshots</CardTitle>
        </CardHead>
        <TableScroll>
          <Table>
            <thead>
              <tr>
                <th>Realm</th>
                <th>Captured</th>
                <NumericHeader>Users</NumericHeader>
                <th>Encryption</th>
                <th>Object</th>
                <NumericHeader>Size</NumericHeader>
              </tr>
            </thead>
            <tbody>
              {result.snapshots.map((snapshot) => (
                <Tr key={snapshot.snapshotId} $selectable>
                  <td>
                    <Link
                      onClick={() =>
                        navigate({
                          name: "inspect",
                          storage: result.storage,
                          bundleKey: snapshot.bundleKey,
                          snapshotId: snapshot.snapshotId,
                        })
                      }
                    >
                      {snapshot.realm}
                    </Link>
                  </td>
                  <td>{when(snapshot.createdAt)}</td>
                  <Numeric>{snapshot.metadataReadable ? count(snapshot.users) : "—"}</Numeric>
                  <td>
                    {snapshot.encrypted ? (
                      <Badge $tone="neutral">Encrypted</Badge>
                    ) : (
                      <Badge $tone="danger">Unencrypted</Badge>
                    )}
                  </td>
                  <td>
                    <Muted>
                      <Mono>{snapshot.bundleKey}</Mono>
                    </Muted>
                  </td>
                  <Numeric>
                    <Muted>
                      <Small>{bytes(snapshot.bytes)}</Small>
                    </Muted>
                  </Numeric>
                </Tr>
              ))}
            </tbody>
          </Table>
        </TableScroll>
      </Card>

      {result.foreign.length > 0 ? (
        <Card>
          <CardHead>
            <CardTitle>Objects PortCloak did not write</CardTitle>
            <Muted>
              <Small>
                Shown rather than hidden — a mistyped prefix looks exactly like an empty one.
              </Small>
            </Muted>
          </CardHead>
          <TableScroll>
            <Table>
              <thead>
                <tr>
                  <th>Key</th>
                  <NumericHeader>Size</NumericHeader>
                  <th>Modified</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {result.foreign.map((object) => (
                  <tr key={object.key}>
                    <td>
                      <Mono>{object.key}</Mono>
                    </td>
                    <Numeric>
                      <Muted>
                        <Small>{bytes(object.size)}</Small>
                      </Muted>
                    </Numeric>
                    <td>
                      <Muted>
                        <Small>{when(object.modTime)}</Small>
                      </Muted>
                    </td>
                    <td>
                      <Badge $tone="neutral">Unrecognised</Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </TableScroll>
        </Card>
      ) : null}
    </div>
  );
}
