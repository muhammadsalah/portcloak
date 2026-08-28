// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Reading inside a snapshot.
 *
 * Getting in is the first half of this screen: a snapshot already open is
 * reused, a new one is fetched, decrypted and verified before any of it is
 * rendered, and a key is asked for only when the bundle actually needs one. The
 * second half is eight tabs over what was found.
 */
import { useCallback, useEffect, useState } from "react";

import { InspectAPI, type Overview } from "../../api";
import { useNavigate } from "../../app/ShellContext";
import type { Route } from "../../app/routes";
import type { SnapshotKey } from "../../components/SnapshotKeyFields";
import {
  Badge,
  Breadcrumb,
  Button,
  Encryption,
  FailureNotice,
  Link,
  Notice,
  PageHead,
  PageSubtitle,
  PageTitle,
  Row,
  Spinner,
  Tabs,
  useModal,
} from "../../design-system";
import { count, when } from "../../utils/format";
import { KeyPrompt } from "./KeyPrompt";
import { closeSnapshot, exportView, verify } from "./actions";
import { DependenciesTab, EntityTab } from "./EntitiesTabs";
import { OverviewTab } from "./OverviewTab";
import { SecretLedgerTab } from "./SecretLedgerTab";
import { UsersTab } from "./UsersTab";
import { tabs, type Tab } from "./tabs";

type InspectRoute = Extract<Route, { name: "inspect" }>;

/** Where the screen is on its way in. */
type Opening =
  | { status: "opening" }
  | { status: "ready"; overview: Overview }
  | { status: "closed"; confirmed: string }
  | { status: "failed"; message: string; hint?: string };

