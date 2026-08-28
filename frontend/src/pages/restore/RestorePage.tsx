// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The restore wizard.
 *
 * Whole-realm import; there is no cherry-picking. The order of the steps is the
 * order of the guarantees: the snapshot is opened and verified before a
 * destination is contacted at all, and the dry run is computed before anything
 * is written.
 */
import { useState } from "react";

import {
  InspectAPI,
  KeysAPI,
  RestoreAPI,
  SnapshotAPI,
  type EnvironmentView,
  type LibraryEntry,
  type Plan,
  type Strategy,
} from "../../api";
import { useNavigate } from "../../app/ShellContext";
import { noKey, type SnapshotKey } from "../../components/SnapshotKeyFields";
import {
  Button,
  Card,
  CardFoot,
  Muted,
  Notice,
  PageSubtitle,
  PageTitle,
  Row,
  Small,
  StepMark,
  StepRail,
  StepRailItem,
  Spinner,
} from "../../design-system";
import { useAsync } from "../../hooks/useAsync";
import { ApplyStep } from "./ApplyStep";
import { DestinationStep } from "./DestinationStep";
import { PreconditionsStep } from "./PreconditionsStep";
import { SnapshotStep } from "./SnapshotStep";
import { StrategyStep } from "./StrategyStep";
import { advanceable, steps, type Step } from "./wizard";

interface Loaded {
  entries: LibraryEntry[];
  destinations: EnvironmentView[];
  strategies: Strategy[];
  outOfScope: string[];
  storedKeys: number;
  keyNote: string;
}

export function RestorePage({ snapshotId }: { snapshotId?: string }) {
  const { state } = useAsync<Loaded>(
    () =>
      Promise.all([
        SnapshotAPI.library(),
        RestoreAPI.destinations(),
        RestoreAPI.strategies(),
        RestoreAPI.outOfScopeNote(),
        KeysAPI.availability().catch(() => null),
      ]).then(([library, destinations, strategies, outOfScope, keys]) => ({
        entries: library.entries,
        destinations,
        strategies,
        outOfScope,
        storedKeys: keys?.candidates ?? 0,
        keyNote: keys?.note ?? "",
      })),
    [],
  );

  if (state.status === "failed") throw state.error;
  if (state.status === "loading") {
    return <Spinner>Loading snapshots and destinations…</Spinner>;
  }

  return <Restore loaded={state.value} snapshotId={snapshotId} />;
}

