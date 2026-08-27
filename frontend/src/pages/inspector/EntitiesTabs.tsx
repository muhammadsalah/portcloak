/**
 * Clients, keys, federations, auth flows and external dependencies.
 *
 * All five come from one `entities` call, so they share a loader. Each is a
 * table whose interesting column is the same question in different words: did
 * the secret travel with the snapshot, or is it something the destination has
 * to already have?
 */
import type { ReactNode } from "react";

import { InspectAPI, type Entities } from "../../api";
import {
  Badge,
  Card,
  CardBody,
  CardFoot,
  CardHead,
  CardTitle,
  FailureNotice,
  Mono,
  Muted,
  Notice,
  Row,
  Small,
  Spinner,
  Strong,
  Table,
  TableScroll,
} from "../../design-system";
import { useAsync } from "../../hooks/useAsync";
import type { Tab } from "./tabs";

export function EntityTab({ tab, snapshotId }: { tab: Tab; snapshotId: string }) {
  return (
    <WithEntities snapshotId={snapshotId}>
      {(entities) => {
        switch (tab) {
          case "clients":
            return <Clients entities={entities} />;
          case "keys":
            return <Keys entities={entities} />;
          case "federations":
            return <Federations entities={entities} />;
          case "flows":
            return <Flows entities={entities} />;
          default:
            return null;
        }
      }}
    </WithEntities>
  );
}

export function DependenciesTab({ snapshotId }: { snapshotId: string }) {
  return (
    <WithEntities snapshotId={snapshotId}>
      {(entities) => <Dependencies entities={entities} />}
    </WithEntities>
  );
}

function WithEntities({
  snapshotId,
  children,
}: {
  snapshotId: string;
  children: (entities: Entities) => ReactNode;
}) {
  const { state } = useAsync(() => InspectAPI.entities(snapshotId), [snapshotId]);

  if (state.status === "failed") throw state.error;
  if (state.status === "loading") return <Spinner>Reading…</Spinner>;
  if (state.value.failure) return <FailureNotice failure={state.value.failure} />;

  return <>{children(state.value)}</>;
}

/** A titled table with an optional sentence under it saying what to read it for. */
function EntityTable({
  title,
  headers,
  rows,
  note,
}: {
  title: string;
  headers: string[];
  rows: ReactNode[][];
  note?: string;
}) {
  return (
    <Card>
      <CardHead>
        <CardTitle>{title}</CardTitle>
      </CardHead>
      <TableScroll>
        <Table>
          <thead>
            <tr>
              {headers.map((header) => (
                <th key={header}>{header}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td colSpan={headers.length}>
                  <Muted>None.</Muted>
                </td>
              </tr>
            ) : (
              rows.map((row, i) => (
                <tr key={i}>
                  {row.map((cell, j) => (
                    <td key={j}>{cell}</td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </Table>
      </TableScroll>
      {note ? <CardFoot $muted>{note}</CardFoot> : null}
    </Card>
  );
}

const carried = <Badge $tone="ok">Carried</Badge>;

function Clients({ entities }: { entities: Entities }) {
  return (
    <EntityTable
      title="Clients"
      headers={["Client ID", "Protocol", "Type", "Secret", "Mappers", "Authz"]}
      rows={entities.clients.map((client) => [
        client.clientId,
        client.protocol,
        client.confidential ? "Confidential" : "Public",
        client.secretMasked ? (
          <Badge $tone="danger">Masked at source</Badge>
        ) : client.secretPresent ? (
          carried
        ) : client.confidential ? (
          <Badge $tone="warn">Not carried</Badge>
        ) : (
          <Muted>n/a</Muted>
        ),
        String(client.mappers),
        client.authorization ? "yes" : "—",
      ])}
      note="secretPresent is the column that decides whether an imported client authenticates unchanged."
    />
  );
}

function Keys({ entities }: { entities: Entities }) {
  return (
    <EntityTable
      title="Key providers"
      headers={["KID", "Provider", "Type", "Algorithm", "Use", "Active", "Private carried"]}
      rows={entities.keys.map((key) => [
        key.kid ?? "—",
        key.provider,
        key.type ?? "—",
        key.algorithm ?? "—",
        key.use ?? "—",
        key.active ? "yes" : "no",
        key.privateCarried ? carried : <Badge $tone="danger">Not carried</Badge>,
      ])}
      note="privateCarried is the token-continuity signal: tokens signed before the move stay verifiable only if the private material travelled."
    />
  );
}

function Federations({ entities }: { entities: Entities }) {
  return (
    <>
      <EntityTable
        title="User federation"
        headers={["Name", "Provider", "Connection", "Users DN", "Bind credential", "Mappers"]}
        rows={entities.federations.map((federation) => [
          federation.name,
          federation.provider,
          federation.connectionUrl ?? "—",
          federation.usersDn ?? "—",
          federation.bindCarried ? carried : <Badge $tone="danger">Not carried</Badge>,
          String(federation.mappers),
        ])}
        note="Federated users are not duplicated into the export, so the directory has to be reachable at the destination."
      />
      <EntityTable
        title="Identity providers"
        headers={["Alias", "Protocol", "Enabled", "Secret", "Mappers"]}
        rows={entities.identityProviders.map((provider) => [
          provider.alias,
          provider.protocol,
          provider.enabled ? "yes" : "no",
          provider.secretCarried ? carried : <Badge $tone="warn">Not carried</Badge>,
          String(provider.mappers),
        ])}
      />
    </>
  );
}

function Flows({ entities }: { entities: Entities }) {
  return (
    <EntityTable
      title="Authentication flows"
      headers={["Alias", "Bound as", "Executions", "Built in", "Config secret"]}
      rows={entities.flows.map((flow) => [
        flow.alias,
        flow.boundAs ?? "—",
        String(flow.executions),
        flow.builtIn ? "yes" : "no",
        flow.configSecret ? carried : "—",
      ])}
    />
  );
}

function Dependencies({ entities }: { entities: Entities }) {
  return (
    <div>
      <Notice
        tone={entities.dependencies.length ? "warn" : "info"}
        title={entities.dependencyNote}
      />
      {entities.dependencies.map((dependency, i) => (
        <Card key={i}>
          <CardBody>
            <Row>
              <Strong>{dependency.name}</Strong>
              <Badge $tone="warn">{dependency.type}</Badge>
            </Row>
            {dependency.detectedAt ? (
              <Muted>
                <Mono>{dependency.detectedAt}</Mono>
              </Muted>
            ) : null}
            {dependency.referencedBy ? (
              <div>
                <Muted>
                  <Small>{`Needed by ${dependency.referencedBy}`}</Small>
                </Muted>
              </div>
            ) : null}
            <div style={{ marginTop: 6 }}>
              <Small>{dependency.consequence}</Small>
            </div>
            <Muted>
              <Small>{dependency.action}</Small>
            </Muted>
          </CardBody>
        </Card>
      ))}
    </div>
  );
}
