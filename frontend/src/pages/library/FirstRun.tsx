/**
 * What the library shows before there is anything to list.
 *
 * Two numbered cards in the order the tool needs them, and the answer to the
 * question everyone asks first — whether PortCloak needs an account of its own.
 */
import styled from "styled-components";

import type { FirstRun as FirstRunView } from "../../api";
import { useNavigate } from "../../app/ShellContext";
import { Mark, onLight } from "../../components/Logo";
import {
  Button,
  Card,
  CardBody,
  Mono,
  Muted,
  Right,
  Row,
  Small,
  StepNumber,
  Strong,
} from "../../design-system";

export function FirstRun({ firstRun }: { firstRun: FirstRunView }) {
  const navigate = useNavigate();

  return (
    <Empty>
      <EmptyMark>
        <Mark size={44} tone={onLight} />
      </EmptyMark>
      <h2>{firstRun.heading}</h2>
      <p>
        <Muted>{firstRun.body}</Muted>
      </p>

      <Cards>
        <Step
          number={1}
          current={firstRun.needsEnvironment}
          title="Add an environment"
          body={firstRun.environmentBody}
          action="Add an environment"
          onClick={() => navigate({ name: "environments" })}
        />
        <Step
          number={2}
          current={!firstRun.needsEnvironment && firstRun.needsStorage}
          title="Add a storage"
          body={firstRun.storageBody}
          action="Add a storage"
          onClick={() => navigate({ name: "storage" })}
        />
      </Cards>

      <NoAccount>
        <CardBody>
          <Row>
            <Strong>{firstRun.noAccountHeading}</Strong>
            <Right>
              <Muted>
                <Mono>{firstRun.configFile}</Mono>
              </Muted>
            </Right>
          </Row>
          <p>
            <Muted>
              <Small>{firstRun.noAccountBody}</Small>
            </Muted>
          </p>
        </CardBody>
      </NoAccount>
    </Empty>
  );
}

/** One numbered thing to do, filled in when it is this one's turn. */
function Step({
  number,
  current,
  title,
  body,
  action,
  onClick,
}: {
  number: number;
  current: boolean;
  title: string;
  body: string;
  action: string;
  onClick: () => void;
}) {
  return (
    <Card>
      <CardBody>
        <Row style={{ marginBottom: 8 }}>
          <StepNumber $pending={!current}>{number}</StepNumber>
          <Strong>{title}</Strong>
        </Row>
        <p style={{ marginTop: 0 }}>
          <Muted>
            <Small>{body}</Small>
          </Muted>
        </p>
        <Button $variant={current ? "primary" : "secondary"} onClick={onClick}>
          {action}
        </Button>
      </CardBody>
    </Card>
  );
}

const Empty = styled.div`
  max-width: 720px;
  margin: 72px auto;
  text-align: center;

  h2 {
    font-size: 24px;
    margin-bottom: 8px;
  }
`;

const EmptyMark = styled.div`
  margin: 0 auto 20px;
  line-height: 0;
`;

const Cards = styled.div`
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  margin-top: 24px;
  text-align: left;
`;

const NoAccount = styled(Card)`
  margin-top: 16px;
  text-align: left;
`;
