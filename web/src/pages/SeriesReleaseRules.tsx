import { useId, useState, type FormEvent } from "react";
import { Button, TextField } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { Alert, FieldSet, FormActions, FormGroup } from "../ui";

type Rules = { codec: string; group: string; revision: number };
export function SeriesReleaseRules({ id }: { id: number }) {
  const [rules, setRules] = useState<Rules>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const codecID = useId();
  const groupID = useId();
  async function load() {
    setBusy(true);
    setError("");
    try {
      setRules(await api<Rules>(`/library/series/${id}/release-rules`));
      setSaved(false);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  async function save(e: FormEvent) {
    e.preventDefault();
    if (!rules) return;
    setBusy(true);
    setError("");
    try {
      setRules(
        await api<Rules>(`/library/series/${id}/release-rules`, {
          method: "PUT",
          body: JSON.stringify(rules),
        }),
      );
      setSaved(true);
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <FieldSet
      legend="Release rules"
      description="Scheduled searches for this show also require this codec or group, on top of the quality profile. Manual searches are unrestricted."
      actions={
        !rules && (
          <Button
            size="1"
            variant="soft"
            disabled={busy}
            onClick={() => void load()}
          >
            Edit release rules
          </Button>
        )
      }
    >
      {rules && (
        <form className="rs-stack" onSubmit={save}>
          <FormGroup label="Required codec" htmlFor={codecID}>
            <select
              id={codecID}
              value={rules.codec}
              onChange={(e) => {
                setSaved(false);
                setRules({ ...rules, codec: e.target.value });
              }}
            >
              <option value="">Any codec</option>
              <option value="h264">H.264 / x264 / AVC</option>
              <option value="h265">H.265 / x265 / HEVC</option>
            </select>
          </FormGroup>
          <FormGroup
            label="Required release group"
            htmlFor={groupID}
            help="Must appear as a bracketed prefix or hyphenated suffix. Existing downloads are unchanged."
          >
            <TextField.Root
              id={groupID}
              value={rules.group}
              onChange={(e) => {
                setSaved(false);
                setRules({ ...rules, group: e.target.value });
              }}
              placeholder="e.g. MeGusta"
              maxLength={80}
            />
          </FormGroup>
          <FormActions>
            <Button disabled={busy}>Save rules</Button>
            <Button
              type="button"
              variant="soft"
              color="gray"
              disabled={busy}
              onClick={() => void load()}
            >
              Reload
            </Button>
          </FormActions>
          {saved && (
            <Alert kind="success" role="status">
              Release rules saved.
            </Alert>
          )}
        </form>
      )}
      {error && <Alert kind="danger">{error}</Alert>}
    </FieldSet>
  );
}
