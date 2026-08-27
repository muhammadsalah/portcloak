// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The navigation rail.
 *
 * Two groups: what an operator does, and what they configure. An item that
 * cannot be used yet is dimmed rather than hidden, so the shape of the tool is
 * visible from the first launch.
 */
import styled from "styled-components";

import { Icon } from "../components/Icon";
import { useShell } from "./ShellContext";
import { routeKey, type Route } from "./routes";

interface NavItem {
  key: string;
  label: string;
  route: Route;
}

const groups: { section: string; items: NavItem[] }[] = [
  {
    section: "Workspace",
    items: [
      { key: "capture", label: "Capture", route: { name: "capture" } },
      { key: "library", label: "Snapshots", route: { name: "library" } },
      { key: "restore", label: "Restore", route: { name: "restore" } },
      { key: "activity", label: "Activity", route: { name: "activity" } },
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
          {group.items.map((item) => {
            // Until there is an environment and a storage, a capture cannot
            // start; until something has been captured, there is nothing to
            // restore.
            const blocked =
              item.key === "capture" && (shell.environments === 0 || shell.storages === 0);
            const restoreBlocked = item.key === "restore" && !shell.hasSnapshots;
            const disabled = blocked || restoreBlocked;

            return (
              <Item
                key={item.key}
                $active={active === item.key}
                $disabled={disabled}
                onClick={() => {
                  if (!disabled) shell.navigate(item.route);
                }}
              >
                <ItemLabel>
                  <Icon name={item.key} />
                  <span>{item.label}</span>
                </ItemLabel>
                <Trailing item={item} />
              </Item>
            );
          })}
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

const Item = styled.div<{ $active: boolean; $disabled: boolean }>`
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 16px;
  cursor: ${(p) => (p.$disabled ? "default" : "pointer")};
  border-left: 3px solid ${(p) => (p.$active ? p.theme.color.primary : "transparent")};
  background: ${(p) => (p.$active ? p.theme.color.navActive : "transparent")};
  color: ${(p) => (p.$active ? "#fff" : p.theme.color.navText)};
  opacity: ${(p) => (p.$disabled ? 0.45 : 1)};
  user-select: none;

  &:hover {
    background: ${(p) =>
      p.$disabled
        ? p.$active
          ? p.theme.color.navActive
          : "transparent"
        : p.$active
          ? p.theme.color.navActive
          : p.theme.color.navHover};
    color: ${(p) => (p.$disabled && !p.$active ? p.theme.color.navText : "#fff")};
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
