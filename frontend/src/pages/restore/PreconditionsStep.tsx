/**
 * Step three: what has already been proved, and what this realm expects to find
 * at the destination.
 *
 * Informative only. Nothing here blocks — the operator manages these
 * environments and is assumed to know what is deployed where.
 */
import styled from "styled-components";

import type { Plan } from "../../api";
import {
  Badge,
  Card,
  CardBody,
  CardHead,
  CardTitle,
  FailureNotice,
  Mono,
  Muted,
  Notice,
  NoticeBox,
  NoticeTitle,
  Row,
  Small,
  Spinner,
} from "../../design-system";

export function PreconditionsStep({
  plan,
  planning,
}: {
  plan: Plan | undefined;
  planning: boolean;
}) {
  if (planning) return <Spinner>Reading the destination…</Spinner>;
  if (!plan) return <Spinner>Preparing…</Spinner>;
  if (plan.failure) return <FailureNotice failure={plan.failure} />;
  if (plan.blocked) {
    return (
      <Notice
        tone="danger"
        title="This snapshot cannot be restored"
        body={plan.blockedNote ?? ""}
      />
    );
  }

  const pre = plan.preconditions;

  return (
    <div>
      <Card>
        <CardHead>
          <CardTitle>Already passed</CardTitle>
        </CardHead>
        <CardBody>
          <Passed>✓ Integrity verified — every artifact matches what was sealed</Passed>
          {pre.decrypted ? (
            <Passed>✓ Decrypted with the key supplied</Passed>
          ) : (
            <Row>
              <Muted>
                <Small>· This snapshot is not encrypted, so nothing needed decrypting</Small>
              </Muted>
            </Row>
          )}
        </CardBody>
      </Card>

      <Card>
        <CardHead>
          <CardTitle>What this realm expects to find</CardTitle>
          {pre.checked ? null : <Badge $tone="warn">Not checked</Badge>}
        </CardHead>
        <CardBody>
          <p style={{ marginTop: 0 }}>
            <Muted>
              <Small>{pre.summary}</Small>
            </Muted>
          </p>

          {pre.dependencies.map((dependency, i) => (
            <NoticeBox key={i} $tone="warn" style={{ marginBottom: 8 }}>
              <NoticeTitle>{`${dependency.name} — ${dependency.type}`}</NoticeTitle>
              {dependency.detectedAt ? (
                <div>
                  <Mono>{dependency.detectedAt}</Mono>
                </div>
              ) : null}
              <Small>{dependency.consequence}</Small>
              <div>
                <Muted>
                  <Small>{dependency.action}</Small>
                </Muted>
              </div>
            </NoticeBox>
          ))}

          <Notice
            tone="info"
            title="This step is informative and does not block"
            body="Nothing here is checked off and Next stays enabled. You manage these environments and are assumed to know what is deployed where."
          />
        </CardBody>
      </Card>
    </div>
  );
}

const Passed = styled.div`
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  color: ${(p) => p.theme.color.success};
`;
