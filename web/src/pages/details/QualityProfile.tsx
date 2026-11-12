import { useId, useState } from "react";
import { Button } from "@radix-ui/themes";
import {
  api,
  messageFor,
  type CatalogProfile,
  type CatalogProfileContext,
} from "../../api";
import { Alert, FieldSet, FormActions, FormGroup } from "../../ui";

/**
 * Per-title quality profile control for the item's own details page. The
 * assignment (profile_id + revision) and the saved profile list come from the
 * same details snapshot, and the reviewed revision is sent back with the write
 * so a stale editor cannot overwrite a newer assignment made elsewhere.
 */
export function QualityProfile({
  id,
  assignment,
  context,
  reload,
  onSaved,
}: {
  id: number;
  assignment?: CatalogProfile;
  context?: CatalogProfileContext;
  reload?: () => void;
  onSaved?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [success, setSuccess] = useState("");
  const inherited =
    assignment?.profile_id === null || assignment?.profile_id === undefined;
  const current = assignment?.effective_profile.name;
  return (
    <section aria-label="Quality profile">
      <FieldSet
        legend="Quality profile"
        description="The quality rules used to accept, reject or upgrade this title's releases. Other titles keep their own profiles."
        actions={
          !open && (
            <Button
              size="1"
              variant="soft"
              disabled={!assignment || !context}
              onClick={() => {
                setOpen(true);
                setSuccess("");
              }}
            >
              Change quality profile
            </Button>
          )
        }
      >
        {!open ? (
          <>
            <p className="rs-muted">
              {current ? (
                <>
                  Currently using <strong>{current}</strong>{" "}
                  {inherited
                    ? "(follows the schedule default)."
                    : "(assigned to this title)."}
                </>
              ) : (
                "Quality profile information is unavailable for this title."
              )}
            </p>
            {success && (
              <Alert kind="success" role="status">
                {success}
              </Alert>
            )}
          </>
        ) : assignment && context ? (
          <QualityProfileForm
            key={`${assignment.profile_id ?? "default"}-${assignment.revision ?? 0}`}
            id={id}
            assignment={assignment}
            context={context}
            reload={reload}
            close={() => setOpen(false)}
            saved={(name, useDefault) => {
              setOpen(false);
              setSuccess(
                useDefault
                  ? `Now follows the schedule default (${name}).`
                  : `Assigned to ${name}.`,
              );
              onSaved?.();
            }}
          />
        ) : (
          <Alert kind="danger">
            Quality profile information is unavailable for this title.
          </Alert>
        )}
      </FieldSet>
    </section>
  );
}

function QualityProfileForm({
  id,
  assignment,
  context,
  reload,
  close,
  saved,
}: {
  id: number;
  assignment: CatalogProfile;
  context: CatalogProfileContext;
  reload?: () => void;
  close: () => void;
  saved: (name: string, useDefault: boolean) => void;
}) {
  const currentID = assignment.profile_id ?? null;
  const revision = assignment.revision ?? 0;
  const [choice, setChoice] = useState(
    currentID === null ? "default" : String(currentID),
  );
  const [reviewed, setReviewed] = useState(false);
  const [saving, setSaving] = useState(false);
  const [stale, setStale] = useState(false);
  const [error, setError] = useState("");
  const selectId = useId();
  const profiles = context.profiles;
  const target = choice === "default" ? null : Number(choice);
  const dirty = target !== currentID;
  const targetName =
    target === null
      ? context.default_profile.name
      : (profiles.find((profile) => profile.id === target)?.name ??
        `Profile ${target}`);
  const currentName = assignment.effective_profile.name;
  async function save() {
    if (!reviewed || saving || stale || !dirty) return;
    setSaving(true);
    setError("");
    try {
      await api(`/automation/items/${id}`, {
        method: "PUT",
        body: JSON.stringify({ profile_id: target, revision }),
      });
      saved(targetName, target === null);
    } catch (e) {
      setError(
        `${messageFor(e)} Reload the saved assignment before another attempt.`,
      );
      setStale(true);
      setReviewed(false);
    } finally {
      setSaving(false);
    }
  }
  return (
    <div className="rs-stack">
      <FormGroup
        label="Quality profile"
        htmlFor={selectId}
        help="Choosing the schedule default lets this title follow future default changes. An explicit profile stays with this title when the default changes."
      >
        <select
          id={selectId}
          aria-label="Target quality profile"
          value={choice}
          disabled={saving || stale}
          onChange={(event) => {
            setChoice(event.target.value);
            setReviewed(false);
          }}
        >
          <option value="default">
            Follow schedule default ({context.default_profile.name})
          </option>
          {profiles.map((profile) => (
            <option key={profile.id} value={String(profile.id)}>
              {profile.name}
            </option>
          ))}
        </select>
      </FormGroup>
      {error && <Alert kind="danger">{error}</Alert>}
      {reviewed && dirty && (
        <section aria-label="Review quality profile change">
          <h3>Review change</h3>
          <p>
            {currentID === null ? "Schedule default: " : "Assigned: "}
            {currentName} →{" "}
            {target === null ? `Schedule default: ${targetName}` : targetName}
          </p>
          <p className="rs-muted rs-small">
            Monitoring, existing files and downloads stay unchanged. Nothing is
            searched, downloaded or replaced, and 720p releases remain excluded
            by the profile's rules.
          </p>
        </section>
      )}
      <FormActions>
        {stale ? (
          <Button variant="soft" disabled={!reload} onClick={() => reload?.()}>
            Reload saved assignment
          </Button>
        ) : reviewed ? (
          <Button disabled={saving || !dirty} onClick={() => void save()}>
            {saving ? "Saving…" : "Save reviewed change"}
          </Button>
        ) : (
          <Button disabled={!dirty || saving} onClick={() => setReviewed(true)}>
            Review change
          </Button>
        )}
        <Button variant="soft" color="gray" disabled={saving} onClick={close}>
          {dirty ? "Cancel" : "Done"}
        </Button>
      </FormActions>
    </div>
  );
}
