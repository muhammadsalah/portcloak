// Copyright 2026 Muhammad Salah
// SPDX-License-Identifier: Apache-2.0

/** Step five: the point of no return, and what it will not undo. */
import {
  BulletList,
  Button,
  Card,
  CardBody,
  CardHead,
  CardTitle,
  Checkbox,
  GroupTitle,
  Notice,
} from "@/design-system";
import { Icon } from "@/components/Icon";

export function ApplyStep({
  realm,
  environment,
  strategy,
  outOfScope,
  applying,
  noTransactionTimeout,
  onNoTransactionTimeout,
  onApply,
}: {
  realm?: string;
  environment: string;
  strategy: string;
  outOfScope: string[];
  applying: boolean;
  noTransactionTimeout: boolean;
  onNoTransactionTimeout: (value: boolean) => void;
  onApply: () => void | Promise<void>;
}) {
  return (
    <Card>
      <CardHead>
        <CardTitle>Apply the import</CardTitle>
      </CardHead>
      <CardBody>
        <p>
          {`PortCloak will import ${realm} into ${environment} using the ${strategy} strategy.`}
        </p>

        <Notice
          tone="warn"
          title="Keycloak's import is not transactional"
          body="If it fails part-way, the destination is left in whatever state Keycloak reached. PortCloak reports what was applied rather than claiming a rollback it cannot perform."
        />

        <Checkbox
          checked={noTransactionTimeout}
          label="Let the import's transactions run without a time limit"
          hint="For a realm too large to write inside the destination's limit. Transactions cannot be turned off. This lifts the limit that cancels one. It matters more here than on a capture: an export cancelled part-way leaves nothing behind, an import leaves a half-applied realm. The limit is also what bounds an import that has stopped making progress, which would otherwise hold a connection to the destination database open until the clone is destroyed."
          onChange={onNoTransactionTimeout}
        />

        <GroupTitle style={{ marginTop: 14 }}>What was never carried</GroupTitle>
        <BulletList>
          {outOfScope.map((note) => (
            <li key={note}>{note}</li>
          ))}
        </BulletList>

        <div style={{ marginTop: 16 }}>
          <Button $variant="primary" disabled={applying} onClick={() => void onApply()}>
            <Icon name="restore" />
            {applying ? "Applying…" : "Apply import"}
          </Button>
        </div>
      </CardBody>
    </Card>
  );
}
