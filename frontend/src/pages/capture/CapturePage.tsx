// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The capture wizard.
 *
 * Five steps, and none of them can be jumped to: each depends on the answer to
 * the one before it. The state for all five lives here, because the review step
 * has to describe every decision the other four made.
 */
import { useState } from "react";

import {
  CaptureAPI,
  KeysAPI,
  type CaptureOptions,
  type KeyRecipient,
  type ProbeResult,
  type WizardDefaults,
} from "../../api";
import { useNavigate } from "../../app/ShellContext";
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
  Spinner,
  StepMark,
  WizardFrame,
  WizardPanel,
  WizardStep,
  WizardSteps,
} from "../../design-system";
import { useAsync } from "../../hooks/useAsync";
import { OptionsStep } from "./OptionsStep";
import { RealmsStep } from "./RealmsStep";
import { ReviewStep } from "./ReviewStep";
import { SourceStep } from "./SourceStep";
import { StorageStep } from "./StorageStep";
import { kindLabel, type CaptureDraft, type Step } from "./draft";

const steps: { key: Step; label: string }[] = [
  { key: "source", label: "Source & probe" },
  { key: "realms", label: "Realms" },
  { key: "options", label: "Options" },
  { key: "storage", label: "Storage sink" },
  { key: "review", label: "Review & run" },
];

export function CapturePage() {
  const { state } = useAsync(
    () =>
      Promise.all([
        CaptureAPI.defaults(),
        // Offered by name rather than as a public key to paste. A key PortCloak
        // already holds is the one an operator will actually be able to restore
        // with.
        KeysAPI.recipients().catch(() => [] as KeyRecipient[]),
      ]).then(([defaults, storedKeys]) => ({ defaults, storedKeys })),
    [],
  );

  if (state.status === "failed") throw state.error;
  if (state.status === "loading") return <Spinner>Loading environments…</Spinner>;

  return <Capture defaults={state.value.defaults} storedKeys={state.value.storedKeys} />;
}

