/**
 * Step one: which environment, and what the probe found there.
 *
 * Nothing advances until the probe has run and passed, so a failure is a
 * sentence now rather than a surprise half an hour into an export.
 */
import { useState } from "react";

import { CaptureAPI, ConfigAPI, type ProbeResult, type WizardDefaults } from "../../api";
import { ProbePanel } from "../../components/ProbePanel";
import {
  Button,
  FailureNotice,
  Field,
  SectionTitle,
  Select,
  Spinner,
} from "../../design-system";
import { kindLabel, type CaptureDraft, type UpdateDraft } from "./draft";

export function SourceStep({
  defaults,
  draft,
  update,
  probe,
  setProbe,
}: {
  defaults: WizardDefaults;
  draft: CaptureDraft;
  update: UpdateDraft;
  probe: ProbeResult | undefined;
  setProbe: (probe: ProbeResult | undefined) => void;
}) {
  const [probing, setProbing] = useState(false);

  const test = async () => {
    setProbing(true);
    const result = await ConfigAPI.testEnvironment(draft.environment);
    setProbe(result);
    setProbing(false);
    if (result.ok) {
      const realms = await CaptureAPI.realms(draft.environment);
      update({
        discoveredRealms: realms.realms ?? [],
        realmsDiscovered: realms.discovered,
        realmsNote: realms.note,
      });
    }
  };

  return (
    <div>
      <SectionTitle>Source &amp; probe</SectionTitle>

      <Field
        label="Environment"
        hint="PortCloak reads this environment. It never restarts or reconfigures the instance serving your logins."
      >
        <Select
          value={draft.environment}
          onChange={(e) => {
            update({ environment: e.target.value, realms: [], discoveredRealms: [] });
            setProbe(undefined);
          }}
        >
          {defaults.environments.map((environment) => (
            <option key={environment.name} value={environment.name}>
              {`${environment.name} — ${kindLabel(environment.kind)} · ${environment.target}`}
            </option>
          ))}
        </Select>
      </Field>

      <Button disabled={!draft.environment || probing} onClick={() => void test()}>
        {probing ? "Testing…" : "Test connection"}
      </Button>

      {probing ? (
        <div style={{ marginTop: 12 }}>
          <Spinner>Probing…</Spinner>
        </div>
      ) : null}

      {probe?.failure ? (
        <div style={{ marginTop: 12 }}>
          <FailureNotice failure={probe.failure} />
        </div>
      ) : null}

      {probe && !probe.failure ? (
        <div style={{ marginTop: 16 }}>
          <ProbePanel facts={probe.facts} ok={probe.ok} />
        </div>
      ) : null}
    </div>
  );
}
