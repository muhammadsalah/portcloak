// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The row menu: the actions a row has that are not the one it is for.
 *
 * A table row that lays every action out flat gives them all the same weight,
 * and the weights are not the same — restoring a snapshot is what the row is
 * for, deleting one is irreversible, and closing an inspection session is
 * housekeeping. Flat, they also multiply: six rows of "Inspect Close Delete"
 * is eighteen links competing with six buttons.
 *
 * So the secondary actions fold behind one trigger and the primary one stays
 * out in the open beside it.
 *
 * The behaviour is the listbox's, for the same reasons set out in Select.tsx —
 * dismissal that leaves focus somewhere sensible, arrow keys, and a popup that
 * flips rather than opening off the bottom of the window. What differs is what
 * an item is: a menu item does something, so there is no selected state and no
 * value, and choosing one closes the menu before its action runs.
 */
import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import styled from "styled-components";

import type { ReactNode } from "react";

export interface MenuItem {
  label: string;
  /** Drawn at 13px on the left of the label. */
  icon?: ReactNode;
  /** Danger paints the item red: it is irreversible, and it should look it. */
  tone?: "default" | "danger";
  onSelect: () => void;
}

const maxMenuHeight = 320;

export function Menu({
  items,
  label = "More actions",
  className,
}: {
  items: MenuItem[];
  label?: string;
  className?: string;
}) {
  const baseId = useId();
  const listId = `${baseId}-menu`;

  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [dropUp, setDropUp] = useState(false);
  // Viewport coordinates, because the list is rendered into the body rather
  // than beside the trigger. Null until the trigger has been measured.
  const [at, setAt] = useState<{ right: number; top?: number; bottom?: number } | null>(null);

  const wrap = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLUListElement>(null);

  const close = useCallback((focusTrigger = true) => {
    setOpen(false);
    if (focusTrigger) trigger.current?.focus();
  }, []);

  const choose = useCallback(
    (index: number) => {
      const item = items[index];
      if (!item) return;
      // Closed first: an action that opens a modal or navigates would otherwise
      // leave this menu hanging over whatever it opened.
      close();
      item.onSelect();
    },
    [close, items],
  );

  // The list is measured against the viewport rather than the trigger's own
  // offset parent. It has to be: every table in the app sits in a box that
  // scrolls sideways, and `overflow-x: auto` computes `overflow-y` to `auto`
  // with it, so a list positioned inside one is clipped at the box's edge no
  // matter what it is stacked above. Rendered into the body at viewport
  // coordinates, there is nothing left to clip it.
  const place = useCallback(() => {
    const box = trigger.current?.getBoundingClientRect();
    if (!box) return;
    const wanted = Math.min(maxMenuHeight, items.length * 36 + 16);
    const below = window.innerHeight - box.bottom;
    const up = below < wanted && box.top > below;
    setDropUp(up);
    setAt({
      // Right-aligned to the trigger, which is what `right: 0` did while the
      // list was positioned against it.
      right: Math.max(0, window.innerWidth - box.right),
      ...(up ? { bottom: window.innerHeight - box.top + 4 } : { top: box.bottom + 4 }),
    });
  }, [items.length]);

  useLayoutEffect(() => {
    if (!open) return;
    place();
  }, [open, place]);

  // A fixed list does not travel with the page, so it is re-placed while
  // anything scrolls underneath it — including the sideways scroll of the
  // table it belongs to, which is why this listens in the capture phase.
  useEffect(() => {
    if (!open) return;
    const onMove = () => place();
    window.addEventListener("scroll", onMove, true);
    window.addEventListener("resize", onMove);
    return () => {
      window.removeEventListener("scroll", onMove, true);
      window.removeEventListener("resize", onMove);
    };
  }, [open, place]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      // The list is no longer inside the wrapper, so it has to be asked
      // separately. Without this, choosing an item would read as a click
      // outside and close the menu before the item could act on it.
      if (wrap.current?.contains(target) || list.current?.contains(target)) return;
      setOpen(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open]);

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (!open) {
      if (event.key === "Enter" || event.key === " " || event.key === "ArrowDown") {
        event.preventDefault();
        setActive(0);
        setOpen(true);
      }
      return;
    }
    switch (event.key) {
      case "Escape":
        event.preventDefault();
        close();
        return;
      case "Tab":
        setOpen(false);
        return;
      case "Enter":
      case " ":
        event.preventDefault();
        choose(active);
        return;
      case "ArrowDown":
        event.preventDefault();
        setActive((i) => Math.min(items.length - 1, i + 1));
        return;
      case "ArrowUp":
        event.preventDefault();
        setActive((i) => Math.max(0, i - 1));
        return;
      case "Home":
        event.preventDefault();
        setActive(0);
        return;
      case "End":
        event.preventDefault();
        setActive(items.length - 1);
    }
  };

  if (items.length === 0) return null;

  return (
    <Wrap ref={wrap} className={className}>
      <Trigger
        ref={trigger}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={listId}
        aria-label={label}
        title={label}
        $open={open}
        onClick={() => (open ? close() : (setActive(0), setOpen(true)))}
        onKeyDown={onKeyDown}
      >
        {/* Three dots, drawn here rather than taken from the icon set: the
            design system does not import from the components folder, and this
            is the one glyph that belongs to a control rather than to a screen. */}
        <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">
          <circle cx="8" cy="3" r="1.4" fill="currentColor" />
          <circle cx="8" cy="8" r="1.4" fill="currentColor" />
          <circle cx="8" cy="13" r="1.4" fill="currentColor" />
        </svg>
      </Trigger>

      {open && at
        ? createPortal(
            <List
              ref={list}
              id={listId}
              role="menu"
              $up={dropUp}
              style={{ right: at.right, top: at.top, bottom: at.bottom }}
            >
              {items.map((item, index) => (
                <Item
                  key={item.label}
                  role="menuitem"
                  $active={index === active}
                  $danger={item.tone === "danger"}
                  onPointerMove={() => setActive(index)}
                  onClick={() => choose(index)}
                >
                  {item.icon ? <ItemIcon>{item.icon}</ItemIcon> : null}
                  <span>{item.label}</span>
                </Item>
              ))}
            </List>,
            document.body,
          )
        : null}
    </Wrap>
  );
}

