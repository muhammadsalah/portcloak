// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * Settings is what PortCloak does to itself: where it keeps its files, what a
 * crashed session left running in someone else's cluster, and what is sitting
 * on this disk.
 *
 * These three panels used to sit beside the audit log, which meant the record
 * of what happened shared a screen with four buttons that make things happen.
 * They are here now and the audit screen is a record again.
 */
import { SettingsAPI } from "../../api";
import { PageSubtitle, PageTitle, Spinner, SplitWide } from "../../design-system";
import { useAsync } from "../../hooks/useAsync";
import { AboutPanel } from "./AboutPanel";
import { LocationPanel } from "./LocationPanel";
import { OrphanPanel } from "./OrphanPanel";
import { WorkingDataPanel } from "./WorkingDataPanel";

export function SettingsPage() {
  const { state, reload } = useAsync(
    () =>
      Promise.all([
        SettingsAPI.location(),
        SettingsAPI.orphans(),
        SettingsAPI.workingData(),
        SettingsAPI.about(),
      ]).then(([location, orphans, working, about]) => ({ location, orphans, working, about })),
    [],
  );

  if (state.status === "failed") throw state.error;

  return (
    <div>
      <PageTitle>Settings</PageTitle>
      <PageSubtitle>
        Where PortCloak keeps its files, and everything it is holding — here and elsewhere.
      </PageSubtitle>

      {state.status === "loading" ? (
        <Spinner>Reading your settings…</Spinner>
      ) : (
        <>
          <LocationPanel location={state.value.location} reload={() => void reload()} />
          <SplitWide>
            <OrphanPanel report={state.value.orphans} reload={() => void reload()} />
            <WorkingDataPanel working={state.value.working} reload={() => void reload()} />
          </SplitWide>
          <AboutPanel about={state.value.about} />
        </>
      )}
    </div>
  );
}
