/**
 * Ephemeral clones a crashed session left behind.
 *
 * Found by PortCloak's own label on launch. Offered, never removed without
 * asking — your cluster is not ours to garbage-collect.
 */
import { SettingsAPI, type OrphanReport } from "../../api";
import {
  Button,
  Card,
  CardBody,
  CardHead,
  CardTitle,
  FailureNotice,
  Muted,
  Notice,
  PathBox,
  Row,
  Small,
  useModal,
} from "../../design-system";

export function OrphanPanel({ report, reload }: { report: OrphanReport; reload: () => void }) {
  const modal = useModal();

  const heading =
    report.orphans.length > 0
      ? `⚠ ${report.orphans.length} orphaned clone${report.orphans.length === 1 ? "" : "s"} found`
      : "Orphaned clones";

  return (
    <Card $tone={report.orphans.length ? "warning" : undefined}>
      <CardHead>
        <CardTitle>{heading}</CardTitle>
        <Button $variant="plain" onClick={reload}>
          Check again
        </Button>
      </CardHead>
      <CardBody>
        {report.orphans.map((orphan) => (
          <div key={`${orphan.environment}:${orphan.ref}`} style={{ marginBottom: 12 }}>
            <PathBox>{orphan.ref}</PathBox>
            <Muted>
              <Small>
                {`${orphan.environment} · created ${orphan.age} ago · ${orphan.state ?? ""}`}
              </Small>
            </Muted>
            <Row style={{ marginTop: 8 }}>
              <Button
                $variant="primary"
                onClick={async () => {
                  const failure = await SettingsAPI.removeOrphan(orphan.environment, orphan.ref);
                  if (failure) {
                    modal.open({
                      title: "Not removed",
                      body: <FailureNotice failure={failure} />,
                      cancelLabel: "Close",
                    });
                    return;
                  }
                  reload();
                }}
              >
                Remove it
              </Button>
              <Button onClick={reload}>Leave it</Button>
            </Row>
          </div>
        ))}

        {report.unchecked.map((entry) => (
          <Notice
            key={entry.environment}
            tone="warn"
            title={`${entry.environment} could not be checked`}
            body={entry.reason}
          />
        ))}

        <p style={{ marginBottom: 0 }}>
          <Muted>
            <Small>{report.note}</Small>
          </Muted>
        </p>
        <p>
          <Muted>
            <Small>
              Found by PortCloak&apos;s own label on launch. Offered, never removed without asking
              — your cluster is not ours to garbage-collect.
            </Small>
          </Muted>
        </p>
      </CardBody>
    </Card>
  );
}
