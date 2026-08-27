// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The one screen that answers "which PortCloak wrote this bundle?".
 *
 * A snapshot manifest records the version that produced it, so when a restore
 * refuses the first thing anyone needs is the identity of the binary in front
 * of them. Copying it has its own button because the alternative is a reporter
 * transcribing a commit hash by eye, and half of those arrive wrong.
 */
import { useState } from "react";

import type { AboutView } from "../../api";
import {
  Button,
  Card,
  CardBody,
  CardHead,
  CardTitle,
  KeyValue,
  Mono,
  Muted,
  Row,
  Small,
} from "../../design-system";

export function AboutPanel({ about }: { about: AboutView }) {
  const rows: [string, string, boolean][] = [
    ["Version", about.version, false],
    ["Commit", about.commit, true],
    ["Built", about.date, false],
    ["Platform", about.platform, true],
    ["Go", about.go, true],
    ["Licence", about.licence, false],
    ["Copyright", about.copyright, false],
    ["Log file", about.logFile, true],
  ];

  return (
    <Card>
      <CardHead>
        <CardTitle>About PortCloak</CardTitle>
      </CardHead>
      <CardBody>
        <KeyValue>
          {rows.map(([label, value, mono]) => (
            <Fragment key={label} label={label} value={value} mono={mono} />
          ))}
        </KeyValue>

        <p>
          <Muted>
            <Small>
              A commit marked dirty was built from a tree with uncommitted changes, so it is not
              exactly the commit it names.
            </Small>
          </Muted>
        </p>

        <Row style={{ marginTop: 12 }}>
          <CopyButton text={about.support} />
        </Row>
      </CardBody>
    </Card>
  );
}

function Fragment({ label, value, mono }: { label: string; value: string; mono: boolean }) {
  return (
    <>
      <dt>{label}</dt>
      <dd>{mono ? <Mono>{value}</Mono> : value}</dd>
    </>
  );
}

/** Says "Copied" for a moment, because a clipboard write is otherwise silent. */
function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  return (
    <Button
      $variant="plain"
      onClick={() => {
        void navigator.clipboard.writeText(text).then(() => {
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1500);
        });
      }}
    >
      {copied ? "Copied" : "Copy build details"}
    </Button>
  );
}
