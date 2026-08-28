// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The dropdown, drawn by the app rather than by the operating system.
 *
 * A native `<select>` can be styled down to its border and no further: the list
 * it opens is an OS menu, and it arrives in the platform's font at the
 * platform's size, ignoring every token in theme.ts. On a tool whose whole
 * interface is built to sit beside the Keycloak admin console, that menu is the
 * one surface that looks like it came from somewhere else.
 *
 * So the list is a listbox this component owns and paints. What that costs is
 * the behaviour the browser used to provide for free, which is why it is all
 * written out below rather than left to chance:
 *
 *   · the keyboard — open, move, select, dismiss, jump to first and last, and
 *     type-ahead, which is not a flourish on a list of two hundred realms;
 *   · the accessibility tree — a combobox and a listbox with a selected option,
 *     so a screen reader is told what a sighted user is shown;
 *   · dismissal — clicking away, tabbing away, or pressing Escape, each of
 *     which leaves focus somewhere sensible;
 *   · not opening off the bottom of the window, which the OS menu handled and
 *     an absolutely positioned div does not.
 *
 * Focus stays on the trigger the whole time and the active option is pointed at
 * with aria-activedescendant. Moving real focus into the list means putting it
 * back on every exit path, and every path that forgets is a keyboard trap.
 */
import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from "react";
import styled, { css } from "styled-components";

import { controlSurface } from "./Form";

export interface SelectOption {
  value: string;
  label: string;
}

/** How far the list may grow before it scrolls instead. */
const maxListHeight = 280;

export function Select({
  value,
  options,
  onChange,
  placeholder = "Select…",
  disabled = false,
  id,
  className,
  style,
  "aria-label": ariaLabel,
}: {
  value: string;
  options: SelectOption[];
  onChange: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
  id?: string;
  className?: string;
  style?: React.CSSProperties;
  "aria-label"?: string;
}) {
  const generatedId = useId();
  const baseId = id ?? generatedId;
  const listId = `${baseId}-list`;

  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [dropUp, setDropUp] = useState(false);

  const wrap = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLUListElement>(null);
  /** Type-ahead buffer, held in a ref because it must not cause a render. */
  const typed = useRef({ text: "", at: 0 });

  const selectedIndex = options.findIndex((o) => o.value === value);
  const selected = selectedIndex >= 0 ? options[selectedIndex] : undefined;

  const openList = useCallback(() => {
    if (disabled || options.length === 0) return;
    // The active option starts where the value is, so opening and pressing
    // Enter changes nothing — the same promise the native control makes.
    setActive(selectedIndex >= 0 ? selectedIndex : 0);
    setOpen(true);
  }, [disabled, options.length, selectedIndex]);

  const close = useCallback((focusTrigger = true) => {
    setOpen(false);
    if (focusTrigger) trigger.current?.focus();
  }, []);

  const choose = useCallback(
    (index: number) => {
      const option = options[index];
      if (!option) return;
      onChange(option.value);
      close();
    },
    [close, onChange, options],
  );

  // Which way the list opens is decided from where the trigger actually is,
  // once, at the moment it opens. A list that would run off the bottom of the
  // window opens upwards instead — the behaviour the OS menu had and an
  // absolutely positioned element does not.
  useLayoutEffect(() => {
    if (!open) return;
    const box = trigger.current?.getBoundingClientRect();
    if (!box) return;
    const below = window.innerHeight - box.bottom;
    setDropUp(below < Math.min(maxListHeight, options.length * 36 + 16) && box.top > below);
  }, [open, options.length]);

  // Keep the active option in view when the keyboard moves past the edge of a
  // list that is scrolling.
  useEffect(() => {
    if (!open) return;
    const node = list.current?.querySelector<HTMLElement>(`[data-index="${active}"]`);
    node?.scrollIntoView({ block: "nearest" });
  }, [active, open]);

  // A click anywhere else dismisses, and pointerdown rather than click so a
  // press that starts outside is not still holding the list open while the
  // button under it is being pressed.
  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      if (!wrap.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open]);

  const typeAhead = useCallback(
    (key: string) => {
      const now = Date.now();
      // A pause resets the buffer, so "co" then later "b" searches for "b"
      // rather than for "cob".
      typed.current.text = now - typed.current.at > 700 ? key : typed.current.text + key;
      typed.current.at = now;
      const needle = typed.current.text.toLowerCase();
      const found = options.findIndex((o) => o.label.toLowerCase().startsWith(needle));
      if (found < 0) return;
      if (open) setActive(found);
      else onChange(options[found].value);
    },
    [onChange, open, options],
  );

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (disabled) return;

    if (!open) {
      switch (event.key) {
        case "Enter":
        case " ":
        case "ArrowDown":
        case "ArrowUp":
          event.preventDefault();
          openList();
          return;
        default:
          if (event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) {
            event.preventDefault();
            typeAhead(event.key);
          }
          return;
      }
    }

    switch (event.key) {
      case "Escape":
        event.preventDefault();
        close();
        return;
      case "Tab":
        // Tab commits nothing and lets focus move on, which is what the native
        // control does once its menu is closed.
        setOpen(false);
        return;
      case "Enter":
      case " ":
        event.preventDefault();
        choose(active);
        return;
      case "ArrowDown":
        event.preventDefault();
        setActive((i) => Math.min(options.length - 1, i + 1));
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
        setActive(options.length - 1);
        return;
      default:
        if (event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) {
          event.preventDefault();
          typeAhead(event.key);
        }
    }
  };

  return (
    <Wrap ref={wrap} className={className} style={style}>
      <Trigger
        ref={trigger}
        id={baseId}
        type="button"
        role="combobox"
        aria-controls={listId}
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-activedescendant={open ? `${baseId}-option-${active}` : undefined}
        aria-label={ariaLabel}
        disabled={disabled}
        $open={open}
        $placeholder={!selected}
        onClick={() => (open ? close() : openList())}
        onKeyDown={onKeyDown}
      >
        <TriggerLabel>{selected?.label ?? placeholder}</TriggerLabel>
        <Chevron aria-hidden="true" viewBox="0 0 12 12">
          <path d="M2.2 4.4 6 8.2l3.8-3.8" fill="none" stroke="currentColor" strokeWidth="1.5" />
        </Chevron>
      </Trigger>

      {open ? (
        <List ref={list} id={listId} role="listbox" aria-labelledby={baseId} $up={dropUp}>
          {options.map((option, index) => (
            <Option
              key={option.value}
              id={`${baseId}-option-${index}`}
              data-index={index}
              role="option"
              aria-selected={option.value === value}
              $active={index === active}
              $selected={option.value === value}
              // Hovering moves the active option, so the keyboard and the mouse
              // never disagree about which row is highlighted.
              onPointerMove={() => setActive(index)}
              onClick={() => choose(index)}
            >
              <Tick aria-hidden="true" viewBox="0 0 12 12" $shown={option.value === value}>
                <path
                  d="M2.5 6.3 4.8 8.6 9.5 3.9"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.6"
                />
              </Tick>
              <OptionLabel>{option.label}</OptionLabel>
            </Option>
          ))}
        </List>
      ) : null}
    </Wrap>
  );
}