export function InspectorPage({ route }: { route: InspectRoute }) {
  const navigate = useNavigate();
  const modal = useModal();

  const [state, setState] = useState<Opening>({ status: "opening" });
  const [tab, setTab] = useState<Tab>((route.tab as Tab) ?? "overview");

  const openWith = useCallback(
    async (key: SnapshotKey): Promise<Overview | null> => {
      const result = await InspectAPI.open({
        storage: route.storage,
        bundleKey: route.bundleKey,
        snapshotId: route.snapshotId,
        passphrase: key.passphrase,
        identities: key.identities,
      });
      if (result.failure) return null;
      setState({ status: "ready", overview: result });
      return result;
    },
    [route.storage, route.bundleKey, route.snapshotId],
  );

  useEffect(() => {
    let cancelled = false;

    const open = async () => {
      // A snapshot already open is reused; a new one is fetched, decrypted and
      // verified before any of it is rendered.
      const reopened = await InspectAPI.reopen(route.snapshotId);
      if (cancelled) return;
      if (!reopened.failure) {
        setState({ status: "ready", overview: reopened });
        return;
      }

      const first = await InspectAPI.open({
        storage: route.storage,
        bundleKey: route.bundleKey,
        snapshotId: route.snapshotId,
        passphrase: "",
        identities: [],
      });
      if (cancelled) return;
      if (!first.failure) {
        setState({ status: "ready", overview: first });
        return;
      }

      const needsKey =
        first.failure.message.toLowerCase().includes("encrypted") ||
        first.failure.message.toLowerCase().includes("decrypt");
      if (!needsKey) {
        setState({
          status: "failed",
          message: first.failure.message,
          hint: first.failure.hint,
        });
        return;
      }

      // The same question the restore wizard asks, in a modal rather than on a
      // step: see components/SnapshotKeyFields.
      modal.open({
        title: "This snapshot is encrypted",
        body: (
          <KeyPrompt
            onOpen={async (key) => {
              const opened = await openWith(key);
              if (!opened) {
                setState({
                  status: "failed",
                  message: "That key did not open this snapshot.",
                  hint: "Check the passphrase, or the private key of one of the recipients it was sealed to.",
                });
              }
            }}
          />
        ),
        confirmLabel: "Open",
        // The label says what it does: declining goes back rather than leaving
        // a screen with nothing on it.
        cancelLabel: "Back to snapshots",
        onCancel: () => navigate({ name: "library" }),
      });
    };

    void open();
    return () => {
      cancelled = true;
    };
    // The route identifies the snapshot; everything else here is stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [route.storage, route.bundleKey, route.snapshotId]);

  if (state.status === "opening") {
    return <Spinner>Downloading, decrypting and verifying…</Spinner>;
  }

  if (state.status === "failed") {
    return (
      <div>
        <FailureNotice failure={{ message: state.message, hint: state.hint }} />
        <Button style={{ marginTop: 12 }} onClick={() => navigate({ name: "library" })}>
          Back to snapshots
        </Button>
      </div>
    );
  }

  if (state.status === "closed") {
    return (
      <div>
        <Notice tone="ok" title="Snapshot closed" body={state.confirmed} />
        <Button style={{ marginTop: 12 }} onClick={() => navigate({ name: "library" })}>
          Back to snapshots
        </Button>
      </div>
    );
  }

  const overview = state.overview;

  return (
    <div>
      <Breadcrumb>
        <Link onClick={() => navigate({ name: "library" })}>Snapshots</Link>
        {` / ${overview.realm} · ${when(String(overview.provenance?.finishedAt ?? ""))}`}
      </Breadcrumb>

      <PageHead>
        <Row>
          <PageTitle style={{ margin: 0 }}>{overview.realm}</PageTitle>
          <Badge $tone={overview.completeness?.verdict === "Complete" ? "ok" : "warn"}>
            {overview.completeness?.verdict ?? "—"}
          </Badge>
          <Encryption encrypted={overview.encrypted} />
          {/*
            A key used without being asked for is still named. Silence would be
            the one thing worse than the prompt it replaces.
          */}
          {overview.unlockedWith ? (
            <Badge $tone="ok">{`Opened with “${overview.unlockedWith}”`}</Badge>
          ) : null}
          {overview.integrityOk ? (
            <Badge $tone="ok">Integrity verified</Badge>
          ) : (
            <Badge $tone="danger">Integrity failed</Badge>
          )}
        </Row>
        {/*
          Restoring is what a snapshot is for, here as on the list it came from,
          so it is the filled one and it anchors the right edge. The other two
          are things done *to* the snapshot on this machine — proving it, and
          ending the session that decrypted it — and both stay outlined.
        */}
        <Row>
          <Button onClick={() => void verify(route.snapshotId, modal)}>Verify</Button>
          <Button
            $variant="danger"
            onClick={() =>
              closeSnapshot(route.snapshotId, modal, (confirmed) =>
                setState({ status: "closed", confirmed }),
              )
            }
          >
            Close snapshot
          </Button>
          <Button
            $variant="primary"
            disabled={overview.degraded}
            onClick={() => navigate({ name: "restore", snapshotId: overview.snapshotId })}
          >
            Restore
          </Button>
        </Row>
      </PageHead>

      <PageSubtitle $mono>
        {`${overview.storage} / ${overview.bundleKey} · captured from ${overview.provenance?.environmentName ?? "—"} · Keycloak ${overview.provenance?.keycloakVersion ?? "—"}`}
      </PageSubtitle>

      {overview.degraded ? (
        <Notice
          tone="danger"
          title="This snapshot did not verify"
          body={overview.degradedNote ?? ""}
        />
      ) : null}

      {!overview.encrypted ? (
        <Notice tone="danger" title="Unencrypted snapshot" body={overview.warning ?? ""} />
      ) : null}

      <Tabs items={labelled(overview)} active={tab} onSelect={setTab} />

      <TabPanel tab={tab} overview={overview} snapshotId={route.snapshotId} />
    </div>
  );
}

function TabPanel({
  tab,
  overview,
  snapshotId,
}: {
  tab: Tab;
  overview: Overview;
  snapshotId: string;
}) {
  const modal = useModal();

  switch (tab) {
    case "overview":
      return <OverviewTab overview={overview} />;
    case "users":
      return <UsersTab snapshotId={snapshotId} indexNote={overview.indexNote} />;
    case "secrets":
      return (
        <SecretLedgerTab
          snapshotId={snapshotId}
          onExport={(query) => exportView(snapshotId, "secretLedger", query, modal)}
        />
      );
    case "deps":
      return <DependenciesTab snapshotId={snapshotId} />;
    default:
      return <EntityTab tab={tab} snapshotId={snapshotId} />;
  }
}

/** The tab labels, with the counts that make them worth clicking. */
function labelled(overview: Overview) {
  return tabs.map((tab) => {
    let suffix = "";
    if (tab.key === "users") suffix = ` ${count(overview.counts?.users)}`;
    if (tab.key === "deps" && overview.dependencies?.length) {
      suffix = ` ${overview.dependencies.length}`;
    }
    if (tab.key === "secrets") suffix = ` ${overview.secretCount}`;
    return { key: tab.key, label: tab.label + suffix };
  });
}
