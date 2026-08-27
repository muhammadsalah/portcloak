/**
 * Step four: which strategy, and what it would do to the live realm.
 *
 * The dry run is recomputed whenever the strategy changes, because the counts
 * are the whole argument for one strategy over another.
 */
import styled from "styled-components";

import type { Plan, Strategy } from "../../api";
import {
  Badge,
  Card,
  CardBody,
  CardFoot,
  CardHead,
  CardTitle,
  Input,
  Muted,
  Notice,
  NoticeBox,
  NoticeTitle,
  Numeric,
  NumericHeader,
  Row,
  Small,
  Spinner,
  Strong,
  Table,
  TableScroll,
} from "../../design-system";
import { count } from "../../utils/format";

export function StrategyStep({
  strategies,
  strategy,
  plan,
  planning,
  realm,
  environment,
  confirmRealm,
  onConfirmRealm,
  onStrategy,
}: {
  strategies: Strategy[];
  strategy: string;
  plan: Plan | undefined;
  planning: boolean;
  realm?: string;
  environment: string;
  confirmRealm: string;
  onConfirmRealm: (value: string) => void;
  onStrategy: (strategy: string) => void;
}) {
  if (planning) return <Spinner>Computing the dry run…</Spinner>;
  if (!plan) return <Spinner>Preparing…</Spinner>;

  return (
    <div>
      <Choices>
        {strategies.map((option) => (
          <Choice
            key={option.value}
            $chosen={strategy === option.value}
            onClick={() => onStrategy(option.value)}
          >
            <CardBody>
              <Row>
                <input type="radio" checked={strategy === option.value} readOnly style={{ width: "auto" }} />
                <Strong>{option.label}</Strong>
                {option.needsAdminApi ? <Badge $tone="info">Admin API</Badge> : null}
              </Row>
              <div style={{ marginTop: 6 }}>
                <Muted>
                  <Small>{option.description}</Small>
                </Muted>
              </div>
            </CardBody>
          </Choice>
        ))}
      </Choices>

      {!plan.dryRun.available ? (
        <Notice tone="warn" title="No preview is available" body={plan.dryRun.unavailable ?? ""} />
      ) : (
        <>
          <Card>
            <CardHead>
              <div>
                <CardTitle>Dry run against the live realm</CardTitle>
                <Muted>
                  <Small style={{ marginLeft: 8 }}>
                    {`computed for ${plan.dryRun.strategy} · nothing has been written`}
                  </Small>
                </Muted>
              </div>
            </CardHead>
            <TableScroll>
              <Table>
                <thead>
                  <tr>
                    <th>Category</th>
                    <NumericHeader>Create</NumericHeader>
                    <NumericHeader>Overwrite</NumericHeader>
                    <NumericHeader>Leave alone</NumericHeader>
                    <th>Note</th>
                  </tr>
                </thead>
                <tbody>
                  {plan.dryRun.categories.map((row) => (
                    <tr key={row.category}>
                      <td>{row.category}</td>
                      <Created>{row.create ? count(row.create) : "0"}</Created>
                      <Overwritten>{row.overwrite ? count(row.overwrite) : "0"}</Overwritten>
                      <Numeric>
                        <Muted>{count(row.leaveAlone)}</Muted>
                      </Numeric>
                      <Note $level={row.noteLevel}>{row.note ?? ""}</Note>
                    </tr>
                  ))}
                </tbody>
              </Table>
            </TableScroll>
            <CardFoot $muted>{`${plan.dryRun.summary} ${plan.dryRun.caveat}`}</CardFoot>
          </Card>

          {plan.confirmationRequired ? (
            <NoticeBox $tone="danger">
              <NoticeTitle>
                {`Overwriting ${realm} replaces the realm already on ${environment}`}
              </NoticeTitle>
              <div style={{ marginBottom: 8 }}>
                <Small>
                  This is destructive and cannot be undone. Type the realm name to confirm.
                </Small>
              </div>
              <Input
                type="text"
                placeholder={realm ?? ""}
                value={confirmRealm}
                onChange={(e) => onConfirmRealm(e.target.value)}
              />
            </NoticeBox>
          ) : null}
        </>
      )}
    </div>
  );
}

const Choices = styled.div`
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin-bottom: 16px;
`;

const Choice = styled(Card)<{ $chosen: boolean }>`
  margin: 0;
  cursor: pointer;
  ${(p) =>
    p.$chosen &&
    `
      border-color: ${p.theme.color.primary};
      background: ${p.theme.color.primarySoft};
    `}
`;

const Created = styled(Numeric)`
  color: ${(p) => p.theme.color.success};
`;

const Overwritten = styled(Numeric)`
  color: ${(p) => p.theme.color.warning};
`;

const Note = styled.td<{ $level?: string }>`
  font-size: 12px;
  color: ${(p) =>
    p.$level === "warning"
      ? p.theme.color.danger
      : p.$level === "caution"
        ? p.theme.color.warning
        : p.theme.color.textSecondary};
`;