/**
 * The element a layout sizes.
 *
 * Exported because the toolbar used to size these by tag name — `select { … }`
 * — and there is no longer a select to name. A component selector is the
 * replacement that cannot go quietly stale: rename or remove this and the
 * toolbar stops compiling rather than silently losing its filter widths.
 */
export const SelectRoot = styled.div`
  position: relative;
  display: inline-block;
  width: 100%;
`;

const Wrap = SelectRoot;

const Trigger = styled.button<{ $open: boolean; $placeholder: boolean }>`
  font-family: ${(p) => p.theme.font.body};
  ${controlSurface}
  display: flex;
  align-items: center;
  gap: 8px;
  text-align: left;
  cursor: pointer;
  border: 1px solid ${(p) => (p.$open ? p.theme.color.primary : p.theme.color.border)};
  border-bottom-color: ${(p) => (p.$open ? p.theme.color.primary : p.theme.color.borderInput)};
  background: ${(p) => p.theme.color.surface};
  color: ${(p) => (p.$placeholder ? p.theme.color.textMuted : p.theme.color.text)};

  &:hover:not(:disabled) {
    border-color: ${(p) => p.theme.color.borderInput};
  }

  &:focus-visible {
    outline: none;
    border-color: ${(p) => p.theme.color.primary};
    box-shadow: inset 0 -1px 0 0 ${(p) => p.theme.color.primary};
  }

  &:disabled {
    cursor: not-allowed;
    background: ${(p) => p.theme.color.surfaceSubtle};
    color: ${(p) => p.theme.color.textMuted};
  }
`;

const TriggerLabel = styled.span`
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
`;

const Chevron = styled.svg`
  width: 12px;
  height: 12px;
  flex: none;
  color: ${(p) => p.theme.color.textSecondary};
`;

const List = styled.ul<{ $up: boolean }>`
  position: absolute;
  left: 0;
  ${(p) =>
    p.$up
      ? css`
          bottom: calc(100% + 4px);
        `
      : css`
          top: calc(100% + 4px);
        `}
  z-index: ${(p) => p.theme.z.dropdown};
  min-width: 100%;
  max-width: min(420px, 90vw);
  max-height: ${maxListHeight}px;
  overflow-y: auto;
  margin: 0;
  padding: 4px;
  list-style: none;
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: 8px;
  /* Deeper than a card and shallower than the modal, which is the order these
     three surfaces sit in. */
  box-shadow: 0 6px 20px rgba(3, 3, 3, 0.16);
`;

const Option = styled.li<{ $active: boolean; $selected: boolean }>`
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-radius: 5px;
  font-size: 14px;
  line-height: 1.4;
  cursor: pointer;
  color: ${(p) => p.theme.color.text};
  background: ${(p) =>
    p.$active ? p.theme.color.rowHover : p.$selected ? p.theme.color.rowSelected : "transparent"};
  font-weight: ${(p) => (p.$selected ? 500 : 400)};
`;

const OptionLabel = styled.span`
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
`;

/*
 * The tick keeps its space whether or not it is drawn, so the labels line up
 * with each other rather than shifting by the width of a tick when the
 * selection moves.
 */
const Tick = styled.svg<{ $shown: boolean }>`
  width: 12px;
  height: 12px;
  flex: none;
  color: ${(p) => p.theme.color.primary};
  visibility: ${(p) => (p.$shown ? "visible" : "hidden")};
`;
