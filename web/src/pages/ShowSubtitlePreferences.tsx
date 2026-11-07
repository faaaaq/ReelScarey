import { useId, useState } from "react";
import { Button } from "@radix-ui/themes";
import { X } from "lucide-react";
import { api, messageFor } from "../api";
import { RequestState } from "../components";
import { Alert, Check, FieldSet, FormActions, FormGroup, Label } from "../ui";
import { useResource } from "../useResource";
import type { SubtitlePreferences, SubtitleTrack } from "./Subtitles";
import "./details/Details.css";

type Response = {
  inherited: boolean;
  preferences: SubtitlePreferences;
  revision: number;
  global_revision: number;
  languages: { code: string; name: string }[];
};
export function ShowSubtitlePreferences({ id }: { id: number }) {
  const [open, setOpen] = useState(false);
  return (
    <FieldSet
      legend="Subtitles"
      description="Optional per-show override of the global subtitle preferences."
      actions={
        <Button size="1" variant="soft" onClick={() => setOpen(!open)}>
          {open ? "Hide subtitle preferences" : "Edit subtitle preferences"}
        </Button>
      }
    >
      {open && <Loader id={id} />}
    </FieldSet>
  );
}
function Loader({ id }: { id: number }) {
  const path = `/library/series/${id}/subtitles`,
    resource = useResource<Response>(path);
  return (
    <>
      <RequestState
        loading={resource.loading}
        error={resource.error}
        retry={resource.reload}
      />
      {resource.data && (
        <Form
          key={`${resource.data.revision}-${resource.data.global_revision}`}
          data={resource.data}
          path={path}
          reload={resource.reload}
        />
      )}
    </>
  );
}
function Form({
  data,
  path,
  reload,
}: {
  data: Response;
  path: string;
  reload: () => void;
}) {
  const [form, setForm] = useState(data.preferences),
    [language, setLanguage] = useState("en"),
    [variant, setVariant] = useState<SubtitleTrack["variant"]>("full"),
    [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const policyID = useId(),
    hearingID = useId();
  async function save(inherit = false) {
    setBusy(true);
    setError("");
    try {
      await api(path, {
        method: inherit ? "DELETE" : "PUT",
        body: JSON.stringify({
          enabled: form.enabled,
          tracks: form.tracks,
          hearing_impaired: form.hearing_impaired,
          match_policy: form.match_policy,
          revision: data.revision,
          global_revision: data.global_revision,
        }),
      });
      reload();
    } catch (e) {
      setError(messageFor(e));
    } finally {
      setBusy(false);
    }
  }
  const name = (code: string) =>
    data.languages.find((l) => l.code === code)?.name ?? code;
  return (
    <fieldset disabled={busy} className="rs-stack">
      <FormGroup
        label="Source"
        help="Applies to this show's registered episodes; global automatic subtitles must also be on. Files and monitoring are unchanged."
      >
        <span>
          <Label kind={data.inherited ? "disabled" : "primary"}>
            {data.inherited
              ? "Using global subtitle preferences"
              : "Custom subtitle preferences"}
          </Label>
        </span>
      </FormGroup>
      <FormGroup label="Automatic">
        <Check
          label="Allow automatic subtitles for this show"
          checked={form.enabled}
          onChange={(enabled) => setForm({ ...form, enabled })}
        />
      </FormGroup>
      <FormGroup label="Tracks" help="Up to nine language and type pairs.">
        {form.tracks.length ? (
          <ul className="det-track-list">
            {form.tracks.map((t) => (
              <li key={t.language + t.variant}>
                <Label>
                  {name(t.language)} · {t.variant}
                </Label>
                <button
                  type="button"
                  className="det-monitor"
                  aria-label={`Remove ${t.language} ${t.variant}`}
                  onClick={() =>
                    setForm({
                      ...form,
                      tracks: form.tracks.filter((v) => v !== t),
                    })
                  }
                >
                  <X size={14} aria-hidden="true" />
                </button>
              </li>
            ))}
          </ul>
        ) : (
          <span className="rs-form-help is-warning">
            Add at least one track before saving.
          </span>
        )}
        <div className="rs-row">
          <select
            aria-label="Subtitle language"
            value={language}
            onChange={(e) => setLanguage(e.target.value)}
          >
            {data.languages.map((l) => (
              <option key={l.code} value={l.code}>
                {l.name}
              </option>
            ))}
          </select>
          <select
            aria-label="Subtitle track type"
            value={variant}
            onChange={(e) =>
              setVariant(e.target.value as SubtitleTrack["variant"])
            }
          >
            <option value="full">Full subtitles</option>
            <option value="signs">Signs / songs</option>
            <option value="forced">Forced</option>
          </select>
          <Button
            type="button"
            variant="soft"
            disabled={
              form.tracks.some(
                (t) => t.language === language && t.variant === variant,
              ) || form.tracks.length >= 9
            }
            onClick={() =>
              setForm({
                ...form,
                tracks: [...form.tracks, { language, variant }],
              })
            }
          >
            Add track
          </Button>
        </div>
      </FormGroup>
      <FormGroup label="Matching policy" htmlFor={policyID}>
        <select
          id={policyID}
          value={form.match_policy}
          onChange={(e) =>
            setForm({
              ...form,
              match_policy: e.target
                .value as SubtitlePreferences["match_policy"],
            })
          }
        >
          <option value="hash">File hash</option>
          <option value="hash_or_release">File hash or exact release</option>
        </select>
      </FormGroup>
      <FormGroup label="Hearing-impaired tracks" htmlFor={hearingID}>
        <select
          id={hearingID}
          value={form.hearing_impaired}
          onChange={(e) =>
            setForm({
              ...form,
              hearing_impaired: e.target
                .value as SubtitlePreferences["hearing_impaired"],
            })
          }
        >
          <option value="any">Any</option>
          <option value="exclude">Exclude</option>
          <option value="prefer">Prefer</option>
        </select>
      </FormGroup>
      {error && <Alert kind="danger">{error}</Alert>}
      <FormActions>
        <Button
          type="button"
          disabled={!form.tracks.length}
          onClick={() => void save()}
        >
          Save show subtitle preferences
        </Button>
        <Button
          type="button"
          variant="soft"
          color="gray"
          disabled={data.inherited}
          onClick={() => void save(true)}
        >
          Use global preferences
        </Button>
      </FormActions>
    </fieldset>
  );
}
