// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The navigation rail.
 *
 * Two groups: what an operator does, and what they configure. Every item on it
 * goes somewhere that stands on its own — which is why there is no dimming and
 * no disabled state left here.
 *
 * The two that used to need it are reached from the thing they act on instead:
 * a capture starts from the Snapshots screen, whose first-run state stands in
 * for the table until there is an environment and a storage to capture between,
 * and a restore starts from the row of the snapshot being restored. Both were
 * items that could not act alone, and dimming them was the rail apologising for
 * offering them at all.
 */
import styled from "styled-components";

import { Icon } from "../components/Icon";
import { useShell } from "./ShellContext";
import { type Route, routeKey } from "./routes";

interface NavItem {
  key: string;
  label: string;
  route: Route;
}

const groups: { section: string; items: NavItem[] }[] = [
  {
    section: "Workspace",
    items: [
      // Activity leads, because it is the screen an operator returns to. The
      // other two start something; this one says what is happening now, which
      // is the question being asked every time the window is opened again.
      { key: "activity", label: "Activity", route: { name: "activity" } },
      { key: "library", label: "Snapshots", route: { name: "library" } },
    ],
  },
  {
    section: "Configuration",
    items: [
      { key: "environments", label: "Environments", route: { name: "environments" } },
      { key: "storage", label: "Storage", route: { name: "storage" } },
      { key: "keys", label: "Keys", route: { name: "keys" } },
      { key: "settings", label: "Settings", route: { name: "settings" } },
      { key: "audit", label: "Audit log", route: { name: "audit" } },
    ],
  },
];

export function Nav() {
  const shell = useShell();
  const active = routeKey(shell.route);

  return (
    <Rail>
      {groups.map((group) => (
        <div key={group.section}>
          <Section>{group.section}</Section>
          {group.items.map((item) => (
            <Item
              key={item.key}
              $active={active === item.key}
              onClick={() => shell.navigate(item.route)}
            >
              <ItemLabel>
                <Icon name={item.key} />
                <span>{item.label}</span>
              </ItemLabel>
              <Trailing item={item} />
            </Item>
          ))}
        </div>
      ))}
    </Rail>
  );
}

/** The count or badge on the right of an item, where the item has one. */
function Trailing({ item }: { item: NavItem }) {
  const shell = useShell();
  if (item.key === "activity" && shell.activeJobs > 0) {
    return <Badge>{shell.activeJobs}</Badge>;
  }
  if (item.key === "environments") return <Count>{shell.environments}</Count>;
  if (item.key === "storage") return <Count>{shell.storages}</Count>;
  return null;
}

const Rail = styled.nav`
  width: ${(p) => p.theme.size.navWidth};
  min-width: ${(p) => p.theme.size.navWidth};
  background: ${(p) => p.theme.color.nav};
  color: ${(p) => p.theme.color.navText};
  overflow-y: auto;
  padding-bottom: 16px;
`;

const Section = styled.div`
  font-size: 11px;
  letter-spacing: 0.6px;
  text-transform: uppercase;
  color: ${(p) => p.theme.color.textMuted};
  padding: 16px 16px 6px;
`;

const Item = styled.div<{ $active: boolean }>`
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 16px;
  cursor: pointer;
  border-left: 3px solid ${(p) => (p.$active ? p.theme.color.primary : "transparent")};
  background: ${(p) => (p.$active ? p.theme.color.navActive : "transparent")};
  color: ${(p) => (p.$active ? "#fff" : p.theme.color.navText)};
  user-select: none;

  &:hover {
    background: ${(p) => (p.$active ? p.theme.color.navActive : p.theme.color.navHover)};
    color: #fff;
  }

  /*
   * The glyph is dimmer than the label at rest and comes up to full strength
   * with it on hover and when active. A line drawing at 16px reads as noise at
   * the same weight as its text.
   */

  svg {
    flex: none;
    opacity: ${(p) => (p.$active ? 1 : 0.75)};
    color: ${(p) => (p.$active ? p.theme.color.infoBorder : "inherit")};
  }

  &:hover svg {
    opacity: 1;
  }
`;

/*
 * Icon and label travel together, so the trailing count or badge stays pinned
 * to the right edge by the item's own space-between.
 */
const ItemLabel = styled.span`
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
`;

const Count = styled.span`
  font-size: 12px;
  color: ${(p) => p.theme.color.textMuted};
`;

const Badge = styled.span`
  background: ${(p) => p.theme.color.primary};
  color: #fff;
  border-radius: ${(p) => p.theme.radius.pill};
  padding: 0 7px;
  font-size: 11px;
  line-height: 18px;
  min-width: 20px;
  text-align: center;
`;