function Capture({
  defaults,
  storedKeys,
}: {
  defaults: WizardDefaults;
  storedKeys: KeyRecipient[];
}) {
  const navigate = useNavigate();

  const [step, setStep] = useState<Step>("source");
  const [probe, setProbe] = useState<ProbeResult | undefined>();
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState<string | undefined>();

  const [draft, setDraft] = useState<CaptureDraft>({
    environment: defaults.environments[0]?.name ?? "",
    realmsNote: "",
    discoveredRealms: [],
    realmsDiscovered: false,
    realms: [],
    manualRealm: "",
    storage: defaults.defaultStorage || defaults.storages[0]?.name || "",
    usersMode: defaults.preferences.usersMode ?? "different_files",
    usersPerFile: defaults.preferences.usersPerFile ?? 1000,
    verify: defaults.preferences.verifyByDefault !== false,
    detectDependencies: defaults.preferences.verifyByDefault !== false,
    encrypt: defaults.preferences.encryptByDefault !== false,
    encryptionMode: "passphrase",
    passphrase: "",
    recipients: [],
    rememberPassphraseAs: "",
    acknowledgedUnencrypted: false,
  });

  const update = (patch: Partial<CaptureDraft>) =>
    setDraft((previous) => ({ ...previous, ...patch }));

  const index = steps.findIndex((s) => s.key === step);
  const isLast = step === "review";
  const canAdvance = advanceable(step, draft, probe);

  const start = async () => {
    setStarting(true);
    setError(undefined);

    const options: CaptureOptions = {
      environment: draft.environment,
      realms: draft.realms,
      storage: draft.storage,
      usersMode: draft.usersMode,
      usersPerFile: draft.usersPerFile,
      verify: draft.verify,
      detectDependencies: draft.detectDependencies,
      encrypt: draft.encrypt,
      encryptionMode: draft.encryptionMode,
      passphrase: draft.passphrase,
      recipients: draft.recipients,
      acknowledgedUnencrypted: draft.acknowledgedUnencrypted,
    };

    // The passphrase is remembered before the capture starts, not after: a
    // capture that fails at upload was still sealed with this passphrase, and a
    // snapshot half-written to storage is exactly the one nobody wants to find
    // they cannot open.
    if (draft.encrypt && draft.encryptionMode === "passphrase" && draft.rememberPassphraseAs) {
      const failure = await KeysAPI.savePassphrase(
        draft.rememberPassphraseAs,
        draft.passphrase,
        `Remembered while capturing ${draft.realms.join(", ")}.`,
      );
      if (failure) {
        setStarting(false);
        setError(
          `The passphrase could not be remembered: ${failure.message} Nothing was captured.`,
        );
        return;
      }
    }

    const result = await CaptureAPI.start(options);
    setStarting(false);
    if (result.failure) {
      setError(result.failure.message + (result.failure.hint ? ` ${result.failure.hint}` : ""));
      return;
    }
    navigate({ name: "activity" });
  };

  return (
    <div>
      <PageTitle>Capture snapshot</PageTitle>
      <PageSubtitle>{subtitle(defaults, draft)}</PageSubtitle>

      {error ? <Notice tone="danger" title="The capture did not start" body={error} /> : null}

      <WizardFrame>
        <WizardSteps>
          {steps.map((entry, i) => (
            <WizardStep
              key={entry.key}
              $active={i === index}
              $done={i < index}
              $clickable={i <= index}
              onClick={() => {
                // Steps already passed can be revisited; later ones cannot be
                // jumped to, because each depends on the one before.
                if (i <= index) setStep(entry.key);
              }}
            >
              <StepMark $state={i < index ? "done" : i === index ? "active" : undefined}>
                {i < index ? "✓" : i + 1}
              </StepMark>
              {entry.label}
            </WizardStep>
          ))}
        </WizardSteps>

        <WizardPanel>
          {step === "source" && (
            <SourceStep
              defaults={defaults}
              draft={draft}
              update={update}
              probe={probe}
              setProbe={setProbe}
            />
          )}
          {step === "realms" && <RealmsStep draft={draft} update={update} />}
          {step === "options" && (
            <OptionsStep
              defaults={defaults}
              draft={draft}
              update={update}
              probe={probe}
              storedKeys={storedKeys}
            />
          )}
          {step === "storage" && (
            <StorageStep defaults={defaults} draft={draft} update={update} />
          )}
          {step === "review" && (
            <ReviewStep defaults={defaults} draft={draft} storedKeys={storedKeys} />
          )}
        </WizardPanel>
      </WizardFrame>

      {/*
        Back on the left, the action that advances on the far right, exactly as
        the restore wizard and both editor screens already do it. This footer
        used to lead with the primary button and end with Cancel, so Capture was
        the one screen in the app where the eye had to travel left to go
        forwards.
      */}
      <Card style={{ marginTop: -1 }}>
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
            <Button
              $variant="primary"
              disabled={!canAdvance.ok || starting}
              onClick={() => {
                if (isLast) void start();
                else setStep(steps[index + 1].key);
              }}
            >
              {isLast ? "Start capture" : "Next"}
            </Button>
          </Row>
        </CardFoot>
      </Card>
    </div>
  );
}

function subtitle(defaults: WizardDefaults, draft: CaptureDraft): string {
  const environment = defaults.environments.find((e) => e.name === draft.environment);
  if (!environment) return "Choose where Keycloak runs and where the snapshot should go.";
  const realms = draft.realms.length ? draft.realms.join(", ") : "no realm selected";
  return `Realm ${realms} · source “${environment.name}” · ${kindLabel(environment.kind)}`;
}

/** Whether the current step has been answered, and what is missing if not. */
function advanceable(
  step: Step,
  draft: CaptureDraft,
  probe: ProbeResult | undefined,
): { ok: boolean; reason?: string } {
  switch (step) {
    case "source":
      if (!draft.environment) return { ok: false, reason: "Choose an environment." };
      if (!probe) {
        return {
          ok: false,
          reason:
            "Run Test first, so a failure is a sentence now rather than a surprise later.",
        };
      }
      if (!probe.ok) {
        return { ok: false, reason: "The probe found something that would stop a capture." };
      }
      return { ok: true };

    case "realms":
      return draft.realms.length > 0
        ? { ok: true }
        : { ok: false, reason: "Select at least one realm." };

    case "options":
      if (draft.encrypt && draft.encryptionMode === "passphrase" && !draft.passphrase) {
        return { ok: false, reason: "Enter a passphrase, or switch to recipients." };
      }
      if (draft.encrypt && draft.encryptionMode === "recipients" && draft.recipients.length === 0) {
        return { ok: false, reason: "Add at least one age recipient." };
      }
      if (!draft.encrypt && !draft.acknowledgedUnencrypted) {
        return { ok: false, reason: "Confirm that this snapshot may be written unencrypted." };
      }
      return { ok: true };

    case "storage":
      return draft.storage
        ? { ok: true }
        : { ok: false, reason: "Choose where the snapshot should go." };

    default:
      return { ok: true };
  }
}
