import { useId, useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { RequestState } from "../components";
import {
  Alert,
  Check,
  FieldSet,
  FormActions,
  FormGroup,
  Label,
  Table,
} from "../ui";
import { useResource } from "../useResource";
import type { SubtitlePreferences, SubtitleTrack } from "./Subtitles";
import "./Subtitles.css";

type Response = {
  inherited: boolean;
  preferences: SubtitlePreferences;
  revision: number;
  global_revision: number;
  languages: { code: string; name: string }[];
};
const variantName: Record<SubtitleTrack["variant"], string> = {
  full: "Full subtitles",
  signs: "Signs / songs",
  forced: "Forced",
};
export function ShowSubtitlePreferences({ id }: { id: number }) {
  const [open, setOpen] = useState(false);
  return (
    <FieldSet
      legend="Subtitles"
      description="Optional per-show override of the global subtitle preferences."
      actions={
        <Button
          size="1"
          variant="soft"
          aria-expanded={open}
          onClick={() => setOpen(!open)}
        >
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
    hearingID = useId(),
    languageID = useId(),
    trackID = useId();
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
        {form.tracks.length > 0 ? (
          <Table label="Show subtitle tracks">
            <tbody>
              {form.tracks.map((track) => (
                <tr key={track.language + track.variant}>
                  <th scope="row">{name(track.language)}</th>
                  <td>{variantName[track.variant]}</td>
                  <td className="is-actions">
                    <Button
                      type="button"
                      size="1"
                      variant="ghost"
                      color="gray"
                      aria-label={`Remove ${name(track.language)} ${variantName[track.variant]}`}
                      onClick={() =>
                        setForm({
                          ...form,
                          tracks: form.tracks.filter(
                            (value) => value !== track,
                          ),
                        })
                      }
                    >
                      Remove
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        ) : (
          <span className="rs-form-help is-warning">
            Add at least one track before saving.
          </span>
        )}
        <div className="rs-row">
          <select
            id={languageID}
            aria-label="Subtitle language"
            value={language}
            onChange={(event) => setLanguage(event.target.value)}
          >
            {data.languages.map((option) => (
              <option key={option.code} value={option.code}>
                {option.name}
              </option>
            ))}
          </select>
          <select
            id={trackID}
            aria-label="Subtitle track type"
            value={variant}
            onChange={(event) =>
              setVariant(event.target.value as SubtitleTrack["variant"])
            }
          >
            {(["full", "signs", "forced"] as const).map((value) => (
              <option key={value} value={value}>
                {variantName[value]}
              </option>
            ))}
          </select>
          <Button
            type="button"
            variant="soft"
            disabled={
              form.tracks.some(
                (track) =>
                  track.language === language && track.variant === variant,
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
          onChange={(event) =>
            setForm({
              ...form,
              match_policy: event.target
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
          onChange={(event) =>
            setForm({
              ...form,
              hearing_impaired: event.target
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
