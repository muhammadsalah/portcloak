/** Step five: the point of no return, and what it will not undo. */
import {
  BulletList,
  Button,
  Card,
  CardBody,
  CardHead,
  CardTitle,
  GroupTitle,
  Notice,
} from "../../design-system";

export function ApplyStep({
  realm,
  environment,
  strategy,
  outOfScope,
  applying,
  onApply,
}: {
  realm?: string;
  environment: string;
  strategy: string;
  outOfScope: string[];
  applying: boolean;
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

        <GroupTitle style={{ marginTop: 14 }}>What was never carried</GroupTitle>
        <BulletList>
          {outOfScope.map((note) => (
            <li key={note}>{note}</li>
          ))}
        </BulletList>

        <div style={{ marginTop: 16 }}>
          <Button $variant="primary" disabled={applying} onClick={() => void onApply()}>
            {applying ? "Applying…" : "Apply import"}
          </Button>
        </div>
      </CardBody>
    </Card>
  );
}
