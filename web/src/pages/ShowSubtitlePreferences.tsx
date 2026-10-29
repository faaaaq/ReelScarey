import { useState } from "react";
import { Button } from "@radix-ui/themes";
import { api, messageFor } from "../api";
import { RequestState } from "../components";
import { useResource } from "../useResource";
import type { SubtitlePreferences, SubtitleTrack } from "./Subtitles";
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
    <section className="panel system-panel">
      <Button variant="soft" onClick={() => setOpen(!open)}>
        {open
          ? "Hide subtitle preferences"
          : "Subtitle preferences for this show"}
      </Button>
      {open && <Loader id={id} />}
    </section>
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
  return (
    <div className="settings-form">
      <h3>
        {data.inherited
          ? "Using global subtitle preferences"
          : "Custom subtitle preferences"}
      </h3>
      <p>
        Overrides apply to this show’s registered episodes. Global automatic
        subtitles must also be enabled. Files and monitoring stay unchanged.
      </p>
      <fieldset disabled={busy} className="backup-fields">
        <label>
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
          />{" "}
          Allow automatic subtitles for this show
        </label>
        <ul>
          {form.tracks.map((t) => (
            <li key={t.language + t.variant}>
              {data.languages.find((l) => l.code === t.language)?.name ??
                t.language}{" "}
              · {t.variant}{" "}
              <Button
                variant="ghost"
                onClick={() =>
                  setForm({
                    ...form,
                    tracks: form.tracks.filter((v) => v !== t),
                  })
                }
              >
                Remove {t.language} {t.variant}
              </Button>
            </li>
          ))}
        </ul>
        <div className="button-row">
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
        <label>
          Matching policy
          <select
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
        </label>
        <label>
          Hearing-impaired tracks
          <select
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
        </label>
        <div className="button-row">
          <Button disabled={!form.tracks.length} onClick={() => void save()}>
            Save show subtitle preferences
          </Button>
          <Button
            variant="soft"
            disabled={data.inherited}
            onClick={() => void save(true)}
          >
            Use global preferences
          </Button>
        </div>
      </fieldset>
      {error && (
        <p role="alert" className="error-box">
          {error}
        </p>
      )}
    </div>
  );
}
