/**
 * The card, which is how every panel in the app is bounded.
 *
 * Head, body and foot are separate components rather than one component with
 * three slots, so a card can leave out the parts it does not need without
 * passing `undefined` through a prop.
 */
import styled from "styled-components";

export const Card = styled.div<{ $tone?: "warning" | "primary"; $flush?: boolean }>`
  background: ${(p) => p.theme.color.surface};
  border: 1px solid
    ${(p) =>
      p.$tone === "warning"
        ? p.theme.color.warning
        : p.$tone === "primary"
          ? p.theme.color.primary
          : p.theme.color.border};
  border-radius: ${(p) => p.theme.radius.base};
  margin-bottom: ${(p) => (p.$flush ? 0 : 16)}px;
  ${(p) => p.$tone === "primary" && `background: ${p.theme.color.primarySoft};`}
`;

export const CardHead = styled.div`
  padding: 12px 16px;
  border-bottom: 1px solid ${(p) => p.theme.color.border};
  background: ${(p) => p.theme.color.surfaceSubtle};
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
`;

export const CardTitle = styled.span`
  font-family: ${(p) => p.theme.font.heading};
  font-size: 15px;
`;

export const CardBody = styled.div<{ $muted?: boolean; $divided?: boolean }>`
  padding: 16px;
  ${(p) => p.$muted && `color: ${p.theme.color.textSecondary};`}
  ${(p) => p.$divided && `border-top: 1px solid ${p.theme.color.border};`}
`;

export const CardFoot = styled.div<{ $muted?: boolean }>`
  padding: 12px 16px;
  border-top: 1px solid ${(p) => p.theme.color.border};
  background: ${(p) => p.theme.color.surfaceSubtle};
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  ${(p) => p.$muted && `color: ${p.theme.color.textSecondary}; font-size: 12px;`}
`;
