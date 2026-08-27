/**
 * One user, in a modal.
 *
 * Presence and metadata only. No credential value is shown, and there is no
 * action here that would reveal one.
 */
import { InspectAPI, type UserDetail as UserDetailView } from "../../api";
import {
  Badge,
  GroupTitle,
  KeyValue,
  Muted,
  Right,
  Row,
  Small,
  type useModal,
} from "../../design-system";
import { count, when } from "../../utils/format";

type Modal = ReturnType<typeof useModal>;

export async function showUser(snapshotId: string, userId: string, modal: Modal): Promise<void> {
  const [detail, failure] = await InspectAPI.user(snapshotId, userId);
  if (failure) {
    modal.open({ title: "That user could not be read", body: <div>{failure.message}</div> });
    return;
  }
  modal.open({
    title: detail.username,
    body: <UserBody detail={detail} />,
    cancelLabel: "Close",
  });
}

function UserBody({ detail }: { detail: UserDetailView }) {
  const attributes = Object.entries(detail.attributes ?? {});

  return (
    <div>
      <KeyValue>
        <dt>Email</dt>
        <dd>{detail.email || "—"}</dd>
        <dt>Name</dt>
        <dd>{`${detail.firstName ?? ""} ${detail.lastName ?? ""}`.trim() || "—"}</dd>
        <dt>Status</dt>
        <dd>{detail.enabled ? "Enabled" : "Disabled"}</dd>
        <dt>Origin</dt>
        <dd>{detail.origin}</dd>
        <dt>Realm roles</dt>
        <dd>{(detail.realmRoles ?? []).join(", ") || "—"}</dd>
        <dt>Groups</dt>
        <dd>{(detail.groups ?? []).join(", ") || "—"}</dd>
        <dt>Required actions</dt>
        <dd>{(detail.requiredActions ?? []).join(", ") || "—"}</dd>
      </KeyValue>

      <GroupTitle style={{ marginTop: 16 }}>Credentials</GroupTitle>
      {(detail.credentials ?? []).map((credential, i) => (
        <Row key={i} style={{ padding: "4px 0", fontSize: 12 }}>
          <Badge $tone="neutral">{credential.type}</Badge>
          {credential.algorithm ? (
            <Muted>
              {`${credential.algorithm}${credential.iterations ? ` · ${count(credential.iterations)} iterations` : ""}`}
            </Muted>
          ) : null}
          {credential.created ? (
            <Right>
              <Muted>{when(credential.created)}</Muted>
            </Right>
          ) : null}
        </Row>
      ))}

      <div style={{ marginTop: 6 }}>
        <Muted>
          <Small>
            Presence and metadata only. No credential value is shown, and there is no action here
            that would reveal one.
          </Small>
        </Muted>
      </div>

      {attributes.length > 0 ? (
        <div>
          <GroupTitle style={{ marginTop: 16 }}>Attributes</GroupTitle>
          <KeyValue>
            {attributes.map(([key, values]) => (
              <div key={key} style={{ display: "contents" }}>
                <dt>{key}</dt>
                <dd>{values.join(", ")}</dd>
              </div>
            ))}
          </KeyValue>
        </div>
      ) : null}
    </div>
  );
}
