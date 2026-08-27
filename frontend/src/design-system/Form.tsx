// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The form controls.
 *
 * Every one of these is the plain HTML element with the app's styling on it, so
 * a page passes `value`, `onChange`, `placeholder` and `disabled` exactly as it
 * would to the element itself. The two that are not — Toggle and Checkbox —
 * take the boolean and the callback, because the shape they wrap is a div and a
 * pseudo-element rather than a control the browser provides.
 */
import type { ReactNode } from "react";
import styled from "styled-components";

const controlSurface = `
  font-size: 14px;
  padding: 6px 10px;
  border-radius: 3px;
  width: 100%;
`;

export const Input = styled.input`
  font-family: ${(p) => p.theme.font.body};
  ${controlSurface}
  border: 1px solid ${(p) => p.theme.color.border};
  border-bottom-color: ${(p) => p.theme.color.borderInput};
  background: ${(p) => p.theme.color.surface};
  color: ${(p) => p.theme.color.text};

  &:focus {
    outline: none;
    border-color: ${(p) => p.theme.color.primary};
    box-shadow: inset 0 -1px 0 0 ${(p) => p.theme.color.primary};
  }
`;

export const Select = styled.select`
  font-family: ${(p) => p.theme.font.body};
  ${controlSurface}
  border: 1px solid ${(p) => p.theme.color.border};
  border-bottom-color: ${(p) => p.theme.color.borderInput};
  background: ${(p) => p.theme.color.surface};
  color: ${(p) => p.theme.color.text};

  &:focus {
    outline: none;
    border-color: ${(p) => p.theme.color.primary};
    box-shadow: inset 0 -1px 0 0 ${(p) => p.theme.color.primary};
  }
`;

export const Textarea = styled.textarea`
  font-family: ${(p) => p.theme.font.body};
  ${controlSurface}
  border: 1px solid ${(p) => p.theme.color.border};
  border-bottom-color: ${(p) => p.theme.color.borderInput};
  background: ${(p) => p.theme.color.surface};
  color: ${(p) => p.theme.color.text};

  &:focus {
    outline: none;
    border-color: ${(p) => p.theme.color.primary};
    box-shadow: inset 0 -1px 0 0 ${(p) => p.theme.color.primary};
  }
`;

export const Label = styled.label`
  display: block;
  font-size: 13px;
  margin-bottom: 4px;
  color: ${(p) => p.theme.color.text};
`;

const FieldBox = styled.div`
  margin-bottom: 16px;
`;

const FieldHint = styled.div`
  font-size: 12px;
  color: ${(p) => p.theme.color.textSecondary};
  margin-top: 4px;
`;

/** A labelled control with the sentence explaining it underneath. */
export function Field({
  label,
  hint,
  children,
}: {
  label?: ReactNode;
  hint?: ReactNode;
  children: ReactNode;
}) {
  return (
    <FieldBox>
      {label !== undefined && <Label>{label}</Label>}
      {children}
      {hint ? <FieldHint>{hint}</FieldHint> : null}
    </FieldBox>
  );
}

export { FieldBox, FieldHint };

/* ── Toggle ────────────────────────────────────────────────────────────── */

const ToggleTrack = styled.div<{ $on: boolean }>`
  position: relative;
  width: 38px;
  height: 20px;
  border-radius: 10px;
  background: ${(p) => (p.$on ? p.theme.color.primary : p.theme.color.toggleOff)};
  cursor: pointer;
  flex: none;
  transition: background 0.12s;

  &::after {
    content: "";
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #fff;
    transition: transform 0.12s;
    transform: translateX(${(p) => (p.$on ? "18px" : "0")});
  }
`;

export function Toggle({ on, onChange }: { on: boolean; onChange: (value: boolean) => void }) {
  return (
    <ToggleTrack
      $on={on}
      role="switch"
      aria-checked={on}
      onClick={() => onChange(!on)}
    />
  );
}

/* ── Checkbox ──────────────────────────────────────────────────────────── */

const CheckboxRow = styled.div`
  display: flex;
  gap: 8px;
  align-items: flex-start;
  margin-bottom: 12px;

  input {
    width: auto;
    margin-top: 3px;
  }
`;

const CheckboxLabel = styled.div`
  font-size: 14px;
`;

const CheckboxHint = styled.div`
  font-size: 12px;
  color: ${(p) => p.theme.color.textSecondary};
`;

/** A checkbox with its label and the sentence that qualifies it. */
export function Checkbox({
  checked,
  label,
  hint,
  onChange,
}: {
  checked: boolean;
  label: ReactNode;
  hint?: ReactNode;
  onChange: (value: boolean) => void;
}) {
  return (
    <CheckboxRow>
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      <div style={{ flex: 1 }}>
        <CheckboxLabel>{label}</CheckboxLabel>
        {hint ? <CheckboxHint>{hint}</CheckboxHint> : null}
      </div>
    </CheckboxRow>
  );
}

/* ── Facet ─────────────────────────────────────────────────────────────── */

export const FacetGroup = styled.div`
  margin-bottom: 18px;
`;

export const FacetLabel = styled.label`
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 3px 0;
  font-size: 13px;
  cursor: pointer;

  input {
    width: auto;
  }
`;

/** The count on the right of a facet, in figures of equal width. */
export const FacetCount = styled.span`
  margin-left: auto;
  color: ${(p) => p.theme.color.textSecondary};
  font-variant-numeric: tabular-nums;
`;
