import { useEffect, useState, type FormEvent } from "react";
import { Badge, Button, TextField } from "@radix-ui/themes";
import { CheckCircle2, FolderOpen, Save, Server } from "lucide-react";
import {
  api,
  messageFor,
  type Settings as LibrarySettings,
  type SettingsUpdate,
  type System,
} from "../api";
import { PageHeading, RequestState } from "../components";
import { useResource } from "../useResource";
import { TMDbSettings } from "./TMDbSettings";
import { IntegrationSettings } from "./IntegrationSettings";

export function Settings() {
  const settings = useResource<LibrarySettings>("/settings");
  const system = useResource<System>("/system");
  const [form, setForm] = useState<SettingsUpdate>();
  const [saved, setSaved] = useState<SettingsUpdate>();
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState(false);

  useEffect(() => {
    if (settings.data) {
      const { library_name, movie_folder, series_folder, revision } =
        settings.data;
      const value = { library_name, movie_folder, series_folder, revision };
      setForm(value);
      setSaved(value);
      setError("");
      setSuccess(false);
    }
  }, [settings.data]);

  const dirty = JSON.stringify(form) !== JSON.stringify(saved);
  function update(
    field: keyof Omit<SettingsUpdate, "revision">,
    value: string,
  ) {
    setForm((current) => (current ? { ...current, [field]: value } : current));
    setSuccess(false);
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!form || saving) return;
    setSaving(true);
    setError("");
    setSuccess(false);
    try {
      const result = await api<LibrarySettings>("/settings", {
        method: "PUT",
        body: JSON.stringify(form),
      });
      const value = {
        library_name: result.library_name,
        movie_folder: result.movie_folder,
        series_folder: result.series_folder,
        revision: result.revision,
      };
      setForm(value);
      setSaved(value);
      setSuccess(true);
    } catch (error) {
      setError(messageFor(error));
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <PageHeading
        eyebrow="MAKE YOURSELF AT HOME"
        title="Settings"
        description="A few simple preferences. One place to keep them."
      />
      <RequestState
        loading={settings.loading || system.loading}
        error={settings.error || system.error}
        retry={() => {
          settings.reload();
          system.reload();
        }}
      />
      {!settings.loading &&
        !system.loading &&
        !settings.error &&
        !system.error &&
        form &&
        system.data && (
          <div className="settings-grid grid gap-6 xl:grid-cols-[1.3fr_1fr]">
            <form className="panel settings-form" onSubmit={submit}>
              <div className="section-title">
                <h2>
                  <FolderOpen size={19} /> Library preferences
                </h2>
                <Badge color="green">Available</Badge>
              </div>
              <p className="muted">
                These preferences are saved to your local database.
              </p>
              <fieldset disabled={saving}>
                <label htmlFor="library-name">Library name</label>
                <TextField.Root
                  id="library-name"
                  value={form.library_name}
                  onChange={(event) =>
                    update("library_name", event.target.value)
                  }
                  maxLength={80}
                  required
                  size="3"
                  autoComplete="off"
                />
                <p className="field-help">Shown on your dashboard.</p>
                <label htmlFor="movie-folder">Movie folder</label>
                <TextField.Root
                  id="movie-folder"
                  value={form.movie_folder}
                  onChange={(event) =>
                    update("movie_folder", event.target.value)
                  }
                  maxLength={80}
                  required
                  size="3"
                  aria-describedby="folder-help"
                />
                <label htmlFor="series-folder">TV folder</label>
                <TextField.Root
                  id="series-folder"
                  value={form.series_folder}
                  onChange={(event) =>
                    update("series_folder", event.target.value)
                  }
                  maxLength={80}
                  required
                  size="3"
                  aria-describedby="folder-help"
                />
                <p className="field-help" id="folder-help">
                  Separate folder names under your media root. Use letters,
                  numbers, spaces, hyphens, or underscores. New movie imports
                  use this movie folder; saving preferences alone creates no
                  folders.
                </p>
              </fieldset>
              {error && (
                <div className="form-error" role="alert">
                  <p>{error}</p>
                  <Button
                    type="button"
                    variant="soft"
                    color="gray"
                    onClick={settings.reload}
                  >
                    Reload saved settings
                  </Button>
                </div>
              )}
              <div className="form-footer">
                <Button type="submit" size="3" disabled={!dirty || saving}>
                  <Save size={17} />
                  {saving ? "Saving…" : "Save preferences"}
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  color="gray"
                  disabled={!dirty || saving}
                  onClick={() => {
                    setForm(saved);
                    setError("");
                    setSuccess(false);
                  }}
                >
                  Reset changes
                </Button>
              </div>
              {success && (
                <p className="save-success" role="status">
                  <CheckCircle2 size={17} /> Preferences saved.
                </p>
              )}
            </form>
            <div className="space-y-6">
              <section className="panel settings-info">
                <div className="section-title">
                  <h2>
                    <Server size={19} /> Server & storage
                  </h2>
                </div>
                <p>
                  Set these paths with environment variables when starting
                  ReelScarey.
                </p>
                <dl>
                  {Object.entries(system.data.paths).map(([key, value]) => (
                    <div key={key}>
                      <dt>{key}</dt>
                      <dd>{value}</dd>
                    </div>
                  ))}
                </dl>
                <div className="server-version">
                  <span>Version {system.data.version}</span>
                  <Badge color="green">Database connected</Badge>
                </div>
              </section>
              <TMDbSettings />
              <IntegrationSettings kind="torznab" />
              <IntegrationSettings kind="qbittorrent" />
            </div>
          </div>
        )}
    </>
  );
}
