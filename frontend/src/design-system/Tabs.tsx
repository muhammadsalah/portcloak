/**
 * A row of tabs.
 *
 * Used three ways in the app and identical in all of them: the inspector's
 * sections, the kind of an environment or storage being edited, and the kind of
 * a key being created. The component takes the items and the current one and
 * calls back — it does not own which tab is showing, because in every one of
 * those cases the surrounding screen already does.
 */
import styled from "styled-components";

export const TabBar = styled.div`
  display: flex;
  gap: 4px;
  border-bottom: 1px solid ${(p) => p.theme.color.border};
  margin-bottom: 16px;
  overflow-x: auto;
`;

export const Tab = styled.div<{ $active?: boolean }>`
  padding: 10px 14px;
  cursor: pointer;
  border-bottom: 3px solid ${(p) => (p.$active ? p.theme.color.primary : "transparent")};
  color: ${(p) => (p.$active ? p.theme.color.primary : p.theme.color.textSecondary)};
  white-space: nowrap;
  font-size: 14px;
  font-weight: ${(p) => (p.$active ? 500 : 400)};

  &:hover {
    color: ${(p) => (p.$active ? p.theme.color.primary : p.theme.color.text)};
  }
`;

export interface TabItem<K extends string> {
  key: K;
  label: string;
}

export function Tabs<K extends string>({
  items,
  active,
  onSelect,
}: {
  items: readonly TabItem<K>[];
  active: K;
  onSelect: (key: K) => void;
}) {
  return (
    <TabBar>
      {items.map((item) => (
        <Tab key={item.key} $active={item.key === active} onClick={() => onSelect(item.key)}>
          {item.label}
        </Tab>
      ))}
    </TabBar>
  );
}

/** The trail back to where this screen was opened from. */
export const Breadcrumb = styled.div`
  font-size: 12px;
  color: ${(p) => p.theme.color.textSecondary};
  margin-bottom: 8px;
`;
