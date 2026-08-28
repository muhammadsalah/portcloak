// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Environments: where Keycloak runs.
 *
 * A list on the left, the editor for whatever is selected on the right. The
 * editor is a separate component holding the draft, so a rejected save can
 * leave the form exactly as it was with the reason on top of it — see the note
 * on `save` in EnvironmentEditor.
 */
import { useState } from "react";

import { ConfigAPI, type ConfigSnapshot, type Environment, type EnvironmentView } from "../../api";
import { useNavigate } from "../../app/ShellContext";
import {
  Button,
  Card,
  CardBody,
  CardHead,
  CardTitle,
  Muted,
  NoticeBox,
  NoticeTitle,
  PageHead,
  PageSubtitle,
  PageTitle,
  Small,
  Spinner,
  Split,
  Truncate,
} from "../../design-system";
import { useAsync } from "../../hooks/useAsync";
import styled from "styled-components";

import { EnvironmentEditor } from "./EnvironmentEditor";
import { kindLabel } from "./kinds";

export function EnvironmentsPage({ select }: { select?: string }) {
  const { state, reload } = useAsync(() => ConfigAPI.load(), []);

  if (state.status === "failed") throw state.error;
  if (state.status === "loading") return <Spinner>Loading configuration…</Spinner>;

  return <Environments snapshot={state.value} select={select} reload={() => void reload()} />;
}

function Environments({
  snapshot,
  select,
  reload,
}: {
  snapshot: ConfigSnapshot;
  select?: string;
  reload: () => void;
}) {
  const navigate = useNavigate();

  // A name that is no longer there — a deleted environment still in a route —
  // is nobody's selection rather than an editor full of undefined.
  const initial = snapshot.environments.find(
    (e) => e.name === (select ?? snapshot.environments[0]?.name),
  );
  const [selected, setSelected] = useState<string | undefined>(initial?.name);
  const [draft, setDraft] = useState<Environment | undefined>(initial ? { ...initial } : undefined);
  // Empty while adding: it is the name the engine is asked to replace, and a
  // new environment replaces nothing.
  const [originalName, setOriginalName] = useState(initial?.name ?? "");
  // Bumped whenever a different environment is picked, so the editor remounts
  // and drops the probe result and typed secrets belonging to the last one.
  const [editorKey, setEditorKey] = useState(0);

  const edit = (environment: EnvironmentView | null) => {
    setSelected(environment?.name);
    setDraft(environment ? { ...environment } : { name: "", kind: "local" });
    setOriginalName(environment?.name ?? "");
    setEditorKey((n) => n + 1);
  };

  return (
    <div>
      <PageHead>
        <div>
          <PageTitle>Environments</PageTitle>
          <PageSubtitle>
            Where Keycloak runs. PortCloak captures from these and restores into them.
          </PageSubtitle>
        </div>
        <Button $variant="primary" onClick={() => edit(null)}>
          Add environment
        </Button>
      </PageHead>

      {snapshot.loadProblems && snapshot.loadProblems.length > 0 ? (
        <ConfigProblems snapshot={snapshot} reload={reload} />
      ) : null}

      {snapshot.environments.length === 0 && !draft ? (
        <NothingYet snapshot={snapshot} />
      ) : (
        <Split>
          <EnvironmentList
            environments={snapshot.environments}
            selected={selected}
            onSelect={edit}
          />
          {draft ? (
            <EnvironmentEditor
              key={editorKey}
              snapshot={snapshot}
              initialDraft={draft}
              originalName={originalName}
              onSaved={(name) => navigate({ name: "environments", select: name })}
              onDeleted={() => navigate({ name: "environments" })}
            />
          ) : (
            <Placeholder />
          )}
        </Split>
      )}
    </div>
  );
}

/**
 * The first launch. An empty list under a heading says nothing about what an
 * environment is or why the tool needs one, so the empty state says both — and
 * says the part operators ask about first, which is what PortCloak does to a
 * running Keycloak.
 */
