// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The shell: masthead across the top, navigation rail down the left, and the
 * current screen in the column beside it.
 *
 * The route switch is the whole of the router. There is no URL to parse and no
 * history to keep — this is a desktop window, and the back button an operator
 * would press does not exist.
 */
import styled from "styled-components";

import { ActivityPage } from "../pages/activity/ActivityPage";
import { AuditPage } from "../pages/audit/AuditPage";
import { CapturePage } from "../pages/capture/CapturePage";
import { EnvironmentsPage } from "../pages/environments/EnvironmentsPage";
import { InspectorPage } from "../pages/inspector/InspectorPage";
import { KeysPage } from "../pages/keys/KeysPage";
import { LibraryPage } from "../pages/library/LibraryPage";
import { RestorePage } from "../pages/restore/RestorePage";
import { SettingsPage } from "../pages/settings/SettingsPage";
import { StoragePage } from "../pages/storage/StoragePage";
import { Masthead } from "./Masthead";
import { Nav } from "./Nav";
import { useShell } from "./ShellContext";
import { ViewBoundary } from "./ViewBoundary";
import type { Route } from "./routes";

export function App() {
  const { route, nonce } = useShell();
  // Identity includes the navigation count, so going back to the screen you are
  // already on re-reads it rather than showing what was there before.
  const screen = `${identify(route)}#${nonce}`;

  return (
    <>
      <Masthead />
      <Body>
        <Nav />
        <Content>
          {/*
            The key remounts the screen when the route changes, so a page's
            loads re-run and none of its state survives into a different one.
            That is what `clear(content)` and a fresh render bought before, and
            it is what makes every page below able to hold its state in
            ordinary hooks.
          */}
          <ViewBoundary resetKey={screen}>
            <Screen key={screen} route={route} />
          </ViewBoundary>
        </Content>
      </Body>
    </>
  );
}

function Screen({ route }: { route: Route }) {
  switch (route.name) {
    case "library":
      return <LibraryPage />;
    case "capture":
      return <CapturePage />;
    case "restore":
      return <RestorePage snapshotId={route.snapshotId} />;
    case "activity":
      return <ActivityPage />;
    case "environments":
      return <EnvironmentsPage select={route.select} />;
    case "storage":
      return <StoragePage select={route.select} />;
    case "browse":
      return <StoragePage select={route.storage} browsing />;
    case "keys":
      return <KeysPage />;
    case "audit":
      return <AuditPage />;
    case "settings":
      return <SettingsPage />;
    case "inspect":
      return <InspectorPage route={route} />;
  }
}

/**
 * A route reduced to a string, so React can tell one screen from another.
 *
 * The parameters are part of it: navigating from one snapshot's inspector to
 * another's is a different screen, and reusing the mounted one would leave it
 * showing the first snapshot's tabs while it loaded the second.
 */
function identify(route: Route): string {
  switch (route.name) {
    case "restore":
      return `restore:${route.snapshotId ?? ""}`;
    case "environments":
    case "storage":
      return `${route.name}:${route.select ?? ""}`;
    case "browse":
      return `browse:${route.storage}`;
    case "inspect":
      return `inspect:${route.storage}:${route.bundleKey}:${route.snapshotId}`;
    default:
      return route.name;
  }
}

const Body = styled.div`
  display: flex;
  flex: 1;
  min-height: 0;
`;

const Content = styled.main`
  flex: 1;
  overflow-y: auto;
  padding: ${(p) => p.theme.size.contentPadding};
  min-width: 0;
`;
