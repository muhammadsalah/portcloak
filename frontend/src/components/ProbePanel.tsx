// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/**
 * The result of probing an environment.
 *
 * Shared by the Environments editor and the capture wizard's first step,
 * because they ask the engine the same question and an operator should not have
 * to learn two answers to it.
 *
 * It reports the facts a capture depends on, not a green tick.
 */
import type { TargetFacts } from "../api";
import {
  Badge,
  KeyValue,
  Muted,
  NoticeBox,
  NoticeTitle,
  Small,
  type Tone,
} from "../design-system";

export function ProbePanel({ facts, ok }: { facts: TargetFacts; ok: boolean }) {
  return (
    <NoticeBox $tone={ok ? "ok" : "danger"}>
      <NoticeTitle>
        {ok
          ? "Probe passed — capture will not touch the serving instance"
          : "The probe found a blocking problem"}
      </NoticeTitle>

      <KeyValue style={{ marginTop: 10 }}>
        {facts.checks.map((check) => {
          const tone = toneFor(check.status);
          return (
            <div key={check.name} style={{ display: "contents" }}>
              <dt>{check.name}</dt>
              <dd>
                {tone ? <Badge $tone={tone}>{check.value}</Badge> : <span>{check.value}</span>}
                {check.advice ? (
                  <div>
                    <Muted>
                      <Small>{check.advice}</Small>
                    </Muted>
                  </div>
                ) : null}
              </dd>
            </div>
          );
        })}
      </KeyValue>

      <div style={{ marginTop: 10 }}>
        <Muted>
          <Small>{facts.readOnlyNote}</Small>
        </Muted>
      </div>
    </NoticeBox>
  );
}

/** A passing check is stated plainly; only the other three get a badge. */
function toneFor(status: string): Tone | null {
  switch (status) {
    case "fail":
      return "danger";
    case "warn":
      return "warn";
    case "skipped":
      return "neutral";
    default:
      return null;
  }
}