function NothingYet({ snapshot }: { snapshot: ConfigSnapshot }) {
  return (
    <Card>
      <CardBody>
        <CardTitle>No environments yet</CardTitle>
        <p>
          An environment is one place a Keycloak server runs — on this machine, on a host you reach
          over SSH, in a Docker container, or in a Kubernetes namespace. PortCloak reads from it. It
          never restarts or reconfigures the instance serving your logins.
        </p>
        <p>
          <Muted>
            <Small>
              {`Add one with the button above, or write it into ${snapshot.configFile} by hand — the file is the same one this screen edits. Credentials never go in it: each secret goes to this machine's keychain and only a handle is written to the file.`}
            </Small>
          </Muted>
        </p>
      </CardBody>
    </Card>
  );
}

/** A malformed config is shown with its line numbers rather than swallowed. */
function ConfigProblems({ snapshot, reload }: { snapshot: ConfigSnapshot; reload: () => void }) {
  return (
    <NoticeBox $tone="danger">
      <NoticeTitle>{`${snapshot.configFile} could not be loaded`}</NoticeTitle>
      <Small>
        PortCloak refuses to start with a half-parsed config rather than silently dropping entries.
        Fix these and reload.
      </Small>
      <ul style={{ fontSize: 12, margin: "8px 0 0", paddingLeft: 18 }}>
        {(snapshot.loadProblems ?? []).map((problem, i) => (
          <li key={i}>
            {problem.line > 0 ? `Line ${problem.line}: ${problem.message}` : problem.message}
          </li>
        ))}
      </ul>
      <Button
        style={{ marginTop: 10 }}
        onClick={async () => {
          await ConfigAPI.reload();
          reload();
        }}
      >
        Reload config.yaml
      </Button>
    </NoticeBox>
  );
}

function Placeholder() {
  return (
    <Card>
      <CardBody $muted>Select an environment on the left, or add one.</CardBody>
    </Card>
  );
}

function EnvironmentList({
  environments,
  selected,
  onSelect,
}: {
  environments: EnvironmentView[];
  selected?: string;
  onSelect: (environment: EnvironmentView) => void;
}) {
  return (
    <Card>
      <CardHead>
        <Muted>
          <Small>{`${environments.length} environment${environments.length === 1 ? "" : "s"}`}</Small>
        </Muted>
      </CardHead>
      {environments.map((environment) => (
        <ListRow
          key={environment.name}
          $selected={selected === environment.name}
          onClick={() => onSelect(environment)}
        >
          <div style={{ fontWeight: 500 }}>{environment.name}</div>
          <Truncate title={`${kindLabel(environment.kind)} · ${environment.target}`}>
            <Muted>
              <Small>{`${kindLabel(environment.kind)} · ${environment.target}`}</Small>
            </Muted>
          </Truncate>
          <ProbeLine environment={environment} />
        </ListRow>
      ))}
    </Card>
  );
}

/**
 * A cached "reachable" from three weeks ago is worse than no information,
 * because it is believed. Staleness is shown, not hidden.
 */
function ProbeLine({ environment }: { environment: EnvironmentView }) {
  if (!environment.lastProbe) return <NeverTested>Never tested</NeverTested>;

  const bad = environment.stale || !environment.lastProbe.ok;
  const label = environment.lastProbe.ok
    ? `Tested ${environment.probeAge}`
    : `Failed ${environment.probeAge}`;

  return (
    <ProbeText $bad={bad}>
      {`${label}${environment.lastProbe.keycloakVersion ? ` · Keycloak ${environment.lastProbe.keycloakVersion}` : ""}${environment.stale ? " — stale" : ""}`}
    </ProbeText>
  );
}

const ListRow = styled.div<{ $selected: boolean }>`
  padding: 12px 16px;
  cursor: pointer;
  border-left: 3px solid ${(p) => (p.$selected ? p.theme.color.primary : "transparent")};
  background: ${(p) => (p.$selected ? p.theme.color.primarySoft : "transparent")};
  border-bottom: 1px solid ${(p) => p.theme.color.borderSubtle};
`;

const NeverTested = styled.div`
  font-size: 12px;
  color: ${(p) => p.theme.color.textMuted};
`;

const ProbeText = styled.div<{ $bad: boolean }>`
  font-size: 12px;
  color: ${(p) => (p.$bad ? p.theme.color.danger : p.theme.color.success)};
`;
