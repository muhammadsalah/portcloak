// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * What is in this snapshot, where it came from, and what is missing from it.
 *
 * The completeness panel is the one that matters: "out of scope" is a design
 * decision, not a failure, and the screen has to say which is which.
 */
import type { Overview } from "../../api";
import {
  Card,
  CardBody,
  CardHead,
  CardTitle,
  GroupTitle,
  KeyValue,
  Muted,
  Notice,
  NoticeBox,
  NoticeTitle,
  Row,
  Small,
  SplitWide,
  Stat,
  StatGrid,
} from "../../design-system";
import { count } from "../../utils/format";
import styled from "styled-components";

export function OverviewTab({ overview }: { overview: Overview }) {
  return (
    <div>
      {overview.tokenContinuity ? (
        <Notice
          tone="info"
          title="Token continuity preserved"
          body={overview.tokenContinuityNote}
        />
      ) : (
        <Notice
          tone="warn"
          title="Token continuity not established"
          body={overview.tokenContinuityNote}
        />
      )}

      <SplitWide>
        <div>
          <Contents overview={overview} />
          <Provenance overview={overview} />
        </div>
        <div>
          <Completeness overview={overview} />
          {overview.dependencies?.length ? <Dependencies overview={overview} /> : null}
        </div>
      </SplitWide>
    </div>
  );
}

function Contents({ overview }: { overview: Overview }) {
  const algorithms = overview.credentials?.algorithms;

  return (
    <Card>
      <CardHead>
        <CardTitle>Contents</CardTitle>
      </CardHead>
      <CardBody>
        <StatGrid>
          <Stat value={count(overview.counts?.users)} label="Users" />
          <Stat value={count(overview.credentials?.passwordHashes)} label="Password hashes" />
          <Stat value={count(overview.credentials?.otp)} label="OTP enrolments" />
          <Stat value={count(overview.credentials?.webauthn)} label="Passkeys" />
        </StatGrid>

        <StatGrid style={{ marginTop: 18 }}>
          <Stat value={count(overview.counts?.clients)} label="Clients" />
          <Stat value={count(overview.counts?.keyProviders)} label="Key providers" />
          <Stat value={count(overview.counts?.identityProviders)} label="Identity providers" />
          <Stat value={count(overview.counts?.federations)} label="User federation" />
        </StatGrid>

        {algorithms ? (
          <div style={{ marginTop: 16 }}>
            <Muted>
              <Small>
                {`Password hashing: ${Object.entries(algorithms)
                  .map(([algorithm, n]) => `${algorithm} (${count(n)})`)
                  .join(", ")}. The destination's password policy has to match, or these stop verifying.`}
              </Small>
            </Muted>
          </div>
        ) : null}
      </CardBody>
    </Card>
  );
}

function Provenance({ overview }: { overview: Overview }) {
  const p = overview.provenance as Record<string, unknown>;

  const rows: [string, unknown][] = [
    ["Source", `${p.environmentKind ?? ""} · ${p.target ?? ""}`],
    ["Keycloak version", p.keycloakVersion],
    ["Capture mode", p.captureMode],
    [
      "Execution",
      p.executionMode === "ephemeral-clone"
        ? "ephemeral clone — the serving instance was untouched"
        : "in place, on isolated ports",
    ],
    ...(p.cloneRef ? ([["Clone reference", `${p.cloneRef} (destroyed)`]] as [string, unknown][]) : []),
    ["Ports", p.ports],
    ["Users mode", p.usersMode],
    ["Secret verification", p.secretVerification],
    ["Dependency scan", p.dependencyScan],
    ["Integrity", overview.integrityMessage],
  ];

  return (
    <Card>
      <CardHead>
        <CardTitle>Provenance</CardTitle>
      </CardHead>
      <CardBody>
        <KeyValue>
          {rows
            .filter(([, value]) => value !== undefined && value !== null && value !== "")
            .map(([label, value]) => (
              <div key={label} style={{ display: "contents" }}>
                <dt>{label}</dt>
                <dd>{String(value)}</dd>
              </div>
            ))}
        </KeyValue>
      </CardBody>
    </Card>
  );
}

function Completeness({ overview }: { overview: Overview }) {
  const categories = overview.completeness?.categories ?? [];
  const captured = categories.filter((c) => c.status === "captured");
  const partial = categories.filter((c) => c.status === "partial");
  const missing = categories.filter((c) => c.status === "missing");
  const outOfScope = categories.filter((c) => c.status === "outOfScope");
  const notChecked = categories.filter((c) => c.status === "notChecked");

  return (
    <Card>
      <CardHead>
        <CardTitle>Completeness</CardTitle>
      </CardHead>
      <CardBody>
        <Good>{`✓ ${captured.length} categories captured`}</Good>
        <Verdict $bad={missing.length > 0}>
          {`${missing.length ? "✕" : "✓"} ${missing.length} missing · ${partial.length} partial`}
        </Verdict>

        {[...partial, ...missing].map((category) => (
          <div key={category.name} style={{ marginLeft: 16 }}>
            <Muted>
              <Small>{`${category.name}: ${category.reason}`}</Small>
            </Muted>
          </div>
        ))}

        {notChecked.map((category) => (
          <NotChecked key={category.name}>{`${category.name}: ${category.reason}`}</NotChecked>
        ))}

        <GroupTitle style={{ marginTop: 14 }}>Out of scope by design</GroupTitle>
        {outOfScope.map((category) => (
          <div key={category.name}>
            <Muted>
              <Small>{`· ${category.name}`}</Small>
            </Muted>
          </div>
        ))}

        <NoticeBox $tone="info" style={{ marginTop: 10, fontSize: 12 }}>
          “Out of scope” is a design decision, not a failure — users re-authenticate after restore.
        </NoticeBox>
      </CardBody>
    </Card>
  );
}

function Dependencies({ overview }: { overview: Overview }) {
  return (
    <Card>
      <CardHead>
        <CardTitle>⚠ External dependencies</CardTitle>
      </CardHead>
      <CardBody>
        <p style={{ marginTop: 0 }}>
          <Muted>
            <Small>
              Provision these on the destination before importing, or the realm imports cleanly and
              then fails at login.
            </Small>
          </Muted>
        </p>
        {overview.dependencies.map((dependency, i) => (
          <NoticeBox key={i} $tone="warn" style={{ marginBottom: 8 }}>
            <NoticeTitle>{dependency.name}</NoticeTitle>
            <Small>
              {`${dependency.type}${dependency.detectedAt ? ` · ${dependency.detectedAt}` : ""}`}
            </Small>
          </NoticeBox>
        ))}
      </CardBody>
    </Card>
  );
}

const Good = styled(Row)`
  color: ${(p) => p.theme.color.success};
`;

const Verdict = styled(Row)<{ $bad: boolean }>`
  color: ${(p) => (p.$bad ? p.theme.color.danger : p.theme.color.success)};
`;

const NotChecked = styled.div`
  font-size: 12px;
  color: ${(p) => p.theme.color.warning};
  margin-top: 6px;
`;
