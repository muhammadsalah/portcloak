// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The wizard frame: a rail of steps down the left, the current step's panel
 * beside it.
 *
 * Capture uses it. Restore lays its steps out horizontally instead — a restore
 * has a destination and a confirmation to keep in view, which the narrow panel
 * fights — so it uses `StepRail` below and renders its own body.
 */
import styled from "styled-components";

export const WizardFrame = styled.div`
  display: flex;
  background: ${(p) => p.theme.color.surface};
  border: 1px solid ${(p) => p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.base};
  min-height: 460px;
`;

export const WizardSteps = styled.div`
  width: 200px;
  min-width: 200px;
  border-right: 1px solid ${(p) => p.theme.color.border};
  padding: 16px 0;
`;

export const WizardStep = styled.div<{ $active?: boolean; $done?: boolean; $clickable?: boolean }>`
  display: flex;
  gap: 10px;
  align-items: center;
  padding: 9px 16px;
  font-size: 14px;
  color: ${(p) => (p.$active || p.$done ? p.theme.color.text : p.theme.color.textSecondary)};
  cursor: ${(p) => (p.$clickable ? "pointer" : "default")};

  ${(p) =>
    p.$active &&
    `
      background: ${p.theme.color.primarySoft};
      border-left: 3px solid ${p.theme.color.primary};
      padding-left: 13px;
    `}
`;

export const WizardPanel = styled.div`
  flex: 1;
  padding: 20px 24px;
  min-width: 0;
`;

/** The restore wizard's steps, laid out along one line above the panel. */
export const StepRail = styled.div`
  display: flex;
  gap: 20px;
  margin-bottom: 18px;
  flex-wrap: wrap;
`;

export const StepRailItem = styled.div<{ $active?: boolean }>`
  display: flex;
  gap: 8px;
  align-items: center;
  color: ${(p) => (p.$active ? p.theme.color.text : p.theme.color.textSecondary)};
`;