const Wrap = styled.div`
  position: relative;
  display: inline-flex;
`;

const Trigger = styled.button<{ $open: boolean }>`
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  border-radius: 6px;
  cursor: pointer;
  border: 1px solid ${(p) => (p.$open ? p.theme.color.border : "transparent")};
  background: ${(p) => (p.$open ? p.theme.color.surfaceSubtle : "transparent")};
  color: ${(p) => p.theme.color.textSecondary};

  &:hover {
    background: ${(p) => p.theme.color.surfaceSubtle};
    color: ${(p) => p.theme.color.text};
  }

  &:focus-visible {
    outline: 2px solid ${(p) => p.theme.color.primary};
    outline-offset: 1px;
  }
`;

const List = styled.ul<{ $up: boolean }>`
  /* Fixed, and placed from the trigger's viewport rect: see place(). The
     offsets arrive as inline styles because they change with every open. */
  position: fixed;
  z-index: ${(p) => p.theme.z.dropdown};
  min-width: 168px;
  max-height: ${maxMenuHeight}px;
  overflow-y: auto;
  margin: 0;
  padding: 4px;
  list-style: none;
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: 8px;
  box-shadow: 0 6px 20px rgba(3, 3, 3, 0.16);
`;

const Item = styled.li<{ $active: boolean; $danger: boolean }>`
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 8px 10px;
  border-radius: 5px;
  font-size: 14px;
  line-height: 1.4;
  white-space: nowrap;
  cursor: pointer;
  color: ${(p) => (p.$danger ? p.theme.color.danger : p.theme.color.text)};
  background: ${(p) =>
    p.$active ? (p.$danger ? p.theme.color.dangerBg : p.theme.color.rowHover) : "transparent"};
`;

const ItemIcon = styled.span`
  display: inline-flex;
  flex: none;
  color: currentColor;

  svg {
    width: 14px;
    height: 14px;
  }
`;