function Restore({ loaded, snapshotId }: { loaded: Loaded; snapshotId?: string }) {
  const navigate = useNavigate();

  const preselected = snapshotId
    ? loaded.entries.find((entry) => entry.snapshotId === snapshotId)
    : undefined;

  const [step, setStep] = useState<Step>(preselected ? "destination" : "snapshot");
  const [snapshot, setSnapshot] = useState<LibraryEntry | undefined>(preselected);
  /**
   * The key for an encrypted snapshot. It is carried rather than used and
   * discarded: the restore job opens the bundle again on its own, so a key that
   * only reached the pre-flight check would fail at the point of no return.
   */
  const [key, setKey] = useState<SnapshotKey>(noKey());
  const [opened, setOpened] = useState(false);
  /** The stored key that actually opened this snapshot, once one has. */
  const [unlockedWith, setUnlockedWith] = useState("");
  const [environment, setEnvironment] = useState(loaded.destinations[0]?.name ?? "");
  const [strategy, setStrategy] = useState("overwrite");
  const [plan, setPlan] = useState<Plan | undefined>();
  const [planning, setPlanning] = useState(false);
  const [confirmRealm, setConfirmRealm] = useState("");
  const [applying, setApplying] = useState(false);
  const [noTransactionTimeout, setNoTransactionTimeout] = useState(false);
  const [error, setError] = useState<string | undefined>();

  const index = steps.findIndex((s) => s.key === step);
  const isLast = step === "apply";

  const canAdvance = advanceable({
    step,
    snapshot,
    key,
    storedKeys: loaded.storedKeys,
    environment,
    plan,
    confirmRealm,
  });

  const replan = async (nextStrategy = strategy) => {
    setPlanning(true);
    setPlan(
      await RestoreAPI.plan({
        snapshotId: snapshot!.snapshotId,
        environment,
        strategy: nextStrategy,
      }),
    );
    setPlanning(false);
  };

  const advance = async () => {
    // Opening the snapshot happens once, on the way into preconditions: verify
    // and decrypt before the destination is contacted at all.
    if (step === "destination" && !opened && snapshot) {
      setPlanning(true);
      const overview = await InspectAPI.open({
        storage: snapshot.storage,
        bundleKey: snapshot.bundleKey,
        snapshotId: snapshot.snapshotId,
        passphrase: key.passphrase,
        identities: key.identities,
      });
      if (overview.failure) {
        setPlanning(false);
        setError(overview.failure.message);
        return;
      }
      setOpened(true);
      setUnlockedWith(overview.unlockedWith ?? "");
    }

    if (step === "destination" || step === "preconditions") {
      await replan();
    }

    setStep(steps[index + 1].key);
  };

  const title = snapshot
    ? `Restore ${snapshot.realm}${environment ? ` into ${environment}` : ""}`
    : "Restore a snapshot";

  return (
    <div>
      <PageTitle>{title}</PageTitle>
      <PageSubtitle $mono>
        {snapshot
          ? `${snapshot.storage} / ${snapshot.bundleKey}${environment ? ` → ${environment}` : ""}`
          : "Whole-realm import. There is no cherry-picking."}
      </PageSubtitle>

      <StepRail>
        {steps.map((entry, i) => (
          <StepRailItem key={entry.key} $active={i === index}>
            <StepMark $state={i < index ? "done" : i === index ? "active" : undefined}>
              {i < index ? "✓" : i + 1}
            </StepMark>
            {entry.label}
          </StepRailItem>
        ))}
      </StepRail>

      {error ? <Notice tone="danger" title="The restore did not start" body={error} /> : null}

      {step === "snapshot" && (
        <SnapshotStep
          entries={loaded.entries}
          selected={snapshot}
          onSelect={(entry) => {
            setSnapshot(entry);
            setKey(noKey());
            setOpened(false);
            setPlan(undefined);
            setError(undefined);
          }}
        />
      )}

      {step === "destination" && (
        <DestinationStep
          snapshot={snapshot}
          destinations={loaded.destinations}
          environment={environment}
          onEnvironment={(name) => {
            setEnvironment(name);
            setPlan(undefined);
          }}
          planning={planning}
          storedKeys={loaded.storedKeys}
          keyNote={loaded.keyNote}
          unlockedWith={unlockedWith}
          keyValue={key}
          onKey={(next) => {
            setKey(next);
            // A changed key has to be proven against the bundle again, or Apply
            // would send one the pre-flight check never saw.
            setOpened(false);
            setUnlockedWith("");
            setError(undefined);
          }}
        />
      )}

      {step === "preconditions" && <PreconditionsStep plan={plan} planning={planning} />}

      {step === "strategy" && (
        <StrategyStep
          strategies={loaded.strategies}
          strategy={strategy}
          plan={plan}
          planning={planning}
          realm={snapshot?.realm}
          environment={environment}
          confirmRealm={confirmRealm}
          onConfirmRealm={setConfirmRealm}
          onStrategy={(next) => {
            setStrategy(next);
            setConfirmRealm("");
            void replan(next);
          }}
        />
      )}

      {step === "apply" && (
        <ApplyStep
          realm={snapshot?.realm}
          environment={environment}
          strategy={strategy}
          outOfScope={loaded.outOfScope}
          applying={applying}
          noTransactionTimeout={noTransactionTimeout}
          onNoTransactionTimeout={setNoTransactionTimeout}
          onApply={async () => {
            setApplying(true);
            setError(undefined);
            const result = await RestoreAPI.apply({
              snapshotId: snapshot!.snapshotId,
              storage: snapshot!.storage,
              bundleKey: snapshot!.bundleKey,
              realm: snapshot!.realm,
              environment,
              strategy,
              passphrase: key.passphrase,
              identities: key.identities,
              confirmRealm,
              noTransactionTimeout,
            });
            setApplying(false);
            if (result.failure) {
              setError(
                result.failure.message + (result.failure.hint ? ` ${result.failure.hint}` : ""),
              );
              return;
            }
            // Straight to Activity, with no dialog in between. The import's
            // progress is the answer to "did it start", and a modal saying so
            // in front of the screen already showing it is one dismissal
            // between the operator and the thing they want to watch.
            navigate({ name: "activity" });
          }}
        />
      )}

      <Card>
        <CardFoot>
          {index > 0 ? (
            <Button onClick={() => setStep(steps[index - 1].key)}>Back</Button>
          ) : (
            <span />
          )}
          <Row>
            {!canAdvance.ok && canAdvance.reason ? (
              <Muted>
                <Small>{canAdvance.reason}</Small>
              </Muted>
            ) : null}
            <Button onClick={() => navigate({ name: "library" })}>Cancel</Button>
            {isLast ? null : (
              <Button $variant="primary" disabled={!canAdvance.ok} onClick={() => void advance()}>
                Next
              </Button>
            )}
          </Row>
        </CardFoot>
      </Card>
    </div>
  );
}
